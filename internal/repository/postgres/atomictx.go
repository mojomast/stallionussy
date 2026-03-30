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
		// 1. Update trade status to "accepted".
		_, err := tx.ExecContext(ctx, `
			UPDATE trade_offers
			SET status = $2, updated_at = $3
			WHERE id = $1`,
			trade.ID, trade.Status, trade.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("update trade status: %w", err)
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

			_, err = tx.ExecContext(ctx, `
				UPDATE stables SET cummies = cummies + $1
				WHERE id = $2`,
				trade.Price, trade.FromStableID,
			)
			if err != nil {
				return fmt.Errorf("credit cummies to seller: %w", err)
			}
		}

		// 3. Move horse from seller stable to buyer stable.
		res, err := tx.ExecContext(ctx, `
			UPDATE horses SET owner_id = $1
			WHERE id = $2 AND owner_id = $3`,
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
// single database transaction: update auction status, pay the seller (minus
// tax), and move the horse from seller to buyer. If any step fails the entire
// operation is rolled back.
//
// Parameters:
//   - auction: the auction being settled (already mutated with final status, tax, etc.)
//   - sellerStableID: the seller's stable ID
//   - buyerStableID: the buyer's stable ID
//   - sellerPayout: the amount credited to the seller (bid minus tax)
//   - newOwnerID: the buyer's user ID to set on the horse
func (d *DB) SettleAuctionAtomically(
	ctx context.Context,
	auction *models.Auction,
	sellerStableID, buyerStableID string,
	sellerPayout int64,
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
		// 1. Update auction status to "sold" with tax info.
		_, err := tx.ExecContext(ctx, `
			UPDATE auctions SET
				status = $2, current_bid = $3, bidder_id = $4,
				bidder_name = $5, bid_count = $6, bid_history = $7,
				completed_at = $8, geoffrussy_tax = $9
			WHERE id = $1`,
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

		// 2. Credit the seller (payout = bid - tax).
		if sellerPayout > 0 && sellerStableID != "" {
			_, err = tx.ExecContext(ctx, `
				UPDATE stables SET cummies = cummies + $1
				WHERE id = $2`,
				sellerPayout, sellerStableID,
			)
			if err != nil {
				return fmt.Errorf("credit seller: %w", err)
			}
		}

		// 3. Move the horse to the buyer's stable and update owner.
		if buyerStableID != "" {
			_, err = tx.ExecContext(ctx, `
				UPDATE horses SET owner_id = $1
				WHERE id = $2`,
				newOwnerID, auction.HorseID,
			)
			if err != nil {
				return fmt.Errorf("move horse to buyer: %w", err)
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
