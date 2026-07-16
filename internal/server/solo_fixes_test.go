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

// ---------------------------------------------------------------------------
// R-9 — injury gating: rest always works, racing/training is blocked, and
// failed requests never burn daily turns
// ---------------------------------------------------------------------------

func injureHorse(t *testing.T, s *Server, horseID string) *models.Horse {
	t.Helper()
	h, err := s.stables.GetHorse(horseID)
	if err != nil {
		t.Fatalf("get horse: %v", err)
	}
	h.Injury = &models.Injury{
		Type:        models.InjuryMuscleStrain,
		Severity:    models.SeverityModerate,
		RacesLeft:   2,
		Description: "test injury",
		OccurredAt:  time.Now(),
	}
	return h
}

func dailyTrainsLeft(s *Server, userID string) int {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	return s.getOrCreateProgress(userID).DailyTrainsLeft
}

func dailyRacesLeft(s *Server, userID string) int {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	return s.getOrCreateProgress(userID).DailyRacesLeft
}

// TestInjuredHorse_RestHealsTrainingBlocked: the safe option (rest) used to
// be the ONLY one rejected for injured horses, while racing and training were
// allowed. Now rest always works (and heals), strenuous training is blocked
// without burning a daily turn.
func TestInjuredHorse_RestHealsTrainingBlocked(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Infirmary Ranch", "user-hurt", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	horse := injureHorse(t, s, stable.Horses[0].ID)

	// Strenuous training is rejected with a clear message and no turn burned.
	trainRR := postJSON(t, s, "/api/horses/"+horse.ID+"/train",
		map[string]any{"workoutType": "Sprint"}, "user-hurt", "hurt")
	if trainRR.Code != http.StatusBadRequest {
		t.Fatalf("train injured: status = %d, want 400\nbody: %s", trainRR.Code, trainRR.Body.String())
	}
	if got := dailyTrainsLeft(s, "user-hurt"); got != defaultDailyTrains {
		t.Fatalf("failed training consumed a daily turn: %d left, want %d (R-9)", got, defaultDailyTrains)
	}

	// Invalid workout type: 400 and no turn burned either.
	badRR := postJSON(t, s, "/api/horses/"+horse.ID+"/train",
		map[string]any{"workoutType": "Zumba"}, "user-hurt", "hurt")
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("invalid workout: status = %d, want 400", badRR.Code)
	}
	if got := dailyTrainsLeft(s, "user-hurt"); got != defaultDailyTrains {
		t.Fatalf("invalid workout consumed a daily turn: %d left, want %d", got, defaultDailyTrains)
	}

	// Rest works while injured and ticks down the recovery counter.
	restRR := postJSON(t, s, "/api/horses/"+horse.ID+"/rest", map[string]any{}, "user-hurt", "hurt")
	if restRR.Code != http.StatusCreated && restRR.Code != http.StatusOK {
		t.Fatalf("rest injured horse: status = %d (R-9: rest must always work)\nbody: %s",
			restRR.Code, restRR.Body.String())
	}
	if horse.Injury == nil {
		t.Fatal("injury healed after one rest; expected 2 rest days")
	}
	if got := horse.Injury.RacesLeft; got != 1 {
		t.Fatalf("injury RacesLeft after rest = %d, want 1", got)
	}

	// A second rest fully heals it.
	rest2 := postJSON(t, s, "/api/horses/"+horse.ID+"/rest", map[string]any{}, "user-hurt", "hurt")
	if rest2.Code != http.StatusCreated && rest2.Code != http.StatusOK {
		t.Fatalf("second rest: status = %d\nbody: %s", rest2.Code, rest2.Body.String())
	}
	if horse.Injury != nil {
		t.Fatalf("injury not healed after enough rest days: %+v", horse.Injury)
	}
}

// TestInjuredHorse_CannotRace: quick races skip injured horses, custom races
// reject them, and challenges refuse them on both sides.
func TestInjuredHorse_CannotRace(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Sideline Ranch", "user-side", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("need 2 starter horses, have %d", len(stable.Horses))
	}
	hurt := injureHorse(t, s, stable.Horses[0].ID)
	sound := stable.Horses[1]

	// Custom race with the injured horse is rejected — and does not burn a
	// daily race (validation precedes the cap).
	raceRR := postJSON(t, s, "/api/races", map[string]any{
		"horseIDs": []string{hurt.ID, sound.ID}, "trackType": "Sprintussy", "purse": 0,
	}, "user-side", "side")
	if raceRR.Code != http.StatusNotFound && raceRR.Code != http.StatusBadRequest {
		t.Fatalf("custom race with injured horse: status = %d, want 4xx\nbody: %s", raceRR.Code, raceRR.Body.String())
	}
	if got := dailyRacesLeft(s, "user-side"); got != defaultDailyRaces {
		t.Fatalf("rejected race consumed a daily entry: %d left, want %d", got, defaultDailyRaces)
	}

	// Quick race still works — it picks the sound horse, never the hurt one.
	quickRR := postJSON(t, s, "/api/races/quick", map[string]any{}, "user-side", "side")
	if quickRR.Code != http.StatusOK {
		t.Fatalf("quick race: status = %d\nbody: %s", quickRR.Code, quickRR.Body.String())
	}
	var result raceResult
	decodeJSON(t, quickRR, &result)
	for _, e := range result.Race.Entries {
		if e.HorseID == hurt.ID {
			t.Fatal("quick race auto-entered an injured horse (R-9)")
		}
	}

	// Challenges refuse an injured horse.
	chRR := postJSON(t, s, "/api/challenges", map[string]any{
		"horseID": hurt.ID, "defenderName": "CPU Arena", "wager": 0,
	}, "user-side", "side")
	if chRR.Code != http.StatusBadRequest {
		t.Fatalf("challenge with injured horse: status = %d, want 400\nbody: %s", chRR.Code, chRR.Body.String())
	}
	if got := dailyRacesLeft(s, "user-side"); got != defaultDailyRaces-1 {
		t.Fatalf("failed challenge did not refund the daily race: %d left, want %d",
			got, defaultDailyRaces-1)
	}
}

// TestQuickRace_AllHorsesInjured_NoTurnBurned: when every horse is hurt the
// player gets a helpful 400 and keeps the daily race entry.
func TestQuickRace_AllHorsesInjured_NoTurnBurned(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Ward Ranch", "user-ward", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	for i := range stable.Horses {
		injureHorse(t, s, stable.Horses[i].ID)
	}

	rr := postJSON(t, s, "/api/races/quick", map[string]any{}, "user-ward", "ward")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("quick race with all horses injured: status = %d, want 400\nbody: %s", rr.Code, rr.Body.String())
	}
	if got := dailyRacesLeft(s, "user-ward"); got != defaultDailyRaces {
		t.Fatalf("rejected quick race consumed a daily entry: %d left, want %d", got, defaultDailyRaces)
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
