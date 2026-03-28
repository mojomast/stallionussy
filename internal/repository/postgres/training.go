package postgres

import (
	"context"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.TrainingSessionRepository = (*TrainingSessionRepo)(nil)

// TrainingSessionRepo implements repository.TrainingSessionRepository backed by PostgreSQL.
type TrainingSessionRepo struct {
	db *DB
}

// NewTrainingSessionRepo returns a new TrainingSessionRepo.
func NewTrainingSessionRepo(db *DB) *TrainingSessionRepo {
	return &TrainingSessionRepo{db: db}
}

// trainingCols is the canonical column list for training_sessions queries.
const trainingCols = `
	id, horse_id, workout_type, xp_gained, fitness_before,
	fitness_after, fatigue_after, injured, injury_note, created_at`

// SaveSession persists a single training session.
func (r *TrainingSessionRepo) SaveSession(ctx context.Context, session *models.TrainingSession) error {
	query := `
		INSERT INTO training_sessions (
			id, horse_id, workout_type, xp_gained, fitness_before,
			fitness_after, fatigue_after, injured, injury_note, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.db.ExecContext(ctx, query,
		session.ID,
		session.HorseID,
		string(session.WorkoutType),
		session.XPGained,
		session.FitnessBefore,
		session.FitnessAfter,
		session.FatigueAfter,
		session.Injury,
		session.InjuryNote,
		session.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save training session: %w", err)
	}
	return nil
}

// GetSessionsByHorse returns all training sessions for a given horse, newest first.
func (r *TrainingSessionRepo) GetSessionsByHorse(ctx context.Context, horseID string) ([]*models.TrainingSession, error) {
	query := `SELECT ` + trainingCols + ` FROM training_sessions WHERE horse_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, horseID)
	if err != nil {
		return nil, fmt.Errorf("get sessions by horse: %w", err)
	}
	defer rows.Close()

	var sessions []*models.TrainingSession
	for rows.Next() {
		s, err := scanTrainingSession(rows)
		if err != nil {
			return nil, fmt.Errorf("get sessions by horse scan: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get sessions by horse rows: %w", err)
	}
	return sessions, nil
}

// GetRecentSessions returns the most recent training sessions across all horses.
func (r *TrainingSessionRepo) GetRecentSessions(ctx context.Context, limit int) ([]*models.TrainingSession, error) {
	query := `SELECT ` + trainingCols + ` FROM training_sessions ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.TrainingSession
	for rows.Next() {
		s, err := scanTrainingSession(rows)
		if err != nil {
			return nil, fmt.Errorf("get recent sessions scan: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get recent sessions rows: %w", err)
	}
	return sessions, nil
}

// scanTrainingSession scans a single training session row.
func scanTrainingSession(sc interface{ Scan(dest ...any) error }) (*models.TrainingSession, error) {
	s := &models.TrainingSession{}
	var workoutType string

	err := sc.Scan(
		&s.ID,
		&s.HorseID,
		&workoutType,
		&s.XPGained,
		&s.FitnessBefore,
		&s.FitnessAfter,
		&s.FatigueAfter,
		&s.Injury,
		&s.InjuryNote,
		&s.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	s.WorkoutType = models.WorkoutType(workoutType)
	return s, nil
}
