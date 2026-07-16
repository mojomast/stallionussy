package models

import "time"

// Session is a server-side login session backing a JWT.
//
// Tokens themselves are never stored — only a SHA-256 hash — so a database
// leak cannot be replayed as live credentials. A JWT is only honored while a
// matching, unexpired session row exists, which gives the server real
// revocation and lets valid logins survive server restarts (the row outlives
// the process).
type Session struct {
	TokenHash string    `json:"-"`          // hex SHA-256 of the JWT (primary key)
	PlayerID  string    `json:"player_id"`  // user ID the session belongs to
	CreatedAt time.Time `json:"created_at"` // when the session was minted
	ExpiresAt time.Time `json:"expires_at"` // hard expiry (STALLION_SESSION_TTL)
	LastSeen  time.Time `json:"last_seen"`  // last authenticated request
}
