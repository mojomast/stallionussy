package postgres

import (
	"context"
	"fmt"

	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.RivalryRepository = (*RivalryRepo)(nil)

// RivalryRepo implements repository.RivalryRepository.
type RivalryRepo struct {
	db *DB
}

// NewRivalryRepo returns a new RivalryRepo.
func NewRivalryRepo(db *DB) *RivalryRepo {
	return &RivalryRepo{db: db}
}

// IncrementRivalry adds one win for winnerID over loserID (upsert).
func (r *RivalryRepo) IncrementRivalry(ctx context.Context, winnerID, loserID string) error {
	_, err := r.db.db.ExecContext(ctx, `
		INSERT INTO rivalries (winner_id, loser_id, wins)
		VALUES ($1, $2, 1)
		ON CONFLICT (winner_id, loser_id) DO UPDATE SET
			wins = wins + 1`,
		winnerID, loserID)
	if err != nil {
		return fmt.Errorf("increment rivalry: %w", err)
	}
	return nil
}

// ListRivalries returns the full rivalry matrix.
func (r *RivalryRepo) ListRivalries(ctx context.Context) (map[string]map[string]int, error) {
	rows, err := r.db.db.QueryContext(ctx,
		`SELECT winner_id, loser_id, wins FROM rivalries`)
	if err != nil {
		return nil, fmt.Errorf("list rivalries: %w", err)
	}
	defer rows.Close()

	out := make(map[string]map[string]int)
	for rows.Next() {
		var winnerID, loserID string
		var wins int
		if err := rows.Scan(&winnerID, &loserID, &wins); err != nil {
			return nil, fmt.Errorf("scan rivalry: %w", err)
		}
		if out[winnerID] == nil {
			out[winnerID] = make(map[string]int)
		}
		out[winnerID][loserID] = wins
	}
	return out, rows.Err()
}
