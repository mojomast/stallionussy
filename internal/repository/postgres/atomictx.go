package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
)

// ---------------------------------------------------------------------------
// Transactional multi-step mutations
//
// These methods wrap critical multi-step operations in a single SQL
// transaction so that partial failures never leave the database in an
// inconsistent state. Each method calls DB.WithTx internally.
// ---------------------------------------------------------------------------

// AcceptTradeAtomically performs the entire trade acceptance flow inside a
// single database transaction: update trade status, transfer cummies between
// stables, and move the horse to the buyer's stable. If any step fails the
// entire operation is rolled back.
//
// The caller is still responsible for updating in-memory state before or after
// this call — this method only touches the database.
func (d *DB) AcceptTradeAtomically(ctx context.Context, trade *models.TradeOffer) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Update trade status to "accepted". Guard on the previous status
		// (H-2): a trade that no longer exists or was already accepted must
		// abort the whole transaction before any money moves.
		res, err := tx.ExecContext(ctx, `
			UPDATE trade_offers
			SET status = $2, updated_at = $3
			WHERE id = $1 AND status = 'Pending'`,
			trade.ID, trade.Status, trade.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("update trade status: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("trade %s not found or no longer pending", trade.ID)
		}

		// 2. Transfer cummies: deduct from buyer (ToStable), credit seller (FromStable).
		if trade.Price > 0 {
			res, err := tx.ExecContext(ctx, `
				UPDATE stables SET cummies = cummies - $1
				WHERE id = $2 AND cummies >= $1`,
				trade.Price, trade.ToStableID,
			)
			if err != nil {
				return fmt.Errorf("deduct cummies from buyer: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("buyer stable %s has insufficient cummies for trade", trade.ToStableID)
			}

			res, err = tx.ExecContext(ctx, `
				UPDATE stables SET cummies = cummies + $1
				WHERE id = $2`,
				trade.Price, trade.FromStableID,
			)
			if err != nil {
				return fmt.Errorf("credit cummies to seller: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("seller stable %s not found", trade.FromStableID)
			}
		}

		// 3. Move horse from seller stable to buyer stable.
		//
		// BUG FIX (C-4): horses.owner_id stores USER IDs, not stable IDs (see
		// stableussy.AddHorseToStable and the hydration query, which lists
		// horses by the stable's OwnerID). The old query filtered and wrote
		// stable IDs, matched zero rows, and rolled back the ENTIRE
		// transaction — while the in-memory trade had already completed. On
		// restart the DB still showed the trade Pending with the buyer's
		// cummies intact and the horse unmoved: duplication. Resolve the
		// stable IDs to their owners' user IDs inside the transaction.
		res, err = tx.ExecContext(ctx, `
			UPDATE horses
			SET owner_id = (SELECT owner_id FROM stables WHERE id = $1)
			WHERE id = $2
			  AND owner_id = (SELECT owner_id FROM stables WHERE id = $3)`,
			trade.ToStableID, trade.HorseID, trade.FromStableID,
		)
		if err != nil {
			return fmt.Errorf("move horse: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("horse %s not found in seller stable %s", trade.HorseID, trade.FromStableID)
		}

		return nil
	})
}

// SettleAuctionAtomically performs the entire auction settlement flow inside a
// single database transaction: update auction status, commit the seller's and
// buyer's final balances, and move the horse from seller to buyer. If any step
// fails the entire operation is rolled back.
//
// BUG FIX (H-1): the buyer's balance is now written inside the same
// transaction as the seller's credit. The buyer's escrow happens in memory at
// bid time with a best-effort persist; if that persist was lost, the old
// settlement minted the seller payout from nothing. Writing both final
// balances atomically (matching the write-through architecture, where the
// in-memory state is the source of truth) closes that hole. All statements
// now check RowsAffected, and the status update is guarded so an auction can
// only be settled once.
//
// Parameters:
//   - auction: the auction being settled (already mutated with final status, tax, etc.)
//   - sellerStableID / sellerFinalBalance: seller stable and its post-payout balance
//   - buyerStableID / buyerFinalBalance: buyer stable and its post-escrow balance
//   - newOwnerID: the buyer's user ID to set on the horse
func (d *DB) SettleAuctionAtomically(
	ctx context.Context,
	auction *models.Auction,
	sellerStableID string, sellerFinalBalance int64,
	buyerStableID string, buyerFinalBalance int64,
	newOwnerID string,
) error {
	bidHistoryJSON, err := json.Marshal(auction.BidHistory)
	if err != nil {
		return fmt.Errorf("marshal bid_history: %w", err)
	}
	var completedAt sql.NullTime
	if !auction.CompletedAt.IsZero() {
		completedAt = sql.NullTime{Time: auction.CompletedAt, Valid: true}
	}

	return d.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Update auction status to "sold" with tax info. Guarded on the
		// previous status so a settlement can never be applied twice (H-1).
		res, err := tx.ExecContext(ctx, `
			UPDATE auctions SET
				status = $2, current_bid = $3, bidder_id = $4,
				bidder_name = $5, bid_count = $6, bid_history = $7,
				completed_at = $8, geoffrussy_tax = $9
			WHERE id = $1 AND status IN ('open', 'ending')`,
			auction.ID,
			auction.Status,
			auction.CurrentBid,
			auction.BidderID,
			auction.BidderName,
			auction.BidCount,
			bidHistoryJSON,
			completedAt,
			auction.GeoffrussyTax,
		)
		if err != nil {
			return fmt.Errorf("update auction: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("auction %s not found or already settled", auction.ID)
		}

		// 2. Commit the seller's final balance (includes the payout applied
		// in memory by the caller).
		if sellerStableID != "" {
			res, err = tx.ExecContext(ctx, `
				UPDATE stables SET cummies = $1
				WHERE id = $2`,
				sellerFinalBalance, sellerStableID,
			)
			if err != nil {
				return fmt.Errorf("credit seller: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("seller stable %s not found", sellerStableID)
			}
		}

		// 3. Commit the buyer's final balance (reflects the bid escrow).
		if buyerStableID != "" {
			res, err = tx.ExecContext(ctx, `
				UPDATE stables SET cummies = $1
				WHERE id = $2`,
				buyerFinalBalance, buyerStableID,
			)
			if err != nil {
				return fmt.Errorf("debit buyer: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("buyer stable %s not found", buyerStableID)
			}
		}

		// 4. Move the horse to the buyer's stable and update owner.
		if buyerStableID != "" {
			res, err = tx.ExecContext(ctx, `
				UPDATE horses SET owner_id = $1
				WHERE id = $2`,
				newOwnerID, auction.HorseID,
			)
			if err != nil {
				return fmt.Errorf("move horse to buyer: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("auction horse %s not found", auction.HorseID)
			}
		}

		return nil
	})
}

// SettlePokerAtomically performs the poker table settlement inside a single
// database transaction: mark the table as settled and credit the winner's
// stable with the pot. If any step fails the entire operation is rolled back.
//
// Parameters:
//   - table: the poker table being settled (already mutated with final status)
//   - winnerStableID: the winning player's stable ID
//   - pot: the total pot to award
//   - currency: "cummies" or "casino_chips"
func (d *DB) SettlePokerAtomically(
	ctx context.Context,
	table *models.PokerTable,
	winnerStableID string,
	pot int64,
	currency string,
) error {
	seatsJSON, err := json.Marshal(table.Seats)
	if err != nil {
		return fmt.Errorf("marshal seats: %w", err)
	}
	logJSON, err := json.Marshal(table.Log)
	if err != nil {
		return fmt.Errorf("marshal log: %w", err)
	}

	return d.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Update the poker table to settled status.
		_, err := tx.ExecContext(ctx, `
			UPDATE poker_tables SET
				status = $2, pot = $3, seats = $4,
				log = $5, updated_at = $6
			WHERE id = $1`,
			table.ID,
			table.Status,
			table.Pot,
			seatsJSON,
			logJSON,
			table.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("update poker table: %w", err)
		}

		// 2. Credit the winner's stable.
		if winnerStableID != "" && pot > 0 {
			col := "casino_chips"
			if currency == "cummies" {
				col = "cummies"
			}
			// col is always one of two known string constants, so this is safe
			// from injection. We build the query dynamically because column
			// names cannot be parameterised in prepared statements.
			query := fmt.Sprintf(`UPDATE stables SET %s = %s + $1 WHERE id = $2`, col, col)
			_, err = tx.ExecContext(ctx, query, pot, winnerStableID)
			if err != nil {
				return fmt.Errorf("credit winner: %w", err)
			}
		}

		return nil
	})
}

// ExpireAuctionAtomically marks an auction as expired inside a transaction.
// This is simpler than settlement (no money or horse movement), but we still
// wrap it for consistency.
func (d *DB) ExpireAuctionAtomically(ctx context.Context, auction *models.Auction) error {
	var completedAt sql.NullTime
	if !auction.CompletedAt.IsZero() {
		completedAt = sql.NullTime{Time: auction.CompletedAt, Valid: true}
	}

	return d.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE auctions SET status = $2, completed_at = $3
			WHERE id = $1`,
			auction.ID, auction.Status, completedAt,
		)
		if err != nil {
			return fmt.Errorf("expire auction: %w", err)
		}
		return nil
	})
}
