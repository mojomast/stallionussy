// Package commussy implements a WebSocket broadcast hub for real-time race
// telemetry in StallionUSSY. Clients connect via WebSocket and receive live
// tick-by-tick race data, narrative events, and race lifecycle updates.
package commussy

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Hub — manages all connected WebSocket clients and broadcasts messages
// ---------------------------------------------------------------------------

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	// clients is the set of registered clients. The bool value is always true.
	clients map[*Client]bool

	// broadcast is a channel of outbound messages to send to every client.
	broadcast chan []byte

	// register requests from new clients.
	register chan *Client

	// unregister requests from disconnecting clients.
	unregister chan *Client

	// mu protects the clients map for concurrent reads (e.g. ClientCount).
	mu sync.RWMutex
}

// NewHub creates and returns a new Hub instance, ready to Run().
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run is the Hub's main event loop. It must be started as a goroutine before
// any clients connect:
//
//	hub := commussy.NewHub()
//	go hub.Run()
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			// Collect clients that failed to receive so we can remove them
			// after releasing the read lock (avoids lock inversion).
			var stale []*Client
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client's send buffer is full — mark for removal.
					stale = append(stale, client)
				}
			}
			h.mu.RUnlock()

			// Now clean up stale clients under a write lock.
			if len(stale) > 0 {
				h.mu.Lock()
				for _, client := range stale {
					if _, ok := h.clients[client]; ok {
						delete(h.clients, client)
						close(client.send)
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ---------------------------------------------------------------------------
// Broadcast message types
// ---------------------------------------------------------------------------

// tickEntryPayload is the JSON shape for a single horse within a race_tick.
type tickEntryPayload struct {
	HorseID   string  `json:"horseID"`
	HorseName string  `json:"horseName"`
	Position  float64 `json:"position"`
	Speed     float64 `json:"speed"`
	Event     string  `json:"event"`
}

// raceTickMessage is the top-level JSON shape for a race_tick broadcast.
type raceTickMessage struct {
	Type    string             `json:"type"`
	RaceID  string             `json:"raceID"`
	Tick    int                `json:"tick"`
	Entries []tickEntryPayload `json:"entries"`
}

// raceLifecycleMessage is the JSON shape for race_start / race_end events.
type raceLifecycleMessage struct {
	Type   string       `json:"type"`
	RaceID string       `json:"raceID"`
	Race   *models.Race `json:"race"`
}

// narrativeMessage is the JSON shape for narrative broadcasts.
type narrativeMessage struct {
	Type    string `json:"type"`
	RaceID  string `json:"raceID"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Hub broadcast helpers
// ---------------------------------------------------------------------------

// BroadcastRaceTick marshals the current tick's race entries into JSON and
// sends them to every connected client. Each entry is slimmed down to the
// fields a spectator cares about: position, speed, and any narrative event.
func (h *Hub) BroadcastRaceTick(raceID string, tick int, entries []models.RaceEntry) {
	payload := make([]tickEntryPayload, len(entries))
	for i, e := range entries {
		// Grab the latest tick event for speed/event if available.
		var speed float64
		var event string
		if len(e.TickLog) > 0 {
			last := e.TickLog[len(e.TickLog)-1]
			speed = last.Speed
			event = last.Event
		}
		payload[i] = tickEntryPayload{
			HorseID:   e.HorseID,
			HorseName: e.HorseName,
			Position:  e.Position,
			Speed:     speed,
			Event:     event,
		}
	}

	msg := raceTickMessage{
		Type:    "race_tick",
		RaceID:  raceID,
		Tick:    tick,
		Entries: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("commussy: failed to marshal race_tick: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastRaceStart sends a race_start event with the full Race object to
// all connected clients.
func (h *Hub) BroadcastRaceStart(raceID string, race *models.Race) {
	msg := raceLifecycleMessage{
		Type:   "race_start",
		RaceID: raceID,
		Race:   race,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("commussy: failed to marshal race_start: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastRaceEnd sends a race_end event with final results to all
// connected clients.
func (h *Hub) BroadcastRaceEnd(raceID string, race *models.Race) {
	msg := raceLifecycleMessage{
		Type:   "race_end",
		RaceID: raceID,
		Race:   race,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("commussy: failed to marshal race_end: %v", err)
		return
	}
	h.broadcast <- data
}

// BroadcastNarrative sends a free-form narrative text event (commentary,
// flavor text, race highlights) to all connected clients.
func (h *Hub) BroadcastNarrative(raceID string, message string) {
	msg := narrativeMessage{
		Type:    "narrative",
		RaceID:  raceID,
		Message: message,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("commussy: failed to marshal narrative: %v", err)
		return
	}
	h.broadcast <- data
}

// ---------------------------------------------------------------------------
// Client — a single WebSocket connection managed by the Hub
// ---------------------------------------------------------------------------

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// pingPeriod sends pings at this interval. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 512
)

// Client is a middleman between a WebSocket connection and the Hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// ReadPump pumps messages from the WebSocket connection to the hub.
//
// For now, incoming messages are discarded — the WebSocket is primarily used
// as a broadcast channel. Future versions may accept client commands here
// (e.g., subscribe to specific race IDs).
//
// The application runs ReadPump in a per-connection goroutine. It ensures
// that there is at most one reader on a connection by executing all reads
// from this goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		// Read and discard. We only care about keeping the connection alive
		// and detecting disconnects.
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				log.Printf("commussy: read error: %v", err)
			}
			break
		}
	}
}

// WritePump pumps messages from the hub to the WebSocket connection.
//
// A goroutine running WritePump is started for each connection. It ensures
// that there is at most one writer to a connection by executing all writes
// from this goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel — send a close frame.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Drain any queued messages into the same write to reduce
			// syscall overhead (nagle-style batching).
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP upgrade handler
// ---------------------------------------------------------------------------

// upgrader configures the WebSocket upgrade with permissive CORS for dev.
// In production, tighten CheckOrigin to validate allowed origins.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins during development
	},
}

// ServeWs handles an HTTP request by upgrading it to a WebSocket connection,
// registering the resulting client with the Hub, and starting the read/write
// pump goroutines.
//
// Usage in your HTTP router:
//
//	hub := commussy.NewHub()
//	go hub.Run()
//	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
//	    commussy.ServeWs(hub, w, r)
//	})
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("commussy: websocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
	}
	hub.register <- client

	// Start pumps in their own goroutines.
	go client.WritePump()
	go client.ReadPump()
}
