package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.AchievementRepository = (*AchievementRepo)(nil)

// AchievementRepo implements repository.AchievementRepository backed by PostgreSQL.
type AchievementRepo struct {
	db *DB
}

// NewAchievementRepo returns a new AchievementRepo.
func NewAchievementRepo(db *DB) *AchievementRepo {
	return &AchievementRepo{db: db}
}

// AddAchievement grants an achievement to a stable.
func (r *AchievementRepo) AddAchievement(ctx context.Context, stableID string, achievement *models.Achievement) error {
	query := `
		INSERT INTO achievements (achievement_id, stable_id, name, description, icon, rarity, unlocked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (stable_id, achievement_id) DO NOTHING`
	_, err := r.db.db.ExecContext(ctx, query,
		achievement.ID,
		stableID,
		achievement.Name,
		achievement.Description,
		achievement.Icon,
		achievement.Rarity,
		achievement.UnlockedAt,
	)
	if err != nil {
		return fmt.Errorf("add achievement: %w", err)
	}
	return nil
}

// GetAchievements returns all achievements unlocked by a stable.
func (r *AchievementRepo) GetAchievements(ctx context.Context, stableID string) ([]*models.Achievement, error) {
	query := `
		SELECT achievement_id, name, description, icon, rarity, unlocked_at
		FROM achievements WHERE stable_id = $1 ORDER BY unlocked_at`
	rows, err := r.db.db.QueryContext(ctx, query, stableID)
	if err != nil {
		return nil, fmt.Errorf("get achievements: %w", err)
	}
	defer rows.Close()

	var achievements []*models.Achievement
	for rows.Next() {
		a := &models.Achievement{}
		var (
			icon   sql.NullString
			rarity sql.NullString
		)
		if err := rows.Scan(
			&a.ID,
			&a.Name,
			&a.Description,
			&icon,
			&rarity,
			&a.UnlockedAt,
		); err != nil {
			return nil, fmt.Errorf("get achievements scan: %w", err)
		}
		a.Icon = icon.String
		a.Rarity = rarity.String
		achievements = append(achievements, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get achievements rows: %w", err)
	}
	return achievements, nil
}

// HasAchievement checks whether a stable has already unlocked an achievement.
func (r *AchievementRepo) HasAchievement(ctx context.Context, stableID, achievementID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM achievements WHERE stable_id = $1 AND achievement_id = $2)`
	var exists bool
	err := r.db.db.QueryRowContext(ctx, query, stableID, achievementID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has achievement: %w", err)
	}
	return exists, nil
}
