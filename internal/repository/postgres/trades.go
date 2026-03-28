package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.TradeRepository = (*TradeRepo)(nil)

// TradeRepo implements repository.TradeRepository backed by PostgreSQL.
type TradeRepo struct {
	db *DB
}

// NewTradeRepo returns a new TradeRepo.
func NewTradeRepo(db *DB) *TradeRepo {
	return &TradeRepo{db: db}
}

// tradeCols is the canonical column list for trade_offers queries.
const tradeCols = `
	id, horse_id, horse_name, from_stable_id, to_stable_id,
	price, status, created_at, updated_at`

// scanTrade scans a single trade offer row.
func scanTrade(sc interface{ Scan(dest ...any) error }) (*models.TradeOffer, error) {
	t := &models.TradeOffer{}
	var horseName sql.NullString

	err := sc.Scan(
		&t.ID,
		&t.HorseID,
		&horseName,
		&t.FromStableID,
		&t.ToStableID,
		&t.Price,
		&t.Status,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.HorseName = horseName.String
	return t, nil
}

// CreateTrade persists a new trade offer.
func (r *TradeRepo) CreateTrade(ctx context.Context, trade *models.TradeOffer) error {
	query := `
		INSERT INTO trade_offers (
			id, horse_id, horse_name, from_stable_id, to_stable_id,
			price, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.db.ExecContext(ctx, query,
		trade.ID,
		trade.HorseID,
		toNullString(trade.HorseName),
		trade.FromStableID,
		trade.ToStableID,
		trade.Price,
		trade.Status,
		trade.CreatedAt,
		trade.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create trade: %w", err)
	}
	return nil
}

// GetTrade retrieves a trade offer by ID.
func (r *TradeRepo) GetTrade(ctx context.Context, id string) (*models.TradeOffer, error) {
	query := `SELECT ` + tradeCols + ` FROM trade_offers WHERE id = $1`
	t, err := scanTrade(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("trade not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get trade: %w", err)
	}
	return t, nil
}

// ListTradesByStable returns all trades involving a given stable (either as
// sender or receiver).
func (r *TradeRepo) ListTradesByStable(ctx context.Context, stableID string) ([]*models.TradeOffer, error) {
	query := `SELECT ` + tradeCols + ` FROM trade_offers
		WHERE from_stable_id = $1 OR to_stable_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, stableID)
	if err != nil {
		return nil, fmt.Errorf("list trades by stable: %w", err)
	}
	defer rows.Close()

	var trades []*models.TradeOffer
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, fmt.Errorf("list trades by stable scan: %w", err)
		}
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list trades by stable rows: %w", err)
	}
	return trades, nil
}

// ListAllTrades returns all trade offers in the database.
func (r *TradeRepo) ListAllTrades(ctx context.Context) ([]*models.TradeOffer, error) {
	query := `SELECT ` + tradeCols + ` FROM trade_offers ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all trades: %w", err)
	}
	defer rows.Close()

	var trades []*models.TradeOffer
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, fmt.Errorf("list all trades scan: %w", err)
		}
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all trades rows: %w", err)
	}
	return trades, nil
}

// UpdateTrade saves changes to an existing trade offer (e.g. status change).
func (r *TradeRepo) UpdateTrade(ctx context.Context, trade *models.TradeOffer) error {
	query := `
		UPDATE trade_offers SET
			horse_id = $2, horse_name = $3, from_stable_id = $4, to_stable_id = $5,
			price = $6, status = $7, updated_at = $8
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		trade.ID,
		trade.HorseID,
		toNullString(trade.HorseName),
		trade.FromStableID,
		trade.ToStableID,
		trade.Price,
		trade.Status,
		trade.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update trade: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update trade rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("trade not found: %s", trade.ID)
	}
	return nil
}
