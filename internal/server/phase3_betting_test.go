package server

// Phase 3 integration tests — betting wiring (Part B.1/B.5) and the full
// tournament lifecycle (Part B.4), including economic conservation.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// TestHTTP_ExhibitionBetting_FullLoop: user-opened pools previously never
// resolved (race IDs are server-generated UUIDs, so no race could ever match)
// — escrowed bets just waited for the stale-pool sweep. Pools now run a
// non-mutating exhibition race after the betting window and settle bets.
func TestHTTP_ExhibitionBetting_FullLoop(t *testing.T) {
	s := NewServer(nil)
	s.bettingWindow = 30 * time.Millisecond

	owner, err := s.createOwnedStable(context.Background(), "Owner Ranch", "user-owner", true)
	if err != nil {
		t.Fatalf("create owner stable: %v", err)
	}
	bettor, err := s.createOwnedStable(context.Background(), "Bettor Ranch", "user-bettor", true)
	if err != nil {
		t.Fatalf("create bettor stable: %v", err)
	}
	if len(owner.Horses) < 2 {
		t.Fatalf("owner needs 2 starter horses, has %d", len(owner.Horses))
	}
	h1, h2 := owner.Horses[0].ID, owner.Horses[1].ID

	// Unauthenticated pool creation is refused.
	anonRR := postJSON(t, s, "/api/betting/pools",
		map[string]any{"raceID": "exh-anon", "horseIDs": []string{h1, h2}}, "", "")
	if anonRR.Code != http.StatusUnauthorized {
		t.Fatalf("anon open pool: status = %d, want 401", anonRR.Code)
	}

	totalBefore := totalCummies(s)
	bettorBefore := bettor.Cummies

	// Open the pool and place a bet inside the window.
	openRR := postJSON(t, s, "/api/betting/pools",
		map[string]any{"raceID": "exh-1", "horseIDs": []string{h1, h2}}, "user-bettor", "bettor")
	if openRR.Code != http.StatusCreated {
		t.Fatalf("open pool: status = %d\nbody: %s", openRR.Code, openRR.Body.String())
	}
	betRR := postJSON(t, s, "/api/betting/pools/exh-1/bet",
		map[string]any{"horseID": h1, "amount": 100}, "user-bettor", "bettor")
	if betRR.Code != http.StatusOK {
		t.Fatalf("place bet: status = %d\nbody: %s", betRR.Code, betRR.Body.String())
	}
	if got, want := bettor.Cummies, bettorBefore-100; got != want {
		t.Fatalf("bet not escrowed: bettor = %d, want %d", got, want)
	}

	// Wait for the exhibition race to close, run, and settle the pool.
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.bettingMu.RLock()
		pool, exists := s.bettingPools["exh-1"]
		resolved := !exists || pool.Status == "resolved"
		s.bettingMu.RUnlock()
		if resolved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exhibition pool never resolved — betting loop is still unwired")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Pari-mutuel with a single bet: win pays back 90% (10% house cut),
	// a loss sends the full 100 to the house. Either way nothing leaks.
	won := bettor.Cummies == bettorBefore-100+90
	lost := bettor.Cummies == bettorBefore-100
	if !won && !lost {
		t.Fatalf("bettor balance %d matches neither win (%d) nor loss (%d)",
			bettor.Cummies, bettorBefore-10, bettorBefore-100)
	}
	if got := totalCummies(s); got != totalBefore {
		t.Fatalf("cummies not conserved through betting: before %d, after %d", totalBefore, got)
	}

	// Exhibition races must not touch horse progression.
	horse, _ := s.stables.GetHorse(h1)
	if horse.Races != 0 || horse.Fatigue != 0 {
		t.Fatalf("exhibition race mutated horse stats: races=%d fatigue=%v", horse.Races, horse.Fatigue)
	}
}

// TestHTTP_Tournament_FullLifecycle drives creation → registration → round
// betting window → rounds → final → prize payout over HTTP, and checks the
// economy balances to the declared 5% burn.
func TestHTTP_Tournament_FullLifecycle(t *testing.T) {
	s := NewServer(nil)

	org, err := s.createOwnedStable(context.Background(), "Organizer Ranch", "user-org", true)
	if err != nil {
		t.Fatalf("create organizer stable: %v", err)
	}
	rival, err := s.createOwnedStable(context.Background(), "Rival Ranch", "user-rival", true)
	if err != nil {
		t.Fatalf("create rival stable: %v", err)
	}

	totalBefore := totalCummies(s)

	// 1. Create (2 rounds, 100 entry fee).
	createRR := postJSON(t, s, "/api/tournaments", map[string]any{
		"name": "USSY Invitational", "trackType": "Sprintussy", "rounds": 2, "entryFee": 100,
	}, "user-org", "org")
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create tournament: status = %d\nbody: %s", createRR.Code, createRR.Body.String())
	}
	var tourn models.Tournament
	decodeJSON(t, createRR, &tourn)

	// 2. Register one horse per stable; fees fund the prize pool exactly once.
	for _, reg := range []struct {
		user, uname string
		stable      *models.Stable
	}{
		{"user-org", "org", org},
		{"user-rival", "rival", rival},
	} {
		before := reg.stable.Cummies
		rr := postJSON(t, s, "/api/tournaments/"+tourn.ID+"/register", map[string]any{
			"horseID": reg.stable.Horses[0].ID, "stableID": reg.stable.ID,
		}, reg.user, reg.uname)
		if rr.Code != http.StatusCreated {
			t.Fatalf("register (%s): status = %d\nbody: %s", reg.uname, rr.Code, rr.Body.String())
		}
		if got, want := reg.stable.Cummies, before-100; got != want {
			t.Fatalf("entry fee not collected from %s: %d, want %d", reg.uname, got, want)
		}
	}
	got, err := s.tournaments.GetTournament(tourn.ID)
	if err != nil {
		t.Fatalf("get tournament: %v", err)
	}
	if got.PrizePool != 200 {
		t.Fatalf("prize pool = %d, want 200 (fees must fund it exactly once)", got.PrizePool)
	}

	// 3. The round-1 betting window is open before the round runs.
	round1 := tournamentRoundRaceID(tourn.ID, 1)
	s.bettingMu.RLock()
	pool, poolOpen := s.bettingPools[round1]
	s.bettingMu.RUnlock()
	if !poolOpen || pool.Status != "open" {
		t.Fatal("round-1 betting pool should be open once the field has 2 horses")
	}
	betRR := postJSON(t, s, "/api/betting/pools/"+round1+"/bet",
		map[string]any{"horseID": rival.Horses[0].ID, "amount": 50}, "user-rival", "rival")
	if betRR.Code != http.StatusOK {
		t.Fatalf("round-1 bet: status = %d\nbody: %s", betRR.Code, betRR.Body.String())
	}

	// 4. Only the organizer may run rounds (H-4 regression guard).
	if rr := postJSON(t, s, "/api/tournaments/"+tourn.ID+"/race", map[string]any{}, "user-rival", "rival"); rr.Code != http.StatusForbidden {
		t.Fatalf("non-organizer round trigger: status = %d, want 403", rr.Code)
	}

	// 5. Run round 1: pool must resolve, round-2 pool must open.
	if rr := postJSON(t, s, "/api/tournaments/"+tourn.ID+"/race", map[string]any{}, "user-org", "org"); rr.Code != http.StatusCreated {
		t.Fatalf("round 1: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}
	s.bettingMu.RLock()
	r1pool, r1exists := s.bettingPools[round1]
	_, r2exists := s.bettingPools[tournamentRoundRaceID(tourn.ID, 2)]
	s.bettingMu.RUnlock()
	if r1exists && r1pool.Status != "resolved" {
		t.Fatalf("round-1 pool status = %q, want resolved", r1pool.Status)
	}
	if !r2exists {
		t.Fatal("round-2 betting pool should open when round 1 completes")
	}

	// 6. Run round 2 (final): tournament finishes and prizes pay out once.
	if rr := postJSON(t, s, "/api/tournaments/"+tourn.ID+"/race", map[string]any{}, "user-org", "org"); rr.Code != http.StatusCreated {
		t.Fatalf("round 2: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}
	finished, _ := s.tournaments.GetTournament(tourn.ID)
	if finished.Status != "Finished" {
		t.Fatalf("tournament status = %q, want Finished", finished.Status)
	}
	if finished.CurrentRound != 2 || len(finished.Races) != 2 {
		t.Fatalf("rounds recorded = %d/%d races=%d, want 2/2 with 2 races",
			finished.CurrentRound, finished.Rounds, len(finished.Races))
	}

	// 7. A finished tournament cannot run further rounds (no double payout).
	if rr := postJSON(t, s, "/api/tournaments/"+tourn.ID+"/race", map[string]any{}, "user-org", "org"); rr.Code != http.StatusBadRequest {
		t.Fatalf("post-final round trigger: status = %d, want 400", rr.Code)
	}

	// 8. Conservation: the only cummies that may vanish are the tournament's
	// ~5% burn (200-pool split 120/50/20 leaves 10 burned). The 50-cummie
	// bet either returned to a winner (minus the 10% cut, kept by the house)
	// or went to the house entirely — both stay inside the economy.
	burn := int64(10)
	if got, want := totalCummies(s), totalBefore-burn; got != want {
		t.Fatalf("cummies not conserved: before=%d after=%d (want after=%d, only the %d burn may vanish)",
			totalBefore, totalCummies(s), want, burn)
	}
}

// TestOpenBettingPoolNeverClobbersEscrow: re-opening an existing pool must
// return the live pool instead of overwriting it (which would vaporize the
// escrowed bets).
func TestOpenBettingPoolNeverClobbersEscrow(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Clobber Ranch", "user-clb", true)
	if err != nil {
		t.Fatalf("createOwnedStable: %v", err)
	}
	horses := []*models.Horse{}
	for i := range stable.Horses {
		h, _ := s.stables.GetHorse(stable.Horses[i].ID)
		horses = append(horses, h)
	}

	p1 := s.openBettingPool("race-x", horses)
	if _, msg := s.placeBet("race-x", "user-clb", "clb", horses[0].ID, 100); msg != "" {
		t.Fatalf("placeBet: %s", msg)
	}
	p2 := s.openBettingPool("race-x", horses)
	if p1 != p2 {
		t.Fatal("second open must return the existing pool, not a fresh one")
	}
	if len(p2.Bets) != 1 || p2.TotalPool != 100 {
		t.Fatalf("escrowed bet lost: bets=%d total=%d", len(p2.Bets), p2.TotalPool)
	}
}
