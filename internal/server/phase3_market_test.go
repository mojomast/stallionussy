package server

// Phase 3 integration tests — stud-market burn, glue-factory funding, and
// the previously-unobtainable achievements (Part B wiring + economy leaks).

import (
	"context"
	"net/http"
	"testing"
)

// TestHTTP_BuyListing_BurnActuallyBurns: the 2% "deflationary burn" was
// really a buyer discount — the buyer paid price-burn and the seller got
// price-burn, so no cummies ever left the economy. Now the buyer pays the
// full price and exactly the burn amount vanishes.
func TestHTTP_BuyListing_BurnActuallyBurns(t *testing.T) {
	s := NewServer(nil)
	seller, err := s.createOwnedStable(context.Background(), "Stud Farm", "user-stud", true)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	buyer, err := s.createOwnedStable(context.Background(), "Mare Farm", "user-mare", true)
	if err != nil {
		t.Fatalf("create buyer: %v", err)
	}

	const price = int64(1000)
	burn := max(int64(1), price*2/100) // 20

	listRR := postJSON(t, s, "/api/market", map[string]any{
		"horseID": seller.Horses[0].ID, "price": price,
	}, "user-stud", "stud")
	if listRR.Code != http.StatusCreated {
		t.Fatalf("create listing: status = %d\nbody: %s", listRR.Code, listRR.Body.String())
	}
	var listing struct {
		ID string `json:"id"`
	}
	decodeJSON(t, listRR, &listing)

	// first_sale must unlock on the first listing (was never granted before).
	if !hasAchievement(seller.Achievements, "first_sale") {
		t.Fatal("first_sale achievement not granted on listing")
	}

	totalBefore := totalCummies(s)
	buyerBefore := buyer.Cummies
	sellerBefore := seller.Cummies

	buyRR := postJSON(t, s, "/api/market/"+listing.ID+"/buy", map[string]any{
		"buyerStableID": buyer.ID, "mareID": buyer.Horses[0].ID,
	}, "user-mare", "mare")
	if buyRR.Code != http.StatusCreated {
		t.Fatalf("buy listing: status = %d\nbody: %s", buyRR.Code, buyRR.Body.String())
	}

	if got, want := buyer.Cummies, buyerBefore-price; got != want {
		t.Fatalf("buyer paid %d, want the full price %d", buyerBefore-got, price)
	}
	if got, want := seller.Cummies, sellerBefore+price-burn; got != want {
		t.Fatalf("seller received %d, want %d (price minus burn)", got-sellerBefore, price-burn)
	}
	if got, want := totalCummies(s), totalBefore-burn; got != want {
		t.Fatalf("economy lost %d cummies, want exactly the %d burn", totalBefore-got, burn)
	}
}

// TestHTTP_Glue_HouseFundedAndFoalProof: the glue factory used to mint its
// payout from nothing, and a freshly bred foal was worth 500+ cummies — an
// infinite breed-to-glue pump. Payouts are now house-funded and foals render
// down to scraps.
func TestHTTP_Glue_HouseFundedAndFoalProof(t *testing.T) {
	s := NewServer(nil)
	stable, err := s.createOwnedStable(context.Background(), "Glue Ranch", "user-glue", true)
	if err != nil {
		t.Fatalf("create stable: %v", err)
	}
	house := s.houseStable()
	if house == nil {
		t.Fatal("house stable missing")
	}

	// Capture both starter horse IDs up front — gluing the first removes it
	// from the stable's Horses slice.
	veteranID := stable.Horses[0].ID
	foalID := stable.Horses[1].ID

	// Make the horse a proven veteran so the payout is meaningful.
	veteran, _ := s.stables.GetHorse(veteranID)
	veteran.Age = 6
	veteran.Races = 20
	veteran.Wins = 8
	veteran.ELO = 1500

	totalBefore := totalCummies(s)
	houseBefore := house.Cummies
	stableBefore := stable.Cummies

	rr := postJSON(t, s, "/api/horses/"+veteran.ID+"/glue", map[string]any{}, "user-glue", "gluer")
	if rr.Code != http.StatusOK {
		t.Fatalf("glue veteran: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}
	var res struct {
		CummiesEarned int64 `json:"cummiesEarned"`
		GlueProduced  int64 `json:"glueProduced"`
	}
	decodeJSON(t, rr, &res)

	// glue = 50 + 6*3 + 20*2 + 8*5 = 148; payout = 1480 + (1500-1200)/10 = 1510.
	if want := int64(1510); res.CummiesEarned != want {
		t.Fatalf("veteran payout = %d, want %d", res.CummiesEarned, want)
	}
	if got, want := stable.Cummies, stableBefore+res.CummiesEarned; got != want {
		t.Fatalf("stable credited %d, want %d", got-stableBefore, res.CummiesEarned)
	}
	if got, want := house.Cummies, houseBefore-res.CummiesEarned; got != want {
		t.Fatalf("house debited %d, want %d — glue must be house-funded", houseBefore-got, res.CummiesEarned)
	}
	if got := totalCummies(s); got != totalBefore {
		t.Fatalf("glue minted money: total before %d, after %d", totalBefore, got)
	}

	// A fresh foal (age 0, default 1200 ELO, no races) is nearly worthless.
	foal, _ := s.stables.GetHorse(foalID)
	foal.Age = 0
	foal.Races = 0
	foal.Wins = 0
	foal.ELO = 1200
	// Give the stable a third horse so the last-active-horse guard passes.
	spare := s.newStarterHorse("user-glue")
	if err := s.stables.AddHorseToStable(stable.ID, spare); err != nil {
		t.Fatalf("add spare horse: %v", err)
	}

	rr2 := postJSON(t, s, "/api/horses/"+foal.ID+"/glue", map[string]any{}, "user-glue", "gluer")
	if rr2.Code != http.StatusOK {
		t.Fatalf("glue foal: status = %d\nbody: %s", rr2.Code, rr2.Body.String())
	}
	var foalRes struct {
		CummiesEarned int64 `json:"cummiesEarned"`
	}
	decodeJSON(t, rr2, &foalRes)
	if want := int64(150); foalRes.CummiesEarned != want {
		t.Fatalf("foal payout = %d, want %d (breed-to-glue pump must not pay)", foalRes.CummiesEarned, want)
	}
}

// TestTournamentWinnerAchievementGranted: tournament_winner was defined but
// never granted by any handler.
func TestTournamentWinnerAchievementGranted(t *testing.T) {
	s := NewServer(nil)
	tournamentID, st1, st2 := tournamentTestSetup(t, s, 100, 1)

	rr := postJSON(t, s, "/api/tournaments/"+tournamentID+"/race", map[string]any{}, "user-1", "alpha")
	if rr.Code != http.StatusCreated {
		t.Fatalf("run round: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}

	if !hasAchievement(st1.Achievements, "tournament_winner") &&
		!hasAchievement(st2.Achievements, "tournament_winner") {
		t.Fatal("tournament finished but nobody received the tournament_winner achievement")
	}
}
