package main

import (
	"testing"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/pedigreussy"
	"github.com/mojomast/stallionussy/internal/stableussy"
)

// newTestCLIState builds an in-memory cliState (no DB) for command tests.
func newTestCLIState() *cliState {
	sm := stableussy.NewStableManager()
	return &cliState{
		sm:     sm,
		trades: pedigreussy.NewTradeManager(),
	}
}

// TestCmdAccept_MovesHorseAndMoney is the H-11 regression: accepting a trade
// in the CLI used to transfer the cummies but never the horse — the buyer
// paid full price and the seller kept the horse.
func TestCmdAccept_MovesHorseAndMoney(t *testing.T) {
	state := newTestCLIState()

	seller := state.sm.CreateStable("Seller Ranch", "seller")
	buyer := state.sm.CreateStable("Buyer Ranch", "buyer")

	horse := &models.Horse{ID: "horse-1", Name: "Trademussy", ELO: 1200}
	if err := state.sm.AddHorseToStable(seller.ID, horse); err != nil {
		t.Fatalf("AddHorseToStable: %v", err)
	}

	const price = int64(400)
	offer := state.trades.CreateOffer(horse.ID, horse.Name, seller.ID, buyer.ID, price)

	sellerBefore := seller.Cummies
	buyerBefore := buyer.Cummies

	cmdAccept(state, []string{offer.ID})

	// Money must have moved buyer -> seller.
	if buyer.Cummies != buyerBefore-price {
		t.Fatalf("buyer balance = %d, want %d", buyer.Cummies, buyerBefore-price)
	}
	if seller.Cummies != sellerBefore+price {
		t.Fatalf("seller balance = %d, want %d", seller.Cummies, sellerBefore+price)
	}

	// The horse must have moved with the money (H-11).
	moved, err := state.sm.GetHorse(horse.ID)
	if err != nil {
		t.Fatalf("GetHorse after trade: %v", err)
	}
	if moved.OwnerID != buyer.OwnerID {
		t.Fatalf("horse owner = %q after trade, want %q — CLI moved the money but not the horse (H-11)",
			moved.OwnerID, buyer.OwnerID)
	}
	buyerHorses := state.sm.ListHorses(buyer.ID)
	if len(buyerHorses) != 1 || buyerHorses[0].ID != horse.ID {
		t.Fatalf("buyer stable roster = %d horses, want the traded horse", len(buyerHorses))
	}
	if sellerHorses := state.sm.ListHorses(seller.ID); len(sellerHorses) != 0 {
		t.Fatalf("seller stable still holds %d horses after trade", len(sellerHorses))
	}
}

// TestCmdAccept_RefundsWhenHorseGone: if the horse can't be transferred the
// payment must be rolled back instead of leaving the buyer poorer with no
// horse.
func TestCmdAccept_RefundsWhenHorseGone(t *testing.T) {
	state := newTestCLIState()

	seller := state.sm.CreateStable("Seller Ranch", "seller")
	buyer := state.sm.CreateStable("Buyer Ranch", "buyer")

	horse := &models.Horse{ID: "horse-2", Name: "Vanishussy", ELO: 1200}
	if err := state.sm.AddHorseToStable(seller.ID, horse); err != nil {
		t.Fatalf("AddHorseToStable: %v", err)
	}
	offer := state.trades.CreateOffer(horse.ID, horse.Name, seller.ID, buyer.ID, 300)

	// The horse disappears (glue factory, fatality...) before acceptance.
	if err := state.sm.RemoveHorse(horse.ID); err != nil {
		t.Fatalf("RemoveHorse: %v", err)
	}

	buyerBefore := buyer.Cummies
	sellerBefore := seller.Cummies

	cmdAccept(state, []string{offer.ID})

	if buyer.Cummies != buyerBefore {
		t.Fatalf("buyer balance = %d after failed trade, want %d (payment not refunded)", buyer.Cummies, buyerBefore)
	}
	if seller.Cummies != sellerBefore {
		t.Fatalf("seller balance = %d after failed trade, want %d", seller.Cummies, sellerBefore)
	}
}
