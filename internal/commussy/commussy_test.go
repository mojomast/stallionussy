package commussy

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers — mock client with a buffered send channel (no real WebSocket)
// ---------------------------------------------------------------------------

// mockClient creates a Client with a nil conn but a buffered send channel.
// This lets us test Hub logic (register, unregister, broadcast) without
// actual WebSocket connections.
func mockClient(hub *Hub) *Client {
	return &Client{
		hub:  hub,
		conn: nil,
		send: make(chan []byte, 256),
	}
}

// drainSend reads all pending messages from a client's send channel,
// returning them as a slice.
func drainSend(c *Client) [][]byte {
	var msgs [][]byte
	for {
		select {
		case msg := <-c.send:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// waitForCondition spins briefly until cond() returns true or timeout.
func waitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// Hub creation
// ---------------------------------------------------------------------------

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.clients == nil {
		t.Error("clients map is nil")
	}
	if hub.broadcast == nil {
		t.Error("broadcast channel is nil")
	}
	if hub.register == nil {
		t.Error("register channel is nil")
	}
	if hub.unregister == nil {
		t.Error("unregister channel is nil")
	}
}

func TestNewHub_ClientCountZero(t *testing.T) {
	hub := NewHub()
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Hub Run — register and unregister
// ---------------------------------------------------------------------------

func TestHub_RegisterClient(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	ok := waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})
	if !ok {
		t.Errorf("expected 1 client, got %d", hub.ClientCount())
	}
}

func TestHub_UnregisterClient(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	hub.unregister <- c

	ok := waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 0
	})
	if !ok {
		t.Errorf("expected 0 clients after unregister, got %d", hub.ClientCount())
	}
}

func TestHub_UnregisterClosesChannel(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	hub.unregister <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 0
	})

	// The send channel should be closed after unregister.
	_, open := <-c.send
	if open {
		t.Error("expected send channel to be closed after unregister")
	}
}

func TestHub_UnregisterNonexistentClient(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	// Don't register — just unregister. Should not panic.
	hub.unregister <- c

	// Give the hub time to process.
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestHub_MultipleClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	clients := make([]*Client, 10)
	for i := 0; i < 10; i++ {
		clients[i] = mockClient(hub)
		hub.register <- clients[i]
	}

	ok := waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 10
	})
	if !ok {
		t.Errorf("expected 10 clients, got %d", hub.ClientCount())
	}

	// Unregister half
	for i := 0; i < 5; i++ {
		hub.unregister <- clients[i]
	}

	ok = waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 5
	})
	if !ok {
		t.Errorf("expected 5 clients, got %d", hub.ClientCount())
	}
}

// ---------------------------------------------------------------------------
// Hub broadcast
// ---------------------------------------------------------------------------

func TestHub_BroadcastToAllClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c1 := mockClient(hub)
	c2 := mockClient(hub)
	c3 := mockClient(hub)
	hub.register <- c1
	hub.register <- c2
	hub.register <- c3

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 3
	})

	testMsg := []byte(`{"type":"test"}`)
	hub.broadcast <- testMsg

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	for i, c := range []*Client{c1, c2, c3} {
		msgs := drainSend(c)
		if len(msgs) != 1 {
			t.Errorf("client %d: expected 1 message, got %d", i, len(msgs))
			continue
		}
		if string(msgs[0]) != string(testMsg) {
			t.Errorf("client %d: got %q, want %q", i, string(msgs[0]), string(testMsg))
		}
	}
}

func TestHub_BroadcastMultipleMessages(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	for i := 0; i < 5; i++ {
		hub.broadcast <- []byte(`{"seq":` + string(rune('0'+i)) + `}`)
	}

	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

func TestHub_BroadcastToNoClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Broadcast to empty hub — should not panic.
	hub.broadcast <- []byte(`{"type":"nobody_home"}`)

	time.Sleep(50 * time.Millisecond)
	// Just verify it didn't panic.
}

func TestHub_StaleClientRemoved(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// Create a client with a zero-buffer send channel — it will immediately be "full".
	staleClient := &Client{
		hub:  hub,
		conn: nil,
		send: make(chan []byte, 0),
	}
	hub.register <- staleClient

	goodClient := mockClient(hub)
	hub.register <- goodClient

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 2
	})

	// Send a broadcast — staleClient can't receive it.
	hub.broadcast <- []byte(`{"type":"bye_stale"}`)

	// Wait for the hub to clean up the stale client.
	ok := waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})
	if !ok {
		t.Errorf("expected stale client to be removed, got %d clients", hub.ClientCount())
	}

	// Good client should have received the message.
	msgs := drainSend(goodClient)
	if len(msgs) != 1 {
		t.Errorf("good client expected 1 message, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Broadcast helpers — BroadcastRaceStart
// ---------------------------------------------------------------------------

func TestBroadcastRaceStart(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	race := &models.Race{
		ID:        "race-1",
		TrackType: models.TrackSprintussy,
		Distance:  800,
		Status:    models.RaceStatusPending,
	}

	hub.BroadcastRaceStart("race-1", race)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg raceLifecycleMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Type != "race_start" {
		t.Errorf("type = %q, want race_start", msg.Type)
	}
	if msg.RaceID != "race-1" {
		t.Errorf("raceID = %q, want race-1", msg.RaceID)
	}
	if msg.Race == nil {
		t.Error("race is nil")
	}
}

// ---------------------------------------------------------------------------
// Broadcast helpers — BroadcastRaceEnd
// ---------------------------------------------------------------------------

func TestBroadcastRaceEnd(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	race := &models.Race{
		ID:     "race-2",
		Status: models.RaceStatusFinished,
	}

	hub.BroadcastRaceEnd("race-2", race)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg raceLifecycleMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Type != "race_end" {
		t.Errorf("type = %q, want race_end", msg.Type)
	}
	if msg.RaceID != "race-2" {
		t.Errorf("raceID = %q, want race-2", msg.RaceID)
	}
}

// ---------------------------------------------------------------------------
// Broadcast helpers — BroadcastRaceTick
// ---------------------------------------------------------------------------

func TestBroadcastRaceTick(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	entries := []models.RaceEntry{
		{
			HorseID:   "h1",
			HorseName: "Lightning",
			Position:  150.0,
			TickLog: []models.TickEvent{
				{Tick: 5, Position: 150.0, Speed: 12.5, Event: "BURST"},
			},
		},
		{
			HorseID:   "h2",
			HorseName: "Thunder",
			Position:  140.0,
			TickLog:   []models.TickEvent{},
		},
	}

	hub.BroadcastRaceTick("race-tick-1", 5, entries)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg raceTickMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Type != "race_tick" {
		t.Errorf("type = %q, want race_tick", msg.Type)
	}
	if msg.RaceID != "race-tick-1" {
		t.Errorf("raceID = %q, want race-tick-1", msg.RaceID)
	}
	if msg.Tick != 5 {
		t.Errorf("tick = %d, want 5", msg.Tick)
	}
	if len(msg.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(msg.Entries))
	}

	// First entry should have speed/event from tick log
	if msg.Entries[0].Speed != 12.5 {
		t.Errorf("entry[0] speed = %v, want 12.5", msg.Entries[0].Speed)
	}
	if msg.Entries[0].Event != "BURST" {
		t.Errorf("entry[0] event = %q, want BURST", msg.Entries[0].Event)
	}

	// Second entry has empty tick log — speed/event should be zero/empty
	if msg.Entries[1].Speed != 0 {
		t.Errorf("entry[1] speed = %v, want 0", msg.Entries[1].Speed)
	}
	if msg.Entries[1].Event != "" {
		t.Errorf("entry[1] event = %q, want empty", msg.Entries[1].Event)
	}
}

func TestBroadcastRaceTick_EmptyEntries(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	hub.BroadcastRaceTick("race-empty", 0, nil)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg raceTickMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Entries == nil {
		// JSON will decode [] as nil or empty slice — both are acceptable.
	}
}

// ---------------------------------------------------------------------------
// Broadcast helpers — BroadcastNarrative
// ---------------------------------------------------------------------------

func TestBroadcastNarrative(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	hub.BroadcastNarrative("race-narr", "Lightning strikes! Thunder gallops ahead!")
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg narrativeMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Type != "narrative" {
		t.Errorf("type = %q, want narrative", msg.Type)
	}
	if msg.Message != "Lightning strikes! Thunder gallops ahead!" {
		t.Errorf("message = %q", msg.Message)
	}
}

// ---------------------------------------------------------------------------
// Broadcast helpers — BroadcastNarrativeTick
// ---------------------------------------------------------------------------

func TestBroadcastNarrativeTick(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	texts := []string{"Horse A surges!", "Horse B stumbles!"}
	classes := []string{"event-burst", "event-stumble"}

	hub.BroadcastNarrativeTick("race-ntick", 7, texts, classes)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg narrativeTickMessage
	if err := json.Unmarshal(msgs[0], &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if msg.Type != "narrative_tick" {
		t.Errorf("type = %q, want narrative_tick", msg.Type)
	}
	if msg.Tick != 7 {
		t.Errorf("tick = %d, want 7", msg.Tick)
	}
	if len(msg.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(msg.Lines))
	}
	if msg.Lines[0].Text != "Horse A surges!" {
		t.Errorf("line[0].text = %q", msg.Lines[0].Text)
	}
	if msg.Lines[0].Class != "event-burst" {
		t.Errorf("line[0].class = %q, want event-burst", msg.Lines[0].Class)
	}
}

func TestBroadcastNarrativeTick_DefaultClass(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := mockClient(hub)
	hub.register <- c

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == 1
	})

	// Pass texts but shorter classes slice.
	texts := []string{"Line 1", "Line 2"}
	classes := []string{"custom-class"} // only one class for two lines

	hub.BroadcastNarrativeTick("race-def", 1, texts, classes)
	time.Sleep(100 * time.Millisecond)

	msgs := drainSend(c)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var msg narrativeTickMessage
	json.Unmarshal(msgs[0], &msg)

	if msg.Lines[0].Class != "custom-class" {
		t.Errorf("line[0].class = %q, want custom-class", msg.Lines[0].Class)
	}
	// Second line should default to "event-normal"
	if msg.Lines[1].Class != "event-normal" {
		t.Errorf("line[1].class = %q, want event-normal", msg.Lines[1].Class)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestHub_ConcurrentRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	const numClients = 50
	var wg sync.WaitGroup

	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = mockClient(hub)
	}

	// Register all concurrently.
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			hub.register <- c
		}(clients[i])
	}
	wg.Wait()

	ok := waitForCondition(time.Second, func() bool {
		return hub.ClientCount() == numClients
	})
	if !ok {
		t.Errorf("expected %d clients, got %d", numClients, hub.ClientCount())
	}

	// Unregister all concurrently.
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			hub.unregister <- c
		}(clients[i])
	}
	wg.Wait()

	ok = waitForCondition(time.Second, func() bool {
		return hub.ClientCount() == 0
	})
	if !ok {
		t.Errorf("expected 0 clients after concurrent unregister, got %d", hub.ClientCount())
	}
}

func TestHub_ConcurrentBroadcast(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	const numClients = 5
	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		clients[i] = mockClient(hub)
		hub.register <- clients[i]
	}

	waitForCondition(500*time.Millisecond, func() bool {
		return hub.ClientCount() == numClients
	})

	// Send many broadcasts concurrently.
	const numMessages = 20
	var wg sync.WaitGroup
	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			hub.broadcast <- []byte(`{"n":` + string(rune('0'+n%10)) + `}`)
		}(i)
	}
	wg.Wait()

	time.Sleep(200 * time.Millisecond)

	// Each client should have received all messages.
	for i, c := range clients {
		msgs := drainSend(c)
		if len(msgs) != numMessages {
			t.Errorf("client %d: expected %d messages, got %d", i, numMessages, len(msgs))
		}
	}
}

func TestHub_ClientCountThreadSafe(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	var wg sync.WaitGroup
	// Read ClientCount concurrently while registering.
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			hub.register <- mockClient(hub)
		}()
		go func() {
			defer wg.Done()
			_ = hub.ClientCount()
		}()
	}
	wg.Wait()
}
