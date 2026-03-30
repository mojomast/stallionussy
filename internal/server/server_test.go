package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mojomast/stallionussy/internal/authussy"
	"github.com/mojomast/stallionussy/internal/models"
)

func TestCreateOwnedStableSeedsStarterHorses(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if stable == nil {
		t.Fatal("expected stable")
	}
	if len(stable.Horses) != starterHorseCount {
		t.Fatalf("expected %d starter horses, got %d", starterHorseCount, len(stable.Horses))
	}
	if stable.StarterGrants != 1 {
		t.Fatalf("starter grants = %d, want 1", stable.StarterGrants)
	}
	for _, horse := range stable.Horses {
		if horse.OwnerID != "user-1" {
			t.Fatalf("starter horse owner = %q, want user-1", horse.OwnerID)
		}
		if horse.Generation != 0 {
			t.Fatalf("starter horse generation = %d, want 0", horse.Generation)
		}
		if horse.Name == "" {
			t.Fatal("starter horse name should not be empty")
		}
	}
}

func TestEnsureStarterHorsesDoesNotReseedGrantedStable(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}

	stable.Horses = nil
	if err := s.ensureStarterHorses(context.Background(), stable); err != nil {
		t.Fatalf("ensureStarterHorses failed: %v", err)
	}
	if len(stable.Horses) != 0 {
		t.Fatalf("expected no reseed after initial grant, got %d horses", len(stable.Horses))
	}
	if stable.StarterGrants != 1 {
		t.Fatalf("starter grants = %d, want 1", stable.StarterGrants)
	}
}

func TestGrantStarterHorsesAllowsOneRecovery(t *testing.T) {
	s := NewServer(nil)

	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}
	stable.Horses = nil

	if err := s.grantStarterHorses(context.Background(), stable, true); err != nil {
		t.Fatalf("grantStarterHorses recovery failed: %v", err)
	}
	if len(stable.Horses) != starterHorseCount {
		t.Fatalf("expected %d recovery horses, got %d", starterHorseCount, len(stable.Horses))
	}
	if stable.StarterGrants != 2 {
		t.Fatalf("starter grants = %d, want 2", stable.StarterGrants)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}
	stable.Horses = nil

	if err := s.grantStarterHorses(context.Background(), stable, true); err == nil {
		t.Fatal("expected second recovery to fail")
	}
}

func TestCreateOwnedStableRejectsSecondStableForUser(t *testing.T) {
	s := NewServer(nil)

	if _, err := s.createOwnedStable(context.Background(), "First", "user-1", true); err != nil {
		t.Fatalf("initial createOwnedStable failed: %v", err)
	}
	if _, err := s.createOwnedStable(context.Background(), "Second", "user-1", true); err == nil {
		t.Fatal("expected second stable creation to fail")
	}
}

func TestUserOwnsHorseAcrossOwnedStable(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) == 0 {
		t.Fatal("expected starter horses")
	}

	horseID := stable.Horses[0].ID
	if !s.userOwnsHorse("user-1", horseID) {
		t.Fatal("expected owner to own starter horse")
	}
	if s.userOwnsHorse("user-2", horseID) {
		t.Fatal("unexpected ownership match for different user")
	}
}

func TestCountActiveStableHorsesIgnoresRetired(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	stable.Horses[0].Retired = true
	if got := countActiveStableHorses(stable); got != len(stable.Horses)-1 {
		t.Fatalf("countActiveStableHorses = %d, want %d", got, len(stable.Horses)-1)
	}
}

func TestLastActiveHorseWarningBlocksDestructiveAction(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	stable.Horses[1].Retired = true
	if err := lastActiveHorseWarning(&stable.Horses[0], stable, "glue"); err == nil {
		t.Fatal("expected last active horse warning")
	}
}

func TestFirstBestActiveHorseChoosesHighestELO(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 horses, got %d", len(stable.Horses))
	}
	h1, err := s.stables.GetHorse(stable.Horses[0].ID)
	if err != nil {
		t.Fatalf("GetHorse failed: %v", err)
	}
	h2, err := s.stables.GetHorse(stable.Horses[1].ID)
	if err != nil {
		t.Fatalf("GetHorse failed: %v", err)
	}
	h1.ELO = 1200
	h2.ELO = 1350
	s.syncHorseToStable(h1)
	s.syncHorseToStable(h2)
	best := s.firstBestActiveHorse(stable)
	if best == nil {
		t.Fatal("expected best active horse")
	}
	if best.ID != h2.ID {
		t.Fatalf("best horse = %s, want %s", best.ID, h2.ID)
	}
}

func TestSlotMultiplierRewardsTrips(t *testing.T) {
	if got := slotMultiplier([]string{"YOGURT", "YOGURT", "YOGURT"}); got <= 4 {
		t.Fatalf("slotMultiplier yogurt trips = %v, want > 4", got)
	}
	if got := slotMultiplier([]string{"CHERRY", "OATS", "BELL"}); got != 0 {
		t.Fatalf("slotMultiplier no match = %v, want 0", got)
	}
}

func TestSettlePokerTablePaysWinningStable(t *testing.T) {
	s := NewServer(nil)
	stable1, _ := s.createOwnedStable(context.Background(), "One", "user-1", true)
	stable2, _ := s.createOwnedStable(context.Background(), "Two", "user-2", true)
	stable1.CasinoChips = 0
	stable2.CasinoChips = 0
	table := &models.PokerTable{
		ID:            "table-1",
		StakeCurrency: "casino_chips",
		BuyIn:         50,
		Pot:           100,
		Seats: []models.PokerSeat{
			{UserID: "user-1", Username: "one", Hand: []models.PokerCard{{Rank: "A", Suit: "S"}, {Rank: "A", Suit: "H"}, {Rank: "A", Suit: "D"}, {Rank: "K", Suit: "S"}, {Rank: "Q", Suit: "S"}}},
			{UserID: "user-2", Username: "two", Hand: []models.PokerCard{{Rank: "2", Suit: "S"}, {Rank: "4", Suit: "H"}, {Rank: "6", Suit: "D"}, {Rank: "8", Suit: "C"}, {Rank: "10", Suit: "S"}}},
		},
	}
	s.settlePokerTable(table)
	if table.Status != models.PokerTableSettled {
		t.Fatalf("table status = %q, want settled", table.Status)
	}
	if stable1.CasinoChips != 100 {
		t.Fatalf("winner casino chips = %d, want 100", stable1.CasinoChips)
	}
	if stable2.CasinoChips != 0 {
		t.Fatalf("loser casino chips = %d, want 0", stable2.CasinoChips)
	}
}

func TestRecordDepartureAndClaimReturn(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	horse, err := s.stables.GetHorse(stable.Horses[0].ID)
	if err != nil {
		t.Fatalf("GetHorse failed: %v", err)
	}
	s.recordDeparture(context.Background(), stable, horse, models.DepartureCauseGlue)
	records := s.listDepartureRecords("user-1", 10)
	if len(records) != 1 {
		t.Fatalf("expected 1 departure record, got %d", len(records))
	}
	rec := records[0]
	rec.State = models.DepartureStateOmen
	rec.ReturnSummary = buildReturnSummary(rec)
	s.saveDepartureRecord(context.Background(), rec)
	returned := rec.HorseSnapshot
	returned.ID = "returned-1"
	returned.Retired = false
	returned.Traits = append(returned.Traits, returnTraitForRecord(rec))
	if err := s.stables.AddHorseToStable(stable.ID, &returned); err != nil {
		t.Fatalf("AddHorseToStable failed: %v", err)
	}
	if _, err := s.stables.GetHorse("returned-1"); err != nil {
		t.Fatalf("expected returned horse to be present: %v", err)
	}
}

func TestMaybeGrantDailyCasinoChipsOncePerDay(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Casino Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	stable.CasinoChips = 0
	if !s.maybeGrantDailyCasinoChips(context.Background(), "user-1", stable) {
		t.Fatal("expected first daily casino chip grant")
	}
	if stable.CasinoChips != casinoDailyChipGrant {
		t.Fatalf("casino chips = %d, want %d", stable.CasinoChips, casinoDailyChipGrant)
	}
	if s.maybeGrantDailyCasinoChips(context.Background(), "user-1", stable) {
		t.Fatal("expected second same-day grant to be skipped")
	}
}

func TestRedactPokerTableForUserHidesOpponentHand(t *testing.T) {
	table := &models.PokerTable{
		Status: models.PokerTableDrawing,
		Seats: []models.PokerSeat{
			{UserID: "user-1", Hand: []models.PokerCard{{Rank: "A", Suit: "S"}}},
			{UserID: "user-2", Hand: []models.PokerCard{{Rank: "K", Suit: "H"}}},
		},
	}
	redacted := redactPokerTableForUser(table, "user-1")
	if len(redacted.Seats[0].Hand) == 0 {
		t.Fatal("expected own hand to remain visible")
	}
	if len(redacted.Seats[1].Hand) != 0 {
		t.Fatal("expected opponent hand to be hidden during active hand")
	}
}

func TestSpinSlotsForStableConsumesAndPaysOut(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Casino Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	stable.CasinoChips = 100
	spin, err := s.spinSlotsForStable(context.Background(), stable, "user-1", 10)
	if err != nil {
		t.Fatalf("spinSlotsForStable failed: %v", err)
	}
	if spin == nil {
		t.Fatal("expected spin result")
	}
	if spin.WagerAmount != 10 {
		t.Fatalf("wager = %d, want 10", spin.WagerAmount)
	}
	if stable.CasinoChips < 90 {
		t.Fatalf("stable chips = %d, want at least 90 after spin resolution", stable.CasinoChips)
	}
}

func TestHandleCapabilities(t *testing.T) {
	s := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	s.handleCapabilities(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !body.Capabilities["starter_recovery"] || !body.Capabilities["casino"] || !body.Capabilities["departed_horses"] {
		t.Fatalf("unexpected capabilities payload: %#v", body.Capabilities)
	}
	if body.Capabilities["async_draw_poker"] || body.Capabilities["slot_machine"] {
		t.Fatalf("capabilities should not advertise unused subfeature keys: %#v", body.Capabilities)
	}
}

func TestHandleClaimStarterHorsesRequiresOwnershipAndEmptyStable(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Starter Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	unauthReq := httptest.NewRequest(http.MethodPost, "/api/stables/"+stable.ID+"/claim-starters", nil)
	unauthReq.SetPathValue("id", stable.ID)
	unauthRR := httptest.NewRecorder()
	s.handleClaimStarterHorses(unauthRR, unauthReq)
	if unauthRR.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want 401", unauthRR.Code)
	}

	wrongOwnerReq := httptest.NewRequest(http.MethodPost, "/api/stables/"+stable.ID+"/claim-starters", nil)
	wrongOwnerReq.SetPathValue("id", stable.ID)
	wrongOwnerReq = wrongOwnerReq.WithContext(context.WithValue(wrongOwnerReq.Context(), authussy.UserContextKey, &authussy.Claims{UserID: "user-2", Username: "other"}))
	wrongOwnerRR := httptest.NewRecorder()
	s.handleClaimStarterHorses(wrongOwnerRR, wrongOwnerReq)
	if wrongOwnerRR.Code != http.StatusForbidden {
		t.Fatalf("wrong owner status = %d, want 403", wrongOwnerRR.Code)
	}

	ownedReq := httptest.NewRequest(http.MethodPost, "/api/stables/"+stable.ID+"/claim-starters", nil)
	ownedReq.SetPathValue("id", stable.ID)
	ownedReq = ownedReq.WithContext(context.WithValue(ownedReq.Context(), authussy.UserContextKey, &authussy.Claims{UserID: "user-1", Username: "owner"}))
	ownedRR := httptest.NewRecorder()
	s.handleClaimStarterHorses(ownedRR, ownedReq)
	if ownedRR.Code != http.StatusBadRequest {
		t.Fatalf("non-empty stable status = %d, want 400", ownedRR.Code)
	}

	for _, horse := range append([]models.Horse(nil), stable.Horses...) {
		if err := s.stables.RemoveHorse(horse.ID); err != nil {
			t.Fatalf("RemoveHorse failed: %v", err)
		}
	}
	stable.Horses = nil
	recoveryReq := httptest.NewRequest(http.MethodPost, "/api/stables/"+stable.ID+"/claim-starters", nil)
	recoveryReq.SetPathValue("id", stable.ID)
	recoveryReq = recoveryReq.WithContext(context.WithValue(recoveryReq.Context(), authussy.UserContextKey, &authussy.Claims{UserID: "user-1", Username: "owner"}))
	recoveryRR := httptest.NewRecorder()
	s.handleClaimStarterHorses(recoveryRR, recoveryReq)
	if recoveryRR.Code != http.StatusOK {
		t.Fatalf("recovery status = %d, want 200", recoveryRR.Code)
	}
}
