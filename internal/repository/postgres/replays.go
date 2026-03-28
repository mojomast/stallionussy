package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.RaceReplayRepository = (*ReplayRepo)(nil)

// ReplayRepo implements repository.RaceReplayRepository backed by PostgreSQL.
type ReplayRepo struct {
	db *DB
}

// NewReplayRepo returns a new ReplayRepo.
func NewReplayRepo(db *DB) *ReplayRepo {
	return &ReplayRepo{db: db}
}

// replayCols is the canonical column list for race_replays queries.
const replayCols = `race_id, track_type, distance, purse, entries, weather, winner_id, winner_name, data, created_at`

// scanReplay scans a single race_replays row.
func scanReplay(sc interface{ Scan(dest ...any) error }) (*models.RaceReplay, error) {
	r := &models.RaceReplay{}
	err := sc.Scan(
		&r.RaceID,
		&r.TrackType,
		&r.Distance,
		&r.Purse,
		&r.Entries,
		&r.Weather,
		&r.WinnerID,
		&r.WinnerName,
		&r.Data,
		&r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// SaveReplay persists a full race replay using upsert (INSERT ON CONFLICT UPDATE).
func (r *ReplayRepo) SaveReplay(ctx context.Context, replay *models.RaceReplay) error {
	query := `
		INSERT INTO race_replays (race_id, track_type, distance, purse, entries, weather, winner_id, winner_name, data, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (race_id) DO UPDATE SET
			data = EXCLUDED.data,
			track_type = EXCLUDED.track_type,
			distance = EXCLUDED.distance,
			purse = EXCLUDED.purse,
			entries = EXCLUDED.entries,
			weather = EXCLUDED.weather,
			winner_id = EXCLUDED.winner_id,
			winner_name = EXCLUDED.winner_name`
	_, err := r.db.db.ExecContext(ctx, query,
		replay.RaceID,
		replay.TrackType,
		replay.Distance,
		replay.Purse,
		replay.Entries,
		replay.Weather,
		replay.WinnerID,
		replay.WinnerName,
		replay.Data,
		replay.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save replay: %w", err)
	}
	return nil
}

// GetReplay retrieves a single race replay by race ID.
func (r *ReplayRepo) GetReplay(ctx context.Context, raceID string) (*models.RaceReplay, error) {
	query := `SELECT ` + replayCols + ` FROM race_replays WHERE race_id = $1`
	replay, err := scanReplay(r.db.db.QueryRowContext(ctx, query, raceID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("replay not found: %s", raceID)
	}
	if err != nil {
		return nil, fmt.Errorf("get replay: %w", err)
	}
	return replay, nil
}

// ListRecentReplays returns the most recent race replays, newest first.
func (r *ReplayRepo) ListRecentReplays(ctx context.Context, limit int) ([]*models.RaceReplay, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT ` + replayCols + ` FROM race_replays ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent replays: %w", err)
	}
	defer rows.Close()

	var replays []*models.RaceReplay
	for rows.Next() {
		replay, err := scanReplay(rows)
		if err != nil {
			return nil, fmt.Errorf("list recent replays scan: %w", err)
		}
		replays = append(replays, replay)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent replays rows: %w", err)
	}
	return replays, nil
}

// DeleteOldReplays removes replays older than the given cutoff time.
// Returns the number of rows deleted.
func (r *ReplayRepo) DeleteOldReplays(ctx context.Context, olderThan time.Time) (int64, error) {
	query := `DELETE FROM race_replays WHERE created_at < $1`
	result, err := r.db.db.ExecContext(ctx, query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("delete old replays: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete old replays rows affected: %w", err)
	}
	return count, nil
}
