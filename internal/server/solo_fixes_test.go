package server

// Regression tests for the SOLO_FINDINGS.md fixes (R-* / S-* / N-2): the
// "horses don't race" causes, solo progression dead ends, and offline NaN
// persistence.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// getJSONReq performs an authenticated (or anonymous when userID is empty)
// GET against the server mux.
func getJSONReq(t *testing.T, s *Server, path, userID, username string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if userID != "" {
		req = injectAuth(req, userID, username)
	}
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// R-2 — quick races must not open zero-window betting pools
// ---------------------------------------------------------------------------

// TestQuickRace_NoBettingPool: quick races simulate synchronously, so any
// pool they opened closed microseconds later — every bet was rejected and the
// pool-opened broadcast popped a stale full-screen modal on every client.
func TestQuickRace_NoBettingPool(t *testing.T) {
	s := NewServer(nil)
	if _, err := s.createOwnedStable(context.Background(), "Racer Ranch", "user-racer", true); err != nil {
		t.Fatalf("create stable: %v", err)
	}

	rr := postJSON(t, s, "/api/races/quick", map[string]any{}, "user-racer", "racer")
	if rr.Code != http.StatusOK {
		t.Fatalf("quick race: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}

	s.bettingMu.RLock()
	poolCount := len(s.bettingPools)
	s.bettingMu.RUnlock()
	if poolCount != 0 {
		t.Fatalf("quick race opened %d betting pool(s); want 0 (R-2)", poolCount)
	}
}

// ---------------------------------------------------------------------------
// R-3 — exhibition pools expose their deadline and produce a watchable race
// ---------------------------------------------------------------------------

func TestExhibitionPool_DeadlineAndWatchableRace(t *testing.T) {
	s := NewServer(nil)
	s.bettingWindow = 30 * time.Millisecond

	owner, err := s.createOwnedStable(context.Background(), "Exhibit Ranch", "user-exh", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	if len(owner.Horses) < 2 {
		t.Fatalf("need 2 starter horses, have %d", len(owner.Horses))
	}
	h1, h2 := owner.Horses[0].ID, owner.Horses[1].ID

	openRR := postJSON(t, s, "/api/betting/pools",
		map[string]any{"raceID": "exh-deadline", "horseIDs": []string{h1, h2}}, "user-exh", "exh")
	if openRR.Code != http.StatusCreated {
		t.Fatalf("open pool: status = %d\nbody: %s", openRR.Code, openRR.Body.String())
	}

	// The pool JSON must carry the betting deadline (R-3: without it a client
	// cannot render a countdown or know when the race will run).
	var pool models.BettingPool
	decodeJSON(t, openRR, &pool)
	if pool.ClosesAt.IsZero() {
		t.Fatal("exhibition pool JSON has no closesAt deadline (R-3)")
	}
	if got, want := pool.ClosesAt, pool.OpenedAt.Add(s.bettingWindow); !got.Equal(want) {
		t.Fatalf("closesAt = %v, want openedAt + window = %v", got, want)
	}

	// Wait for the window to elapse and the exhibition race to resolve.
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.bettingMu.RLock()
		p, exists := s.bettingPools["exh-deadline"]
		resolved := !exists || p.Status == "resolved"
		s.bettingMu.RUnlock()
		if resolved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exhibition pool never resolved")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The race that decided the pool must be fetchable for replay (was 404).
	replayRR := getJSONReq(t, s, "/api/races/exh-deadline", "", "")
	if replayRR.Code != http.StatusOK {
		t.Fatalf("exhibition race replay: status = %d, want 200 (R-3)\nbody: %s",
			replayRR.Code, replayRR.Body.String())
	}
	var replay raceResult
	decodeJSON(t, replayRR, &replay)
	if replay.Race == nil || len(replay.Race.Entries) < 2 {
		t.Fatalf("exhibition replay has no race entries: %+v", replay.Race)
	}
	if len(replay.NarrativeIndexed) == 0 {
		t.Fatal("exhibition replay has no narrative")
	}
}

// TestExhibitionPool_ZeroBets_BroadcastsResolution: a pool nobody bet on must
// still resolve cleanly (the pool disappears rather than lingering open).
func TestExhibitionPool_ZeroBets_Resolves(t *testing.T) {
	s := NewServer(nil)
	s.bettingWindow = 30 * time.Millisecond

	owner, err := s.createOwnedStable(context.Background(), "Quiet Ranch", "user-quiet", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	h1, h2 := owner.Horses[0].ID, owner.Horses[1].ID

	openRR := postJSON(t, s, "/api/betting/pools",
		map[string]any{"raceID": "exh-quiet", "horseIDs": []string{h1, h2}}, "user-quiet", "quiet")
	if openRR.Code != http.StatusCreated {
		t.Fatalf("open pool: status = %d\nbody: %s", openRR.Code, openRR.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		s.bettingMu.RLock()
		_, exists := s.bettingPools["exh-quiet"]
		s.bettingMu.RUnlock()
		if !exists {
			break // zero-bet pools are removed once resolved
		}
		if time.Now().After(deadline) {
			t.Fatal("zero-bet exhibition pool never resolved")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
