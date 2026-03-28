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
var _ repository.RaceResultRepository = (*RaceResultRepo)(nil)

// RaceResultRepo implements repository.RaceResultRepository backed by PostgreSQL.
type RaceResultRepo struct {
	db *DB
}

// NewRaceResultRepo returns a new RaceResultRepo.
func NewRaceResultRepo(db *DB) *RaceResultRepo {
	return &RaceResultRepo{db: db}
}

// raceResultCols is the canonical column list for race_results queries.
const raceResultCols = `
	race_id, horse_id, horse_name, track_type, distance,
	finish_place, total_horses, final_time_ns, elo_before, elo_after,
	earnings, weather, created_at`

// scanRaceResult scans a single race result row. FinalTime is stored as
// nanoseconds (int64) and converted to time.Duration.
func scanRaceResult(sc interface{ Scan(dest ...any) error }) (*models.RaceResult, error) {
	rr := &models.RaceResult{}
	var finalTimeNs int64
	var weather sql.NullString

	err := sc.Scan(
		&rr.RaceID,
		&rr.HorseID,
		&rr.HorseName,
		&rr.TrackType,
		&rr.Distance,
		&rr.FinishPlace,
		&rr.TotalHorses,
		&finalTimeNs,
		&rr.ELOBefore,
		&rr.ELOAfter,
		&rr.Earnings,
		&weather,
		&rr.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	rr.FinalTime = time.Duration(finalTimeNs)
	rr.Weather = weather.String

	return rr, nil
}

// RecordResult persists a single horse's result from a completed race.
// FinalTime is stored as nanoseconds (int64).
func (r *RaceResultRepo) RecordResult(ctx context.Context, result *models.RaceResult) error {
	query := `
		INSERT INTO race_results (
			race_id, horse_id, horse_name, track_type, distance,
			finish_place, total_horses, final_time_ns, elo_before, elo_after,
			earnings, weather, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	_, err := r.db.db.ExecContext(ctx, query,
		result.RaceID,
		result.HorseID,
		result.HorseName,
		result.TrackType,
		result.Distance,
		result.FinishPlace,
		result.TotalHorses,
		int64(result.FinalTime), // store as nanoseconds
		result.ELOBefore,
		result.ELOAfter,
		result.Earnings,
		toNullString(result.Weather),
		result.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record race result: %w", err)
	}
	return nil
}

// GetHorseHistory returns all race results for a given horse, newest first.
func (r *RaceResultRepo) GetHorseHistory(ctx context.Context, horseID string) ([]*models.RaceResult, error) {
	query := `SELECT ` + raceResultCols + ` FROM race_results WHERE horse_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, horseID)
	if err != nil {
		return nil, fmt.Errorf("get horse history: %w", err)
	}
	defer rows.Close()

	var results []*models.RaceResult
	for rows.Next() {
		rr, err := scanRaceResult(rows)
		if err != nil {
			return nil, fmt.Errorf("get horse history scan: %w", err)
		}
		results = append(results, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get horse history rows: %w", err)
	}
	return results, nil
}

// GetRaceResults returns all results for a given race ID.
func (r *RaceResultRepo) GetRaceResults(ctx context.Context, raceID string) ([]*models.RaceResult, error) {
	query := `SELECT ` + raceResultCols + ` FROM race_results WHERE race_id = $1 ORDER BY finish_place`
	rows, err := r.db.db.QueryContext(ctx, query, raceID)
	if err != nil {
		return nil, fmt.Errorf("get race results: %w", err)
	}
	defer rows.Close()

	var results []*models.RaceResult
	for rows.Next() {
		rr, err := scanRaceResult(rows)
		if err != nil {
			return nil, fmt.Errorf("get race results scan: %w", err)
		}
		results = append(results, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get race results rows: %w", err)
	}
	return results, nil
}

// GetRecentResults returns the most recent race results across all horses.
func (r *RaceResultRepo) GetRecentResults(ctx context.Context, limit int) ([]*models.RaceResult, error) {
	query := `SELECT ` + raceResultCols + ` FROM race_results ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent results: %w", err)
	}
	defer rows.Close()

	var results []*models.RaceResult
	for rows.Next() {
		rr, err := scanRaceResult(rows)
		if err != nil {
			return nil, fmt.Errorf("get recent results scan: %w", err)
		}
		results = append(results, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get recent results rows: %w", err)
	}
	return results, nil
}
