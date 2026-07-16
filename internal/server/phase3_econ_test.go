package server

// Phase 3 integration tests — economy wiring for the MEDIUM findings.
// These exercise the full HTTP handler → domain → (in-memory) repository path
// using the real server wiring, per the Phase 2 test conventions.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTP_ExchangeChips_FullRoundTrip covers M-8: the buy/cashout rates are
// asymmetric by design (house edge), so both the overview and the exchange
// response must disclose them, and the balances must move at exactly those
// rates.
func TestHTTP_ExchangeChips_FullRoundTrip(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Rate Ranch", "user-ex", true)
	if err != nil {
		t.Fatalf("createOwnedStable: %v", err)
	}

	startCummies := stable.Cummies
	startChips := stable.CasinoChips

	// Buy 10 chips: costs 10 × 25 = 250 cummies.
	buyReq := httptest.NewRequest(http.MethodPost, "/api/casino/chips/exchange",
		jsonBody(map[string]any{"direction": "buy", "amount": 10}))
	buyReq.Header.Set("Content-Type", "application/json")
	buyReq = injectAuth(buyReq, "user-ex", "exuser")
	buyRR := httptest.NewRecorder()
	s.mux.ServeHTTP(buyRR, buyReq)
	if buyRR.Code != http.StatusOK {
		t.Fatalf("buy: status = %d, want 200\nbody: %s", buyRR.Code, buyRR.Body.String())
	}

	var buyResp struct {
		Cummies      int64 `json:"cummies"`
		CasinoChips  int64 `json:"casinoChips"`
		ExchangeRate int64 `json:"exchangeRate"`
		CashoutRate  int64 `json:"cashoutRate"`
	}
	decodeJSON(t, buyRR, &buyResp)
	if buyResp.ExchangeRate != casinoExchangeRate || buyResp.CashoutRate != casinoChipCashoutRate {
		t.Fatalf("rates not disclosed: exchange=%d cashout=%d", buyResp.ExchangeRate, buyResp.CashoutRate)
	}
	// The exchange handler may also claim the daily chip grant; only assert
	// on cummies (the grant is chips-only) plus a chips lower bound.
	if got, want := buyResp.Cummies, startCummies-10*casinoExchangeRate; got != want {
		t.Fatalf("cummies after buy = %d, want %d", got, want)
	}
	if buyResp.CasinoChips < startChips+10 {
		t.Fatalf("chips after buy = %d, want >= %d", buyResp.CasinoChips, startChips+10)
	}

	// Cash out 10 chips: credits 10 × 10 = 100 cummies.
	outReq := httptest.NewRequest(http.MethodPost, "/api/casino/chips/exchange",
		jsonBody(map[string]any{"direction": "cashout", "amount": 10}))
	outReq.Header.Set("Content-Type", "application/json")
	outReq = injectAuth(outReq, "user-ex", "exuser")
	outRR := httptest.NewRecorder()
	s.mux.ServeHTTP(outRR, outReq)
	if outRR.Code != http.StatusOK {
		t.Fatalf("cashout: status = %d, want 200\nbody: %s", outRR.Code, outRR.Body.String())
	}
	var outResp struct {
		Cummies     int64 `json:"cummies"`
		CasinoChips int64 `json:"casinoChips"`
	}
	decodeJSON(t, outRR, &outResp)
	if got, want := outResp.Cummies, buyResp.Cummies+10*casinoChipCashoutRate; got != want {
		t.Fatalf("cummies after cashout = %d, want %d", got, want)
	}
	if got, want := outResp.CasinoChips, buyResp.CasinoChips-10; got != want {
		t.Fatalf("chips after cashout = %d, want %d", got, want)
	}
}

// TestDailyChipGrantFundedByHouse covers M-1: the 40-chip daily grant is no
// longer minted from nothing — the House of USSY treasury pays its cashout
// value (400 cummies), and a broke house grants nothing without consuming
// the player's day.
func TestDailyChipGrantFundedByHouse(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Grant Ranch", "user-grant", true)
	if err != nil {
		t.Fatalf("createOwnedStable: %v", err)
	}
	house := s.houseStable()
	if house == nil {
		t.Fatal("house stable missing")
	}

	houseBefore := house.Cummies
	chipsBefore := stable.CasinoChips
	if !s.maybeGrantDailyCasinoChips(context.Background(), "user-grant", stable) {
		t.Fatal("expected first grant of the day to succeed")
	}
	if got, want := stable.CasinoChips, chipsBefore+casinoDailyChipGrant; got != want {
		t.Fatalf("chips = %d, want %d", got, want)
	}
	if got, want := house.Cummies, houseBefore-casinoDailyChipGrant*casinoChipCashoutRate; got != want {
		t.Fatalf("house treasury = %d, want %d (grant must be house-funded)", got, want)
	}

	// Same day: no second grant, no second debit.
	if s.maybeGrantDailyCasinoChips(context.Background(), "user-grant", stable) {
		t.Fatal("second grant on the same day should be refused")
	}

	// Broke house: the grant is withheld and the day is NOT consumed.
	s2 := NewServer(nil)
	stable2, _ := s2.createOwnedStable(context.Background(), "Broke Ranch", "user-broke", true)
	house2 := s2.houseStable()
	house2.Cummies = 0
	if s2.maybeGrantDailyCasinoChips(context.Background(), "user-broke", stable2) {
		t.Fatal("broke house should not grant chips")
	}
	// Refill the house — the player can now claim (day was not burned).
	house2.Cummies = houseTreasurySeed
	if !s2.maybeGrantDailyCasinoChips(context.Background(), "user-broke", stable2) {
		t.Fatal("grant should succeed once the house is solvent again")
	}
}

// TestDebitHouseChips covers the M-2 jackpot funding primitive: chip funding
// comes out of the house treasury at cashout value and never overdraws it.
func TestDebitHouseChips(t *testing.T) {
	s := NewServer(nil)
	house := s.houseStable()
	if house == nil {
		t.Fatal("house stable missing")
	}

	house.Cummies = 10 * casinoChipCashoutRate
	if got := s.debitHouseChips(context.Background(), 4); got != 4 {
		t.Fatalf("funded = %d, want 4", got)
	}
	if got, want := house.Cummies, int64(6*casinoChipCashoutRate); got != want {
		t.Fatalf("house = %d, want %d", got, want)
	}

	// Request more than the house can afford: partial funding, change returned.
	if got := s.debitHouseChips(context.Background(), 100); got != 6 {
		t.Fatalf("funded = %d, want 6 (house-bounded)", got)
	}
	if house.Cummies != 0 {
		t.Fatalf("house = %d, want 0", house.Cummies)
	}

	// Empty house: nothing funded.
	if got := s.debitHouseChips(context.Background(), 10); got != 0 {
		t.Fatalf("funded = %d, want 0", got)
	}
}
