package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	spin, err := s.spinSlotsForStable(context.Background(), stable, "user-1", "testuser", 10, 9)
	if err != nil {
		t.Fatalf("spinSlotsForStable failed: %v", err)
	}
	if spin == nil {
		t.Fatal("expected spin result")
	}
	if spin.WagerAmount != 10 {
		t.Fatalf("wager = %d, want 10", spin.WagerAmount)
	}
	// Total cost is 10 * 9 = 90 chips. After payout, chips should be at least 10 (since we deducted 90 from 100).
	if stable.CasinoChips < 10 {
		t.Fatalf("stable chips = %d, want at least 10 after spin resolution", stable.CasinoChips)
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

// ===========================================================================
// Rate Limiter Tests
// ===========================================================================

func TestRateLimiterAllowsRequestsUnderLimit(t *testing.T) {
	rl := newRateLimiter()

	// Auth category allows 10 burst tokens. All 10 should be allowed.
	for i := 0; i < 10; i++ {
		if !rl.allow("192.168.1.1", rlCategoryAuth) {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}
}

func TestRateLimiterBlocksAfterBurstExhausted(t *testing.T) {
	rl := newRateLimiter()

	// Exhaust the auth bucket (10 tokens).
	for i := 0; i < 10; i++ {
		rl.allow("192.168.1.1", rlCategoryAuth)
	}

	// 11th request should be blocked.
	if rl.allow("192.168.1.1", rlCategoryAuth) {
		t.Fatal("11th request should have been rate-limited")
	}
}

func TestRateLimiterDifferentIPsAreIndependent(t *testing.T) {
	rl := newRateLimiter()

	// Exhaust IP-1's auth bucket.
	for i := 0; i < 10; i++ {
		rl.allow("10.0.0.1", rlCategoryAuth)
	}

	// IP-2 should still be allowed.
	if !rl.allow("10.0.0.2", rlCategoryAuth) {
		t.Fatal("different IP should not be rate-limited")
	}
}

func TestRateLimiterDifferentCategoriesAreIndependent(t *testing.T) {
	rl := newRateLimiter()

	// Exhaust the auth bucket for an IP.
	for i := 0; i < 10; i++ {
		rl.allow("10.0.0.1", rlCategoryAuth)
	}

	// Same IP should still be allowed in the read category.
	if !rl.allow("10.0.0.1", rlCategoryRead) {
		t.Fatal("different category should not be rate-limited")
	}
}

func TestRateLimiterTokensRefillOverTime(t *testing.T) {
	rl := newRateLimiter()

	// Exhaust the auth bucket (10 tokens).
	for i := 0; i < 10; i++ {
		rl.allow("10.0.0.1", rlCategoryAuth)
	}
	if rl.allow("10.0.0.1", rlCategoryAuth) {
		t.Fatal("should be rate-limited after exhausting tokens")
	}

	// Simulate time passing by manually adjusting lastSeen.
	rl.mu.Lock()
	v := rl.visitors["10.0.0.1"][rlCategoryAuth]
	v.lastSeen = v.lastSeen.Add(-61 * time.Second) // 61s ago → ~1.7 tokens refilled
	rl.mu.Unlock()

	// After enough time, a request should be allowed again.
	if !rl.allow("10.0.0.1", rlCategoryAuth) {
		t.Fatal("should be allowed after token refill")
	}
}

func TestRateLimiterCleanupRemovesStaleVisitors(t *testing.T) {
	rl := newRateLimiter()
	rl.allow("10.0.0.1", rlCategoryAuth)
	rl.allow("10.0.0.2", rlCategoryRead)

	// Age both visitors past the cleanup threshold.
	rl.mu.Lock()
	for _, buckets := range rl.visitors {
		for _, v := range buckets {
			v.lastSeen = time.Now().Add(-10 * time.Minute)
		}
	}
	rl.mu.Unlock()

	rl.cleanup(5 * time.Minute)

	rl.mu.Lock()
	remaining := len(rl.visitors)
	rl.mu.Unlock()

	if remaining != 0 {
		t.Fatalf("expected 0 visitors after cleanup, got %d", remaining)
	}
}

func TestRateLimiterCleanupKeepsRecentVisitors(t *testing.T) {
	rl := newRateLimiter()
	rl.allow("10.0.0.1", rlCategoryAuth) // recent
	rl.allow("10.0.0.2", rlCategoryRead) // will be stale

	// Only age IP-2.
	rl.mu.Lock()
	rl.visitors["10.0.0.2"][rlCategoryRead].lastSeen = time.Now().Add(-10 * time.Minute)
	rl.mu.Unlock()

	rl.cleanup(5 * time.Minute)

	rl.mu.Lock()
	remaining := len(rl.visitors)
	rl.mu.Unlock()

	if remaining != 1 {
		t.Fatalf("expected 1 visitor after cleanup, got %d", remaining)
	}
}

func TestClassifyRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   rateLimitCategory
	}{
		{"POST", "/api/auth/login", rlCategoryAuth},
		{"POST", "/api/auth/register", rlCategoryAuth},
		{"GET", "/api/auth/me", rlCategoryRead},
		{"POST", "/api/casino/chips/exchange", rlCategoryCasino},
		{"GET", "/api/casino/poker", rlCategoryCasino},
		{"POST", "/api/casino/slots/spin", rlCategoryCasino},
		{"POST", "/api/stables", rlCategoryMutation},
		{"PUT", "/api/stables/abc", rlCategoryMutation},
		{"DELETE", "/api/market/abc", rlCategoryMutation},
		{"GET", "/api/stables", rlCategoryRead},
		{"GET", "/api/horses", rlCategoryRead},
	}

	for _, tt := range tests {
		r := httptest.NewRequest(tt.method, tt.path, nil)
		got := classifyRequest(r)
		if got != tt.want {
			t.Errorf("%s %s: got category %d, want %d", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		want       string
	}{
		{"plain remote addr", "192.168.1.1:12345", "", "", "192.168.1.1"},
		{"x-forwarded-for single", "10.0.0.1:1234", "203.0.113.50", "", "203.0.113.50"},
		{"x-forwarded-for chain", "10.0.0.1:1234", "203.0.113.50, 70.41.3.18", "", "203.0.113.50"},
		{"x-real-ip", "10.0.0.1:1234", "", "203.0.113.99", "203.0.113.99"},
		{"xff takes precedence over xri", "10.0.0.1:1234", "1.2.3.4", "5.6.7.8", "1.2.3.4"},
		{"no port in remote addr", "192.168.1.1", "", "", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				r.Header.Set("X-Real-IP", tt.xri)
			}
			got := extractIP(r)
			if got != tt.want {
				t.Errorf("extractIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimitMiddlewareReturns429(t *testing.T) {
	rl := newRateLimiter()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rateLimitMiddleware(rl, inner)

	// Exhaust auth limit (10 tokens).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("POST", "/api/auth/login", nil)
		req.RemoteAddr = "1.2.3.4:5678"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, rr.Code)
		}
	}

	// 11th should be 429.
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
}

func TestRateLimitMiddlewareSkipsWebSocketAndOptions(t *testing.T) {
	rl := newRateLimiter()

	// Exhaust all buckets for this IP by hammering a mutation endpoint.
	for i := 0; i < 200; i++ {
		rl.allow("5.5.5.5", rlCategoryAuth)
		rl.allow("5.5.5.5", rlCategoryCasino)
		rl.allow("5.5.5.5", rlCategoryMutation)
		rl.allow("5.5.5.5", rlCategoryRead)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rateLimitMiddleware(rl, inner)

	// OPTIONS should pass through even when rate-limited.
	optReq := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	optReq.RemoteAddr = "5.5.5.5:1234"
	optRR := httptest.NewRecorder()
	handler.ServeHTTP(optRR, optReq)
	if optRR.Code != http.StatusOK {
		t.Fatalf("OPTIONS should bypass rate limit, got %d", optRR.Code)
	}

	// WebSocket upgrade should pass through.
	wsReq := httptest.NewRequest("GET", "/ws", nil)
	wsReq.RemoteAddr = "5.5.5.5:1234"
	wsReq.Header.Set("Upgrade", "websocket")
	wsRR := httptest.NewRecorder()
	handler.ServeHTTP(wsRR, wsReq)
	if wsRR.Code != http.StatusOK {
		t.Fatalf("WebSocket upgrade should bypass rate limit, got %d", wsRR.Code)
	}
}

func TestRateLimiterReadCategoryBurstSize(t *testing.T) {
	rl := newRateLimiter()

	// Read category allows 120 burst tokens.
	for i := 0; i < 120; i++ {
		if !rl.allow("10.0.0.1", rlCategoryRead) {
			t.Fatalf("read request %d should have been allowed", i+1)
		}
	}

	// 121st should be blocked.
	if rl.allow("10.0.0.1", rlCategoryRead) {
		t.Fatal("121st read request should have been rate-limited")
	}
}

// ---------------------------------------------------------------------------
// Poker hand evaluation tests
// ---------------------------------------------------------------------------

func TestEvaluatePokerHand(t *testing.T) {
	tests := []struct {
		name      string
		hand      []models.PokerCard
		wantScore int
		wantLabel string
	}{
		{
			name: "straight flush",
			hand: []models.PokerCard{
				{Rank: "5", Suit: "H"}, {Rank: "6", Suit: "H"}, {Rank: "7", Suit: "H"}, {Rank: "8", Suit: "H"}, {Rank: "9", Suit: "H"},
			},
			wantScore: 800,
			wantLabel: "straight flush",
		},
		{
			name: "royal flush (ace-high straight flush)",
			hand: []models.PokerCard{
				{Rank: "10", Suit: "S"}, {Rank: "J", Suit: "S"}, {Rank: "Q", Suit: "S"}, {Rank: "K", Suit: "S"}, {Rank: "A", Suit: "S"},
			},
			wantScore: 800,
			wantLabel: "straight flush",
		},
		{
			name: "four of a kind",
			hand: []models.PokerCard{
				{Rank: "K", Suit: "S"}, {Rank: "K", Suit: "H"}, {Rank: "K", Suit: "D"}, {Rank: "K", Suit: "C"}, {Rank: "3", Suit: "S"},
			},
			wantScore: 700,
			wantLabel: "four of a kind",
		},
		{
			name: "full house",
			hand: []models.PokerCard{
				{Rank: "A", Suit: "S"}, {Rank: "A", Suit: "H"}, {Rank: "A", Suit: "D"}, {Rank: "K", Suit: "S"}, {Rank: "K", Suit: "H"},
			},
			wantScore: 650,
			wantLabel: "full house",
		},
		{
			name: "flush (not straight)",
			hand: []models.PokerCard{
				{Rank: "2", Suit: "D"}, {Rank: "5", Suit: "D"}, {Rank: "7", Suit: "D"}, {Rank: "J", Suit: "D"}, {Rank: "A", Suit: "D"},
			},
			wantScore: 600,
			wantLabel: "flush",
		},
		{
			name: "straight (mixed suits)",
			hand: []models.PokerCard{
				{Rank: "4", Suit: "S"}, {Rank: "5", Suit: "H"}, {Rank: "6", Suit: "D"}, {Rank: "7", Suit: "C"}, {Rank: "8", Suit: "S"},
			},
			wantScore: 550,
			wantLabel: "straight",
		},
		{
			name: "ace-low straight (A-2-3-4-5)",
			hand: []models.PokerCard{
				{Rank: "A", Suit: "S"}, {Rank: "2", Suit: "H"}, {Rank: "3", Suit: "D"}, {Rank: "4", Suit: "C"}, {Rank: "5", Suit: "S"},
			},
			wantScore: 550,
			wantLabel: "straight",
		},
		{
			name: "ace-high straight (10-J-Q-K-A)",
			hand: []models.PokerCard{
				{Rank: "10", Suit: "S"}, {Rank: "J", Suit: "H"}, {Rank: "Q", Suit: "D"}, {Rank: "K", Suit: "C"}, {Rank: "A", Suit: "S"},
			},
			wantScore: 550,
			wantLabel: "straight",
		},
		{
			name: "three of a kind",
			hand: []models.PokerCard{
				{Rank: "9", Suit: "S"}, {Rank: "9", Suit: "H"}, {Rank: "9", Suit: "D"}, {Rank: "4", Suit: "C"}, {Rank: "7", Suit: "S"},
			},
			wantScore: 500,
			wantLabel: "three of a kind",
		},
		{
			name: "two pair",
			hand: []models.PokerCard{
				{Rank: "J", Suit: "S"}, {Rank: "J", Suit: "H"}, {Rank: "3", Suit: "D"}, {Rank: "3", Suit: "C"}, {Rank: "K", Suit: "S"},
			},
			wantScore: 400,
			wantLabel: "two pair",
		},
		{
			name: "one pair",
			hand: []models.PokerCard{
				{Rank: "6", Suit: "S"}, {Rank: "6", Suit: "H"}, {Rank: "2", Suit: "D"}, {Rank: "9", Suit: "C"}, {Rank: "K", Suit: "S"},
			},
			wantScore: 300,
			wantLabel: "one pair",
		},
		{
			name: "high card",
			hand: []models.PokerCard{
				{Rank: "2", Suit: "S"}, {Rank: "5", Suit: "H"}, {Rank: "8", Suit: "D"}, {Rank: "J", Suit: "C"}, {Rank: "A", Suit: "H"},
			},
			wantScore: 100,
			wantLabel: "high card",
		},
		{
			name: "ace-low straight flush",
			hand: []models.PokerCard{
				{Rank: "A", Suit: "C"}, {Rank: "2", Suit: "C"}, {Rank: "3", Suit: "C"}, {Rank: "4", Suit: "C"}, {Rank: "5", Suit: "C"},
			},
			wantScore: 800,
			wantLabel: "straight flush",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, label := evaluatePokerHand(tt.hand)
			if score != tt.wantScore || label != tt.wantLabel {
				t.Errorf("evaluatePokerHand() = (%d, %q), want (%d, %q)", score, label, tt.wantScore, tt.wantLabel)
			}
		})
	}
}

func TestIsPokerStraight(t *testing.T) {
	tests := []struct {
		name string
		hand []models.PokerCard
		want bool
	}{
		{
			name: "normal straight",
			hand: []models.PokerCard{
				{Rank: "3"}, {Rank: "4"}, {Rank: "5"}, {Rank: "6"}, {Rank: "7"},
			},
			want: true,
		},
		{
			name: "ace-low straight",
			hand: []models.PokerCard{
				{Rank: "A"}, {Rank: "2"}, {Rank: "3"}, {Rank: "4"}, {Rank: "5"},
			},
			want: true,
		},
		{
			name: "ace-high straight",
			hand: []models.PokerCard{
				{Rank: "10"}, {Rank: "J"}, {Rank: "Q"}, {Rank: "K"}, {Rank: "A"},
			},
			want: true,
		},
		{
			name: "not a straight (gap)",
			hand: []models.PokerCard{
				{Rank: "2"}, {Rank: "3"}, {Rank: "5"}, {Rank: "6"}, {Rank: "7"},
			},
			want: false,
		},
		{
			name: "not a straight (pair)",
			hand: []models.PokerCard{
				{Rank: "4"}, {Rank: "4"}, {Rank: "5"}, {Rank: "6"}, {Rank: "7"},
			},
			want: false,
		},
		{
			name: "not a straight (wrap K-A-2)",
			hand: []models.PokerCard{
				{Rank: "Q"}, {Rank: "K"}, {Rank: "A"}, {Rank: "2"}, {Rank: "3"},
			},
			want: false,
		},
		{
			name: "wrong number of cards",
			hand: []models.PokerCard{
				{Rank: "3"}, {Rank: "4"}, {Rank: "5"}, {Rank: "6"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPokerStraight(tt.hand); got != tt.want {
				t.Errorf("isPokerStraight() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ===========================================================================
// HTTP Handler Integration Tests
// ===========================================================================
//
// These tests exercise the full HTTP handler path via s.mux.ServeHTTP.
// Since NewServer(nil) runs in pure in-memory mode (no auth middleware),
// we inject auth claims directly into the request context using
// authussy.UserContextKey.

// injectAuth returns a new request with the given user claims injected into context.
func injectAuth(r *http.Request, userID, username string) *http.Request {
	claims := &authussy.Claims{UserID: userID, Username: username}
	ctx := context.WithValue(r.Context(), authussy.UserContextKey, claims)
	return r.WithContext(ctx)
}

// jsonBody returns a bytes.Reader for a JSON-encoded request body.
func jsonBody(v interface{}) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// decodeJSON decodes the response body into the provided value and fails the test on error.
func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("failed to decode response body: %v\nbody: %s", err, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 1. POST /api/stables — Create Stable
// ---------------------------------------------------------------------------

func TestHTTP_CreateStable_Success(t *testing.T) {
	s := NewServer(nil)

	body := jsonBody(map[string]string{"name": "Test Ranch"})
	req := httptest.NewRequest(http.MethodPost, "/api/stables", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/stables: status = %d, want 201\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp models.Stable
	decodeJSON(t, rr, &resp)
	if resp.Name != "Test Ranch" {
		t.Fatalf("stable name = %q, want %q", resp.Name, "Test Ranch")
	}
	if resp.OwnerID != "user-1" {
		t.Fatalf("stable ownerID = %q, want %q", resp.OwnerID, "user-1")
	}
	if len(resp.Horses) != starterHorseCount {
		t.Fatalf("expected %d starter horses, got %d", starterHorseCount, len(resp.Horses))
	}
}

func TestHTTP_CreateStable_DuplicateReject(t *testing.T) {
	s := NewServer(nil)

	// Create first stable.
	body := jsonBody(map[string]string{"name": "First"})
	req := httptest.NewRequest(http.MethodPost, "/api/stables", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first stable: status = %d, want 201", rr.Code)
	}

	// Attempt second stable — should be 409 Conflict.
	body2 := jsonBody(map[string]string{"name": "Second"})
	req2 := httptest.NewRequest(http.MethodPost, "/api/stables", body2)
	req2.Header.Set("Content-Type", "application/json")
	req2 = injectAuth(req2, "user-1", "testuser")
	rr2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("second stable: status = %d, want 409\nbody: %s", rr2.Code, rr2.Body.String())
	}
}

func TestHTTP_CreateStable_MissingName(t *testing.T) {
	s := NewServer(nil)

	body := jsonBody(map[string]string{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/stables", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty name: status = %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// 2. GET /api/leaderboard — Public Endpoint
// ---------------------------------------------------------------------------

func TestHTTP_Leaderboard_NoAuth(t *testing.T) {
	s := NewServer(nil)

	// Create a stable with horses so the leaderboard has data.
	_, err := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/leaderboard", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/leaderboard: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var entries []models.LeaderboardEntry
	decodeJSON(t, rr, &entries)
	// At minimum we have the "House of USSY" stable and our user stable.
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 leaderboard entries, got %d", len(entries))
	}
}

func TestHTTP_Leaderboard_SortParam(t *testing.T) {
	s := NewServer(nil)
	_, _ = s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	for _, sortBy := range []string{"elo", "wins", "earnings"} {
		req := httptest.NewRequest(http.MethodGet, "/api/leaderboard?sort="+sortBy, nil)
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /api/leaderboard?sort=%s: status = %d, want 200", sortBy, rr.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. POST /api/breed — Breeding
// ---------------------------------------------------------------------------

func TestHTTP_Breed_RequiresAuth(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	body := jsonBody(map[string]string{
		"sireID":   stable.Horses[0].ID,
		"mareID":   stable.Horses[1].ID,
		"stableID": stable.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/breed", body)
	req.Header.Set("Content-Type", "application/json")
	// No auth injected!

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/breed without auth: status = %d, want 401\nbody: %s", rr.Code, rr.Body.String())
	}
}

func TestHTTP_Breed_Success(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Breeding Farm", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}
	if len(stable.Horses) < 2 {
		t.Fatalf("expected at least 2 starter horses, got %d", len(stable.Horses))
	}

	sireID := stable.Horses[0].ID
	mareID := stable.Horses[1].ID

	body := jsonBody(map[string]string{
		"sireID":   sireID,
		"mareID":   mareID,
		"stableID": stable.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/breed", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/breed: status = %d, want 201\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Foal           *models.Horse `json:"foal"`
		BloodlineBonus float64       `json:"bloodlineBonus"`
	}
	decodeJSON(t, rr, &resp)
	if resp.Foal == nil {
		t.Fatal("expected foal in response")
	}
	if resp.Foal.Name == "" {
		t.Fatal("foal should have a name")
	}
	if resp.Foal.Generation != 1 {
		t.Fatalf("foal generation = %d, want 1", resp.Foal.Generation)
	}
}

func TestHTTP_Breed_WrongOwner(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	body := jsonBody(map[string]string{
		"sireID":   stable.Horses[0].ID,
		"mareID":   stable.Horses[1].ID,
		"stableID": stable.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/breed", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-2", "otheruser") // Wrong user

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST /api/breed with wrong owner: status = %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 4. POST /api/casino/chips/exchange — Casino Chip Exchange
// ---------------------------------------------------------------------------

func TestHTTP_CasinoExchange_RequiresAuth(t *testing.T) {
	s := NewServer(nil)

	body := jsonBody(map[string]interface{}{
		"direction": "buy",
		"amount":    10,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/casino/chips/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	// No auth

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/casino/chips/exchange without auth: status = %d, want 401", rr.Code)
	}
}

func TestHTTP_CasinoExchange_BuyChips(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Casino Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	// Ensure the stable has enough cummies for exchange.
	// The protected floor is 500 cummies, exchange rate is 25 cummies/chip.
	// Buying 2 chips costs 50 cummies. Stable starts with 1000 cummies by default.
	stable.Cummies = 2000

	body := jsonBody(map[string]interface{}{
		"direction": "buy",
		"amount":    2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/casino/chips/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/casino/chips/exchange buy: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Cummies     int64 `json:"cummies"`
		CasinoChips int64 `json:"casinoChips"`
	}
	decodeJSON(t, rr, &resp)
	// 2 chips at 25 cummies each = 50 cost; also daily grant may add 40 chips
	if resp.Cummies > 2000-50+1 { // allow for rounding
		t.Fatalf("cummies = %d, expected around %d", resp.Cummies, 2000-50)
	}
	if resp.CasinoChips < 2 {
		t.Fatalf("casinoChips = %d, expected at least 2", resp.CasinoChips)
	}
}

func TestHTTP_CasinoExchange_InsufficientCummies(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Broke Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	// Set cummies just above the floor — buying even 1 chip would breach it.
	stable.Cummies = casinoProtectedCummiesFloor + 10 // 510

	body := jsonBody(map[string]interface{}{
		"direction": "buy",
		"amount":    100, // cost = 100 * 25 = 2500, way over budget
	})
	req := httptest.NewRequest(http.MethodPost, "/api/casino/chips/exchange", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("buy with insufficient cummies: status = %d, want 400\nbody: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 5. POST /api/market — Create Market Listing
// ---------------------------------------------------------------------------

func TestHTTP_CreateListing_Success(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Seller Ranch", "user-1", true)
	if err != nil {
		t.Fatalf("createOwnedStable failed: %v", err)
	}

	horseID := stable.Horses[0].ID

	body := jsonBody(map[string]interface{}{
		"horseID": horseID,
		"price":   100,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/market", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/market: status = %d, want 201\nbody: %s", rr.Code, rr.Body.String())
	}

	var listing models.StudListing
	decodeJSON(t, rr, &listing)
	if listing.HorseID != horseID {
		t.Fatalf("listing horseID = %q, want %q", listing.HorseID, horseID)
	}
	if listing.Price != 100 {
		t.Fatalf("listing price = %d, want 100", listing.Price)
	}
	if !listing.Active {
		t.Fatal("listing should be active")
	}
}

func TestHTTP_CreateListing_WrongOwner(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	body := jsonBody(map[string]interface{}{
		"horseID": stable.Horses[0].ID,
		"price":   100,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/market", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-2", "otheruser") // Not the owner

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("create listing wrong owner: status = %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 6. DELETE /api/market/{id} — Delist (ownership enforcement)
// ---------------------------------------------------------------------------

func TestHTTP_DelistListing_OwnerSuccess(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	horse, _ := s.stables.GetHorse(stable.Horses[0].ID)
	listing, err := s.market.CreateListing(horse, "user-1", 100)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/market/"+listing.ID, nil)
	req.SetPathValue("id", listing.ID)
	req = injectAuth(req, "user-1", "testuser")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE /api/market/{id} by owner: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	decodeJSON(t, rr, &resp)
	if resp["status"] != "delisted" {
		t.Fatalf("expected status=delisted, got %q", resp["status"])
	}
}

func TestHTTP_DelistListing_WrongOwnerForbidden(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	horse, _ := s.stables.GetHorse(stable.Horses[0].ID)
	listing, err := s.market.CreateListing(horse, "user-1", 100)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/market/"+listing.ID, nil)
	req.SetPathValue("id", listing.ID)
	req = injectAuth(req, "user-2", "attacker") // Not the owner!

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("DELETE /api/market/{id} by wrong user: status = %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	}
}

func TestHTTP_DelistListing_NoAuthRequiresOwnerID(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	horse, _ := s.stables.GetHorse(stable.Horses[0].ID)
	listing, err := s.market.CreateListing(horse, "user-1", 100)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	// No auth, no ownerID → should be rejected
	req := httptest.NewRequest(http.MethodDelete, "/api/market/"+listing.ID, nil)
	req.SetPathValue("id", listing.ID)

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE /api/market/{id} without auth or ownerID: status = %d, want 401\nbody: %s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 7. POST /api/market/{id}/buy — Buy a Listing
// ---------------------------------------------------------------------------

func TestHTTP_BuyListing_Success(t *testing.T) {
	s := NewServer(nil)

	// Seller creates a stable and lists a horse.
	sellerStable, _ := s.createOwnedStable(context.Background(), "Seller Ranch", "user-seller", true)
	sellerHorse, _ := s.stables.GetHorse(sellerStable.Horses[0].ID)
	listing, err := s.market.CreateListing(sellerHorse, "user-seller", 50)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	// Buyer creates a stable with enough cummies.
	buyerStable, _ := s.createOwnedStable(context.Background(), "Buyer Ranch", "user-buyer", true)
	buyerStable.Cummies = 5000

	buyerMareID := buyerStable.Horses[0].ID

	body := jsonBody(map[string]interface{}{
		"buyerStableID": buyerStable.ID,
		"mareID":        buyerMareID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/market/"+listing.ID+"/buy", body)
	req.SetPathValue("id", listing.ID)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-buyer", "buyer")

	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/market/{id}/buy: status = %d, want 201\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Foal        *models.Horse             `json:"foal"`
		Transaction *models.MarketTransaction `json:"transaction"`
	}
	decodeJSON(t, rr, &resp)
	if resp.Foal == nil {
		t.Fatal("expected foal in buy response")
	}
	if resp.Foal.Name == "" {
		t.Fatal("foal should have a name")
	}
}

// ---------------------------------------------------------------------------
// 8. GET /api/stables/{id} — Get Stable
// ---------------------------------------------------------------------------

func TestHTTP_GetStable_Success(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Test Ranch", "user-1", true)

	req := httptest.NewRequest(http.MethodGet, "/api/stables/"+stable.ID, nil)
	req.SetPathValue("id", stable.ID)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/stables/{id}: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp models.Stable
	decodeJSON(t, rr, &resp)
	if resp.ID != stable.ID {
		t.Fatalf("stable ID = %q, want %q", resp.ID, stable.ID)
	}
	if resp.Name != "Test Ranch" {
		t.Fatalf("stable name = %q, want %q", resp.Name, "Test Ranch")
	}
}

func TestHTTP_GetStable_NotFound(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/stables/nonexistent-id", nil)
	req.SetPathValue("id", "nonexistent-id")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /api/stables/nonexistent: status = %d, want 404", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// 9. GET /api/market — List Market Listings
// ---------------------------------------------------------------------------

func TestHTTP_ListMarket_Empty(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/market", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/market: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var listings []models.StudListing
	decodeJSON(t, rr, &listings)
	if len(listings) != 0 {
		t.Fatalf("expected 0 listings, got %d", len(listings))
	}
}

func TestHTTP_ListMarket_WithListings(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)
	horse, _ := s.stables.GetHorse(stable.Horses[0].ID)
	_, err := s.market.CreateListing(horse, "user-1", 200)
	if err != nil {
		t.Fatalf("CreateListing failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/market", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/market: status = %d, want 200", rr.Code)
	}

	var listings []models.StudListing
	decodeJSON(t, rr, &listings)
	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}
	if listings[0].Price != 200 {
		t.Fatalf("listing price = %d, want 200", listings[0].Price)
	}
}

// ---------------------------------------------------------------------------
// 10. GET /api/capabilities — Public Endpoint via mux
// ---------------------------------------------------------------------------

func TestHTTP_Capabilities_ViaMux(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/capabilities: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var body struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	decodeJSON(t, rr, &body)
	if !body.Capabilities["casino"] {
		t.Fatal("expected casino capability to be true")
	}
}

// ---------------------------------------------------------------------------
// 11. GET /api/stables — List Stables
// ---------------------------------------------------------------------------

func TestHTTP_ListStables(t *testing.T) {
	s := NewServer(nil)
	_, _ = s.createOwnedStable(context.Background(), "Ranch1", "user-1", true)
	_, _ = s.createOwnedStable(context.Background(), "Ranch2", "user-2", true)

	req := httptest.NewRequest(http.MethodGet, "/api/stables", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/stables: status = %d, want 200", rr.Code)
	}

	var stables []*models.Stable
	decodeJSON(t, rr, &stables)
	// House of USSY + 2 user stables = at least 3
	if len(stables) < 3 {
		t.Fatalf("expected at least 3 stables, got %d", len(stables))
	}
}

// ---------------------------------------------------------------------------
// 12. GET /api/stables/{id}/horses — List Stable Horses
// ---------------------------------------------------------------------------

func TestHTTP_ListStableHorses(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	req := httptest.NewRequest(http.MethodGet, "/api/stables/"+stable.ID+"/horses", nil)
	req.SetPathValue("id", stable.ID)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/stables/{id}/horses: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var horses []*models.Horse
	decodeJSON(t, rr, &horses)
	if len(horses) != starterHorseCount {
		t.Fatalf("expected %d horses, got %d", starterHorseCount, len(horses))
	}
}

// ---------------------------------------------------------------------------
// 13. GET /api/casino — Casino Overview (auth required)
// ---------------------------------------------------------------------------

func TestHTTP_CasinoOverview_RequiresAuth(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/casino", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/casino without auth: status = %d, want 401", rr.Code)
	}
}

func TestHTTP_CasinoOverview_Success(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Casino Ranch", "user-1", true)
	stable.CasinoChips = 100

	req := httptest.NewRequest(http.MethodGet, "/api/casino", nil)
	req = injectAuth(req, "user-1", "testuser")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/casino: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	decodeJSON(t, rr, &resp)
	if _, ok := resp["stableID"]; !ok {
		t.Fatal("expected stableID in casino overview response")
	}
	if _, ok := resp["exchangeRate"]; !ok {
		t.Fatal("expected exchangeRate in casino overview response")
	}
}

// ---------------------------------------------------------------------------
// 14. POST /api/casino/slots/spin — Slot Machine (auth required)
// ---------------------------------------------------------------------------

func TestHTTP_SlotSpin_RequiresAuth(t *testing.T) {
	s := NewServer(nil)

	body := jsonBody(map[string]interface{}{"wager": 10})
	req := httptest.NewRequest(http.MethodPost, "/api/casino/slots/spin", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/casino/slots/spin without auth: status = %d, want 401", rr.Code)
	}
}

func TestHTTP_SlotSpin_Success(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Slots Ranch", "user-1", true)
	stable.CasinoChips = 500

	body := jsonBody(map[string]interface{}{"wager": 10})
	req := httptest.NewRequest(http.MethodPost, "/api/casino/slots/spin", body)
	req.Header.Set("Content-Type", "application/json")
	req = injectAuth(req, "user-1", "testuser")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/casino/slots/spin: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp models.SlotSpin
	decodeJSON(t, rr, &resp)
	if resp.WagerAmount != 10 {
		t.Fatalf("wager = %d, want 10", resp.WagerAmount)
	}
	if len(resp.Symbols) != 5 {
		t.Fatalf("expected 5 symbols (middle row), got %d", len(resp.Symbols))
	}
	if len(resp.Reels) != 5 {
		t.Fatalf("expected 5 reels, got %d", len(resp.Reels))
	}
	if resp.Lines != 9 {
		t.Fatalf("expected 9 lines (default), got %d", resp.Lines)
	}
	if resp.TotalPayout != resp.PayoutAmount {
		t.Fatalf("totalPayout (%d) != payoutAmount (%d)", resp.TotalPayout, resp.PayoutAmount)
	}
}

// ---------------------------------------------------------------------------
// 15. GET /api/horses — Horse Leaderboard (public)
// ---------------------------------------------------------------------------

func TestHTTP_ListHorses(t *testing.T) {
	s := NewServer(nil)
	_, _ = s.createOwnedStable(context.Background(), "Ranch", "user-1", true)

	req := httptest.NewRequest(http.MethodGet, "/api/horses", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/horses: status = %d, want 200", rr.Code)
	}

	var horses []*models.Horse
	decodeJSON(t, rr, &horses)
	// At minimum: 12 legendary + 2 starter = 14
	if len(horses) < 14 {
		t.Fatalf("expected at least 14 horses, got %d", len(horses))
	}
}

// ---------------------------------------------------------------------------
// 16. GET /api/horses/{id} — Get Single Horse (public)
// ---------------------------------------------------------------------------

func TestHTTP_GetHorse_Success(t *testing.T) {
	s := NewServer(nil)
	stable, _ := s.createOwnedStable(context.Background(), "Ranch", "user-1", true)
	horseID := stable.Horses[0].ID

	req := httptest.NewRequest(http.MethodGet, "/api/horses/"+horseID, nil)
	req.SetPathValue("id", horseID)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/horses/{id}: status = %d, want 200\nbody: %s", rr.Code, rr.Body.String())
	}

	var horse models.Horse
	decodeJSON(t, rr, &horse)
	if horse.ID != horseID {
		t.Fatalf("horse ID = %q, want %q", horse.ID, horseID)
	}
}

func TestHTTP_GetHorse_NotFound(t *testing.T) {
	s := NewServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/horses/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /api/horses/nonexistent: status = %d, want 404", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Marker: verify that the "bytes", "strings", "fmt" imports are used.
// ---------------------------------------------------------------------------

// These exist purely to satisfy the compiler for imports used in test helpers.
var _ = fmt.Sprintf
var _ = strings.HasPrefix
var _ = bytes.NewReader
