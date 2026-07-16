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

// ---------------------------------------------------------------------------
// R-4 — the daily race cap is enforced BEFORE a challenge is created
// ---------------------------------------------------------------------------

func TestCreateChallenge_AtDailyCap_NotCreated(t *testing.T) {
	s := NewServer(nil)
	challenger, err := s.createOwnedStable(context.Background(), "Capped Ranch", "user-capped", true)
	if err != nil {
		t.Fatalf("create challenger stable: %v", err)
	}
	if _, err := s.createOwnedStable(context.Background(), "Defender Ranch", "user-defend", true); err != nil {
		t.Fatalf("create defender stable: %v", err)
	}

	// Exhaust the challenger's daily races.
	for i := 0; i < defaultDailyRaces; i++ {
		if _, err := s.consumeDailyRace("user-capped"); err != nil {
			t.Fatalf("consume race %d: %v", i, err)
		}
	}

	rr := postJSON(t, s, "/api/challenges", map[string]any{
		"horseID": challenger.Horses[0].ID, "defenderName": "user-defend", "wager": 50,
	}, "user-capped", "user-capped")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("challenge at cap: status = %d, want 429\nbody: %s", rr.Code, rr.Body.String())
	}

	// The challenge must NOT exist — before the fix it was created, broadcast,
	// and acceptable despite the 429.
	s.challengeMu.RLock()
	count := len(s.challenges)
	s.challengeMu.RUnlock()
	if count != 0 {
		t.Fatalf("challenge was created despite the 429 (R-4): %d challenges registered", count)
	}
}

// TestCreateChallenge_FailedCreation_RefundsDailyRace: if the challenge
// itself is invalid, the pre-consumed daily race entry is returned.
func TestCreateChallenge_FailedCreation_RefundsDailyRace(t *testing.T) {
	s := NewServer(nil)
	challenger, err := s.createOwnedStable(context.Background(), "Refund Ranch", "user-refund", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}

	rr := postJSON(t, s, "/api/challenges", map[string]any{
		"horseID": challenger.Horses[0].ID, "defenderName": "nobody-here", "wager": 0,
	}, "user-refund", "user-refund")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("challenge vs missing player: status = %d, want 400\nbody: %s", rr.Code, rr.Body.String())
	}

	s.progressMu.Lock()
	left := s.getOrCreateProgress("user-refund").DailyRacesLeft
	s.progressMu.Unlock()
	if left != defaultDailyRaces {
		t.Fatalf("daily race not refunded after failed challenge: %d left, want %d", left, defaultDailyRaces)
	}
}

// ---------------------------------------------------------------------------
// R-5 — first_challenge is granted for CPU-arena challenges too
// ---------------------------------------------------------------------------

func TestBotChallenge_GrantsFirstChallenge(t *testing.T) {
	s := NewServer(nil)
	challenger, err := s.createOwnedStable(context.Background(), "Arena Ranch", "user-arena", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}

	rr := postJSON(t, s, "/api/challenges", map[string]any{
		"horseID": challenger.Horses[0].ID, "defenderName": "CPU Arena", "wager": 0,
	}, "user-arena", "user-arena")
	if rr.Code != http.StatusCreated {
		t.Fatalf("bot challenge: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}

	if !hasAchievement(challenger.Achievements, "first_challenge") {
		t.Fatal("first_challenge not granted for a CPU-arena challenge (R-5)")
	}
}

// ---------------------------------------------------------------------------
// R-6 — challenges live long enough for asynchronous PvP
// ---------------------------------------------------------------------------

func TestChallengeTTL_HoursNotMinutes(t *testing.T) {
	if challengeTTL < time.Hour {
		t.Fatalf("challengeTTL = %v; async PvP needs hours, not minutes (R-6)", challengeTTL)
	}

	s := NewServer(nil)
	challenger, err := s.createOwnedStable(context.Background(), "TTL Ranch", "user-ttl", true)
	if err != nil {
		t.Fatalf("create challenger stable: %v", err)
	}
	if _, err := s.createOwnedStable(context.Background(), "TTL Defender", "user-ttl2", true); err != nil {
		t.Fatalf("create defender stable: %v", err)
	}

	rr := postJSON(t, s, "/api/challenges", map[string]any{
		"horseID": challenger.Horses[0].ID, "defenderName": "user-ttl2", "wager": 0,
	}, "user-ttl", "user-ttl")
	if rr.Code != http.StatusCreated {
		t.Fatalf("challenge: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}
	var challenge models.Challenge
	decodeJSON(t, rr, &challenge)
	if got := time.Until(challenge.ExpiresAt); got < challengeTTL-time.Minute {
		t.Fatalf("challenge expires in %v, want ~%v", got, challengeTTL)
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
