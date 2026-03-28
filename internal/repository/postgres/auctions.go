package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.AuctionRepository = (*AuctionRepo)(nil)

// AuctionRepo implements repository.AuctionRepository backed by PostgreSQL.
type AuctionRepo struct {
	db *DB
}

// NewAuctionRepo returns a new AuctionRepo.
func NewAuctionRepo(db *DB) *AuctionRepo {
	return &AuctionRepo{db: db}
}

// auctionCols is the canonical column list for auctions queries.
const auctionCols = `
	id, seller_id, seller_name, stable_id, horse_id, horse_name,
	starting_bid, current_bid, bidder_id, bidder_name, bid_count,
	bid_history, status, duration, created_at, expires_at,
	completed_at, geoffrussy_tax`

// scanAuction scans a single auction row. bid_history is stored as JSONB
// and unmarshalled here.
func scanAuction(sc interface{ Scan(dest ...any) error }) (*models.Auction, error) {
	a := &models.Auction{}
	var (
		bidHistoryJSON []byte
		completedAt    sql.NullTime
	)

	err := sc.Scan(
		&a.ID,
		&a.SellerID,
		&a.SellerName,
		&a.StableID,
		&a.HorseID,
		&a.HorseName,
		&a.StartingBid,
		&a.CurrentBid,
		&a.BidderID,
		&a.BidderName,
		&a.BidCount,
		&bidHistoryJSON,
		&a.Status,
		&a.Duration,
		&a.CreatedAt,
		&a.ExpiresAt,
		&completedAt,
		&a.GeoffrussyTax,
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSONB bid history.
	if len(bidHistoryJSON) > 0 {
		if err := json.Unmarshal(bidHistoryJSON, &a.BidHistory); err != nil {
			return nil, fmt.Errorf("unmarshal bid_history: %w", err)
		}
	}
	if a.BidHistory == nil {
		a.BidHistory = []models.AuctionBid{}
	}

	if completedAt.Valid {
		a.CompletedAt = completedAt.Time
	}

	return a, nil
}

// CreateAuction persists a new auction.
func (r *AuctionRepo) CreateAuction(ctx context.Context, auction *models.Auction) error {
	bidHistoryJSON, err := json.Marshal(auction.BidHistory)
	if err != nil {
		return fmt.Errorf("marshal bid_history: %w", err)
	}

	var completedAt sql.NullTime
	if !auction.CompletedAt.IsZero() {
		completedAt = sql.NullTime{Time: auction.CompletedAt, Valid: true}
	}

	query := `
		INSERT INTO auctions (
			id, seller_id, seller_name, stable_id, horse_id, horse_name,
			starting_bid, current_bid, bidder_id, bidder_name, bid_count,
			bid_history, status, duration, created_at, expires_at,
			completed_at, geoffrussy_tax
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
	_, err = r.db.db.ExecContext(ctx, query,
		auction.ID,
		auction.SellerID,
		auction.SellerName,
		auction.StableID,
		auction.HorseID,
		auction.HorseName,
		auction.StartingBid,
		auction.CurrentBid,
		auction.BidderID,
		auction.BidderName,
		auction.BidCount,
		bidHistoryJSON,
		auction.Status,
		auction.Duration,
		auction.CreatedAt,
		auction.ExpiresAt,
		completedAt,
		auction.GeoffrussyTax,
	)
	if err != nil {
		return fmt.Errorf("create auction: %w", err)
	}
	return nil
}

// GetAuction retrieves an auction by ID.
func (r *AuctionRepo) GetAuction(ctx context.Context, id string) (*models.Auction, error) {
	query := `SELECT ` + auctionCols + ` FROM auctions WHERE id = $1`
	a, err := scanAuction(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("auction not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get auction: %w", err)
	}
	return a, nil
}

// ListActiveAuctions returns all auctions with status "open" or "ending".
func (r *AuctionRepo) ListActiveAuctions(ctx context.Context) ([]*models.Auction, error) {
	query := `SELECT ` + auctionCols + ` FROM auctions
		WHERE status IN ('open', 'ending')
		ORDER BY expires_at ASC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active auctions: %w", err)
	}
	defer rows.Close()

	var auctions []*models.Auction
	for rows.Next() {
		a, err := scanAuction(rows)
		if err != nil {
			return nil, fmt.Errorf("list active auctions scan: %w", err)
		}
		auctions = append(auctions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active auctions rows: %w", err)
	}
	return auctions, nil
}

// ListAuctionsByUser returns all auctions where the user is the seller or
// current highest bidder.
func (r *AuctionRepo) ListAuctionsByUser(ctx context.Context, userID string) ([]*models.Auction, error) {
	query := `SELECT ` + auctionCols + ` FROM auctions
		WHERE seller_id = $1 OR bidder_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list auctions by user: %w", err)
	}
	defer rows.Close()

	var auctions []*models.Auction
	for rows.Next() {
		a, err := scanAuction(rows)
		if err != nil {
			return nil, fmt.Errorf("list auctions by user scan: %w", err)
		}
		auctions = append(auctions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list auctions by user rows: %w", err)
	}
	return auctions, nil
}

// UpdateAuction saves changes to an existing auction.
func (r *AuctionRepo) UpdateAuction(ctx context.Context, auction *models.Auction) error {
	bidHistoryJSON, err := json.Marshal(auction.BidHistory)
	if err != nil {
		return fmt.Errorf("marshal bid_history: %w", err)
	}

	var completedAt sql.NullTime
	if !auction.CompletedAt.IsZero() {
		completedAt = sql.NullTime{Time: auction.CompletedAt, Valid: true}
	}

	query := `
		UPDATE auctions SET
			seller_id = $2, seller_name = $3, stable_id = $4,
			horse_id = $5, horse_name = $6, starting_bid = $7,
			current_bid = $8, bidder_id = $9, bidder_name = $10,
			bid_count = $11, bid_history = $12, status = $13,
			duration = $14, expires_at = $15, completed_at = $16,
			geoffrussy_tax = $17
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		auction.ID,
		auction.SellerID,
		auction.SellerName,
		auction.StableID,
		auction.HorseID,
		auction.HorseName,
		auction.StartingBid,
		auction.CurrentBid,
		auction.BidderID,
		auction.BidderName,
		auction.BidCount,
		bidHistoryJSON,
		auction.Status,
		auction.Duration,
		auction.ExpiresAt,
		completedAt,
		auction.GeoffrussyTax,
	)
	if err != nil {
		return fmt.Errorf("update auction: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update auction rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("auction not found: %s", auction.ID)
	}
	return nil
}
