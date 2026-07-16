package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.BettingPoolRepository = (*BettingPoolRepo)(nil)

// BettingPoolRepo implements repository.BettingPoolRepository.
// Horses and bets are stored as JSON blobs, matching the codebase's
// convention for nested aggregates (tournament standings, poker seats, ...).
type BettingPoolRepo struct {
	db *DB
}

// NewBettingPoolRepo returns a new BettingPoolRepo.
func NewBettingPoolRepo(db *DB) *BettingPoolRepo {
	return &BettingPoolRepo{db: db}
}

// SavePool upserts the full pool state.
func (r *BettingPoolRepo) SavePool(ctx context.Context, pool *models.BettingPool) error {
	horsesJSON, err := json.Marshal(pool.Horses)
	if err != nil {
		return fmt.Errorf("marshal pool horses: %w", err)
	}
	betsJSON, err := json.Marshal(pool.Bets)
	if err != nil {
		return fmt.Errorf("marshal pool bets: %w", err)
	}
	_, err = r.db.db.ExecContext(ctx, `
		INSERT INTO betting_pools (
			race_id, status, kind, horses, bets, total_pool, house_cut,
			opened_at, closed_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (race_id) DO UPDATE SET
			status     = EXCLUDED.status,
			kind       = EXCLUDED.kind,
			horses     = EXCLUDED.horses,
			bets       = EXCLUDED.bets,
			total_pool = EXCLUDED.total_pool,
			house_cut  = EXCLUDED.house_cut,
			closed_at  = EXCLUDED.closed_at,
			updated_at = EXCLUDED.updated_at`,
		pool.RaceID, pool.Status, pool.Kind, horsesJSON, betsJSON,
		pool.TotalPool, pool.HouseCut, pool.OpenedAt,
		nullableTime(pool.ClosedAt), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("save betting pool: %w", err)
	}
	return nil
}

// GetPool retrieves a pool by race ID. Returns nil, nil when absent.
func (r *BettingPoolRepo) GetPool(ctx context.Context, raceID string) (*models.BettingPool, error) {
	row := r.db.db.QueryRowContext(ctx, `
		SELECT race_id, status, kind, horses, bets, total_pool, house_cut,
		       opened_at, closed_at
		FROM betting_pools WHERE race_id = $1`, raceID)
	pool, err := scanBettingPool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return pool, err
}

// ListUnresolvedPools returns every pool still carrying live escrow.
func (r *BettingPoolRepo) ListUnresolvedPools(ctx context.Context) ([]*models.BettingPool, error) {
	rows, err := r.db.db.QueryContext(ctx, `
		SELECT race_id, status, kind, horses, bets, total_pool, house_cut,
		       opened_at, closed_at
		FROM betting_pools
		WHERE status IN ('open', 'closed')
		ORDER BY opened_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list unresolved pools: %w", err)
	}
	defer rows.Close()

	var out []*models.BettingPool
	for rows.Next() {
		pool, err := scanBettingPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pool)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanBettingPool(row rowScanner) (*models.BettingPool, error) {
	pool := &models.BettingPool{}
	var horsesJSON, betsJSON []byte
	var closedAt sql.NullTime
	if err := row.Scan(
		&pool.RaceID, &pool.Status, &pool.Kind, &horsesJSON, &betsJSON,
		&pool.TotalPool, &pool.HouseCut, &pool.OpenedAt, &closedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("scan betting pool: %w", err)
	}
	if closedAt.Valid {
		pool.ClosedAt = closedAt.Time
	}
	if err := json.Unmarshal(horsesJSON, &pool.Horses); err != nil {
		return nil, fmt.Errorf("unmarshal pool horses: %w", err)
	}
	if err := json.Unmarshal(betsJSON, &pool.Bets); err != nil {
		return nil, fmt.Errorf("unmarshal pool bets: %w", err)
	}
	return pool, nil
}
