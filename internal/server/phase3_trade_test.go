package server

// Phase 3 integration tests — trade wiring (M-7 plus the pay-then-lose-the-
// horse gap) and the M-9 hold'em auto-fold authorization fix.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
)

// tradeTestSetup creates a seller and a buyer, each with a stable, and
// returns the server plus both stables.
func tradeTestSetup(t *testing.T) (*Server, *models.Stable, *models.Stable) {
	t.Helper()
	s := NewServer(nil)
	seller, err := s.createOwnedStable(context.Background(), "Seller Ranch", "user-seller", true)
	if err != nil {
		t.Fatalf("create seller stable: %v", err)
	}
	buyer, err := s.createOwnedStable(context.Background(), "Buyer Ranch", "user-buyer", true)
	if err != nil {
		t.Fatalf("create buyer stable: %v", err)
	}
	return s, seller, buyer
}

func postJSON(t *testing.T, s *Server, path string, body any, userID, username string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req = injectAuth(req, userID, username)
	}
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

// TestHTTP_Trade_FullPath exercises create → accept over HTTP and verifies
// both the horse and the money actually move.
func TestHTTP_Trade_FullPath(t *testing.T) {
	s, seller, buyer := tradeTestSetup(t)
	horseID := seller.Horses[0].ID
	const price = int64(500)

	rr := postJSON(t, s, "/api/trades", map[string]any{
		"horseID": horseID, "fromStable": seller.ID, "toStable": buyer.ID, "price": price,
	}, "user-seller", "seller")
	if rr.Code != http.StatusCreated {
		t.Fatalf("create trade: status = %d, want 201\nbody: %s", rr.Code, rr.Body.String())
	}
	var offer models.TradeOffer
	decodeJSON(t, rr, &offer)

	sellerBefore := seller.Cummies
	buyerBefore := buyer.Cummies

	acceptRR := postJSON(t, s, "/api/trades/"+offer.ID+"/accept", map[string]any{}, "user-buyer", "buyer")
	if acceptRR.Code != http.StatusOK {
		t.Fatalf("accept trade: status = %d, want 200\nbody: %s", acceptRR.Code, acceptRR.Body.String())
	}

	if got, want := buyer.Cummies, buyerBefore-price; got != want {
		t.Fatalf("buyer cummies = %d, want %d", got, want)
	}
	if got, want := seller.Cummies, sellerBefore+price; got != want {
		t.Fatalf("seller cummies = %d, want %d", got, want)
	}
	if hs := s.getStableForHorse(horseID); hs == nil || hs.ID != buyer.ID {
		t.Fatalf("horse did not move to the buyer stable")
	}
}

// TestHTTP_Trade_RejectsNegativePrice covers M-7.
func TestHTTP_Trade_RejectsNegativePrice(t *testing.T) {
	s, seller, buyer := tradeTestSetup(t)
	rr := postJSON(t, s, "/api/trades", map[string]any{
		"horseID": seller.Horses[0].ID, "fromStable": seller.ID, "toStable": buyer.ID, "price": -100,
	}, "user-seller", "seller")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("negative price: status = %d, want 400\nbody: %s", rr.Code, rr.Body.String())
	}
}

// TestHTTP_Trade_RejectsForeignHorse: a trade for a horse that is not in the
// source stable must be rejected at creation.
func TestHTTP_Trade_RejectsForeignHorse(t *testing.T) {
	s, seller, buyer := tradeTestSetup(t)
	// The horse belongs to the BUYER, not the seller's stable.
	rr := postJSON(t, s, "/api/trades", map[string]any{
		"horseID": buyer.Horses[0].ID, "fromStable": seller.ID, "toStable": buyer.ID, "price": 100,
	}, "user-seller", "seller")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("foreign horse: status = %d, want 400\nbody: %s", rr.Code, rr.Body.String())
	}
}

// TestHTTP_Trade_FailedPaymentReopensOffer: an accept the buyer cannot afford
// must not consume the offer or move the horse.
func TestHTTP_Trade_FailedPaymentReopensOffer(t *testing.T) {
	s, seller, buyer := tradeTestSetup(t)
	horseID := seller.Horses[0].ID
	price := buyer.Cummies + 1_000_000 // unaffordable

	rr := postJSON(t, s, "/api/trades", map[string]any{
		"horseID": horseID, "fromStable": seller.ID, "toStable": buyer.ID, "price": price,
	}, "user-seller", "seller")
	if rr.Code != http.StatusCreated {
		t.Fatalf("create trade: status = %d\nbody: %s", rr.Code, rr.Body.String())
	}
	var offer models.TradeOffer
	decodeJSON(t, rr, &offer)

	acceptRR := postJSON(t, s, "/api/trades/"+offer.ID+"/accept", map[string]any{}, "user-buyer", "buyer")
	if acceptRR.Code != http.StatusBadRequest {
		t.Fatalf("unaffordable accept: status = %d, want 400\nbody: %s", acceptRR.Code, acceptRR.Body.String())
	}

	// The offer must be pending again — not consumed by the failed accept.
	reloaded, err := s.trades.GetOffer(offer.ID)
	if err != nil {
		t.Fatalf("get offer: %v", err)
	}
	if reloaded.Status != "Pending" {
		t.Fatalf("offer status = %q, want Pending after failed payment", reloaded.Status)
	}
	// And the horse must not have moved.
	if hs := s.getStableForHorse(horseID); hs == nil || hs.ID != seller.ID {
		t.Fatalf("horse must remain with the seller after a failed payment")
	}
}

// TestHoldemActionRequiresSeatBeforeTimeoutFold covers M-9: a user who is not
// seated at the table must get a 403 and must NOT be able to trigger the
// timeout auto-fold of the acting player.
func TestHoldemActionRequiresSeatBeforeTimeoutFold(t *testing.T) {
	s := NewServer(nil)

	table := &models.PokerTable{
		ID:         "tbl-m9",
		Name:       "M9 Table",
		CreatedBy:  "user-a",
		GameType:   models.PokerGameHoldem,
		Status:     models.PokerTablePreFlop,
		MaxPlayers: 2,
		ActionSeat: 0,
		// Deadline long expired: before the fix, ANY caller triggered the fold.
		ActionDeadline: time.Now().Add(-time.Hour),
		Seats: []models.PokerSeat{
			{UserID: "user-a", Username: "alice", ChipStack: 100},
			{UserID: "user-b", Username: "bob", ChipStack: 100},
		},
	}
	s.pokerMu.Lock()
	s.pokerTables[table.ID] = table
	s.pokerMu.Unlock()

	rr := postJSON(t, s, "/api/casino/poker/"+table.ID+"/action",
		map[string]any{"action": "fold"}, "user-intruder", "mallory")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unseated action: status = %d, want 403\nbody: %s", rr.Code, rr.Body.String())
	}

	s.pokerMu.RLock()
	folded := s.pokerTables[table.ID].Seats[0].Folded
	s.pokerMu.RUnlock()
	if folded {
		t.Fatal("unseated caller must not trigger the timeout auto-fold")
	}
}
