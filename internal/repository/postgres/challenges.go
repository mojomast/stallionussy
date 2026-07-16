package postgres

import (
	"context"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.ChallengeRepository = (*ChallengeRepo)(nil)

// ChallengeRepo implements repository.ChallengeRepository.
type ChallengeRepo struct {
	db *DB
}

// NewChallengeRepo returns a new ChallengeRepo.
func NewChallengeRepo(db *DB) *ChallengeRepo {
	return &ChallengeRepo{db: db}
}

// SaveChallenge upserts a challenge row (creation or any status change).
func (r *ChallengeRepo) SaveChallenge(ctx context.Context, c *models.Challenge) error {
	_, err := r.db.db.ExecContext(ctx, `
		INSERT INTO challenges (
			id, challenger_id, challenger_name, challenger_horse,
			challenger_horse_name, defender_id, defender_name,
			defender_horse, defender_horse_name, wager, status,
			created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			defender_id         = EXCLUDED.defender_id,
			defender_name       = EXCLUDED.defender_name,
			defender_horse      = EXCLUDED.defender_horse,
			defender_horse_name = EXCLUDED.defender_horse_name,
			status              = EXCLUDED.status`,
		c.ID, c.ChallengerID, c.ChallengerName, c.ChallengerHorse,
		c.ChallengerHorseName, c.DefenderID, c.DefenderName,
		c.DefenderHorse, c.DefenderHorseName, c.Wager, c.Status,
		c.CreatedAt, c.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save challenge: %w", err)
	}
	return nil
}

// ListChallenges returns the most recent challenges, newest first.
func (r *ChallengeRepo) ListChallenges(ctx context.Context, limit int) ([]*models.Challenge, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.db.QueryContext(ctx, `
		SELECT id, challenger_id, challenger_name, challenger_horse,
		       challenger_horse_name, defender_id, defender_name,
		       defender_horse, defender_horse_name, wager, status,
		       created_at, expires_at
		FROM challenges
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	defer rows.Close()

	var out []*models.Challenge
	for rows.Next() {
		c := &models.Challenge{}
		if err := rows.Scan(
			&c.ID, &c.ChallengerID, &c.ChallengerName, &c.ChallengerHorse,
			&c.ChallengerHorseName, &c.DefenderID, &c.DefenderName,
			&c.DefenderHorse, &c.DefenderHorseName, &c.Wager, &c.Status,
			&c.CreatedAt, &c.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan challenge: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
