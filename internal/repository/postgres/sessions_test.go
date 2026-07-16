package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

func TestSQLiteSessionCRUD(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	sess := &models.Session{
		TokenHash: "hash-1",
		PlayerID:  "user-1",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
		LastSeen:  now,
	}
	if err := repo.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := repo.GetSession(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got == nil || got.PlayerID != "user-1" {
		t.Fatalf("GetSession = %+v", got)
	}
	if !got.CreatedAt.Equal(now) || !got.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("timestamps mangled: created=%v expires=%v", got.CreatedAt, got.ExpiresAt)
	}

	// Unknown hash: nil, nil (not an error).
	got, err = repo.GetSession(ctx, "nope")
	if err != nil || got != nil {
		t.Fatalf("GetSession(unknown) = %+v, %v; want nil, nil", got, err)
	}

	// Re-creating the same token is an upsert, not an error: register+login
	// in the same second mint byte-identical JWTs (second-granular iat), so
	// the same token_hash arrives twice and must refresh the row.
	sess2 := *sess
	sess2.ExpiresAt = now.Add(2 * time.Hour)
	sess2.LastSeen = now.Add(time.Minute)
	if err := repo.CreateSession(ctx, &sess2); err != nil {
		t.Fatalf("same-token CreateSession should upsert, got error: %v", err)
	}
	got, err = repo.GetSession(ctx, "hash-1")
	if err != nil || got == nil {
		t.Fatalf("GetSession after upsert: %+v, %v", got, err)
	}
	if !got.ExpiresAt.Equal(sess2.ExpiresAt) {
		t.Fatalf("upsert did not refresh expiry: got %v, want %v", got.ExpiresAt, sess2.ExpiresAt)
	}

	// Delete.
	if err := repo.DeleteSession(ctx, "hash-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got, _ = repo.GetSession(ctx, "hash-1"); got != nil {
		t.Fatalf("session survived delete: %+v", got)
	}
}

func TestSQLiteSessionTouchValidatesAndRefreshes(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	if err := repo.CreateSession(ctx, &models.Session{
		TokenHash: "live",
		PlayerID:  "user-1",
		CreatedAt: base,
		ExpiresAt: base.Add(time.Hour),
		LastSeen:  base,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// A live session touches successfully and last_seen advances.
	later := base.Add(10 * time.Minute)
	ok, err := repo.TouchSession(ctx, "live", later)
	if err != nil || !ok {
		t.Fatalf("TouchSession(live) = %v, %v; want true", ok, err)
	}
	got, _ := repo.GetSession(ctx, "live")
	if !got.LastSeen.Equal(later) {
		t.Fatalf("last_seen = %v, want %v", got.LastSeen, later)
	}

	// A request arriving after expiry is rejected — the session cannot be
	// resurrected by touching it.
	afterExpiry := base.Add(2 * time.Hour)
	ok, err = repo.TouchSession(ctx, "live", afterExpiry)
	if err != nil || ok {
		t.Fatalf("TouchSession(expired) = %v, %v; want false", ok, err)
	}
	// last_seen must NOT have advanced past the rejected touch.
	got, _ = repo.GetSession(ctx, "live")
	if !got.LastSeen.Equal(later) {
		t.Fatalf("expired touch mutated last_seen: %v", got.LastSeen)
	}

	// An unknown token hash is also a clean rejection.
	ok, err = repo.TouchSession(ctx, "ghost", later)
	if err != nil || ok {
		t.Fatalf("TouchSession(unknown) = %v, %v; want false", ok, err)
	}
}

func TestSQLiteSessionExpiryPurge(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	mk := func(hash, player string, expires time.Time) {
		t.Helper()
		if err := repo.CreateSession(ctx, &models.Session{
			TokenHash: hash, PlayerID: player,
			CreatedAt: base, ExpiresAt: expires, LastSeen: base,
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", hash, err)
		}
	}
	mk("expired-1", "user-1", base.Add(-time.Hour))
	mk("expired-2", "user-2", base.Add(-time.Minute))
	mk("live-1", "user-1", base.Add(time.Hour))

	n, err := repo.DeleteExpiredSessions(ctx, base)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("purged %d sessions, want 2", n)
	}
	if got, _ := repo.GetSession(ctx, "live-1"); got == nil {
		t.Fatal("live session was purged")
	}
	if got, _ := repo.GetSession(ctx, "expired-1"); got != nil {
		t.Fatal("expired session survived the purge")
	}
}

func TestSQLiteSessionDeleteForPlayer(t *testing.T) {
	db := newSQLiteDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()

	base := time.Now().UTC()
	for _, h := range []string{"a", "b"} {
		if err := repo.CreateSession(ctx, &models.Session{
			TokenHash: h, PlayerID: "user-1",
			CreatedAt: base, ExpiresAt: base.Add(time.Hour), LastSeen: base,
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", h, err)
		}
	}
	if err := repo.CreateSession(ctx, &models.Session{
		TokenHash: "c", PlayerID: "user-2",
		CreatedAt: base, ExpiresAt: base.Add(time.Hour), LastSeen: base,
	}); err != nil {
		t.Fatalf("CreateSession(c): %v", err)
	}

	if err := repo.DeleteSessionsForPlayer(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteSessionsForPlayer: %v", err)
	}
	for _, h := range []string{"a", "b"} {
		if got, _ := repo.GetSession(ctx, h); got != nil {
			t.Fatalf("user-1 session %q survived", h)
		}
	}
	if got, _ := repo.GetSession(ctx, "c"); got == nil {
		t.Fatal("user-2 session was collaterally deleted")
	}
}
