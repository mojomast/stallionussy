package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.PlayerProgressRepository = (*ProgressRepo)(nil)

// ProgressRepo implements repository.PlayerProgressRepository backed by PostgreSQL.
type ProgressRepo struct {
	db *DB
}

// NewProgressRepo returns a new ProgressRepo.
func NewProgressRepo(db *DB) *ProgressRepo {
	return &ProgressRepo{db: db}
}

const progressCols = `
	user_id, login_streak, last_login_date, total_logins,
	daily_trains_left, daily_races_left, last_daily_reset,
	prestige_level, prestige_xp, lifetime_earnings`

func scanProgress(sc interface{ Scan(dest ...any) error }) (*models.PlayerProgress, error) {
	p := &models.PlayerProgress{}
	err := sc.Scan(
		&p.UserID,
		&p.LoginStreak,
		&p.LastLoginDate,
		&p.TotalLogins,
		&p.DailyTrainsLeft,
		&p.DailyRacesLeft,
		&p.LastDailyReset,
		&p.PrestigeLevel,
		&p.PrestigeXP,
		&p.LifetimeEarnings,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// CreateProgress persists a new player progress record.
func (r *ProgressRepo) CreateProgress(ctx context.Context, progress *models.PlayerProgress) error {
	query := `
		INSERT INTO player_progress (
			user_id, login_streak, last_login_date, total_logins,
			daily_trains_left, daily_races_left, last_daily_reset,
			prestige_level, prestige_xp, lifetime_earnings
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.db.ExecContext(ctx, query,
		progress.UserID,
		progress.LoginStreak,
		progress.LastLoginDate,
		progress.TotalLogins,
		progress.DailyTrainsLeft,
		progress.DailyRacesLeft,
		progress.LastDailyReset,
		progress.PrestigeLevel,
		progress.PrestigeXP,
		progress.LifetimeEarnings,
	)
	if err != nil {
		return fmt.Errorf("create progress: %w", err)
	}
	return nil
}

// GetProgress retrieves a player's progress by user ID.
func (r *ProgressRepo) GetProgress(ctx context.Context, userID string) (*models.PlayerProgress, error) {
	query := `SELECT ` + progressCols + ` FROM player_progress WHERE user_id = $1`
	p, err := scanProgress(r.db.db.QueryRowContext(ctx, query, userID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("progress not found: %s", userID)
	}
	if err != nil {
		return nil, fmt.Errorf("get progress: %w", err)
	}
	return p, nil
}

// ListProgress returns all player progress records.
func (r *ProgressRepo) ListProgress(ctx context.Context) ([]*models.PlayerProgress, error) {
	query := `SELECT ` + progressCols + ` FROM player_progress ORDER BY user_id ASC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list progress: %w", err)
	}
	defer rows.Close()

	var progress []*models.PlayerProgress
	for rows.Next() {
		p, err := scanProgress(rows)
		if err != nil {
			return nil, fmt.Errorf("list progress scan: %w", err)
		}
		progress = append(progress, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list progress rows: %w", err)
	}
	return progress, nil
}

// UpdateProgress saves changes to an existing player progress record.
func (r *ProgressRepo) UpdateProgress(ctx context.Context, progress *models.PlayerProgress) error {
	query := `
		UPDATE player_progress SET
			login_streak = $2,
			last_login_date = $3,
			total_logins = $4,
			daily_trains_left = $5,
			daily_races_left = $6,
			last_daily_reset = $7,
			prestige_level = $8,
			prestige_xp = $9,
			lifetime_earnings = $10
		WHERE user_id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		progress.UserID,
		progress.LoginStreak,
		progress.LastLoginDate,
		progress.TotalLogins,
		progress.DailyTrainsLeft,
		progress.DailyRacesLeft,
		progress.LastDailyReset,
		progress.PrestigeLevel,
		progress.PrestigeXP,
		progress.LifetimeEarnings,
	)
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update progress rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("progress not found: %s", progress.UserID)
	}
	return nil
}
