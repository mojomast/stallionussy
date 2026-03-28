package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.GlueFactoryRepository = (*GlueFactoryRepo)(nil)

// GlueFactoryRepo implements repository.GlueFactoryRepository backed by PostgreSQL.
type GlueFactoryRepo struct {
	db *DB
}

// NewGlueFactoryRepo returns a new GlueFactoryRepo.
func NewGlueFactoryRepo(db *DB) *GlueFactoryRepo {
	return &GlueFactoryRepo{db: db}
}

// RecordGlue persists a glue factory result.
func (r *GlueFactoryRepo) RecordGlue(ctx context.Context, result *models.GlueFactoryResult, ownerID, stableID string) error {
	id := uuid.New().String()
	query := `
		INSERT INTO glue_factory (
			id, horse_id, horse_name, owner_id, stable_id,
			glue_produced, cummies_earned, bonus_material, bonus_amount,
			eulogy, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	_, err := r.db.db.ExecContext(ctx, query,
		id,
		result.HorseID,
		result.HorseName,
		ownerID,
		stableID,
		result.GlueProduced,
		result.CummiesEarned,
		result.BonusMaterial,
		result.BonusAmount,
		result.Eulogy,
		result.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record glue: %w", err)
	}
	return nil
}

// GetStableGlueHistory returns all glue factory records for a stable.
func (r *GlueFactoryRepo) GetStableGlueHistory(ctx context.Context, stableID string) ([]*models.GlueFactoryResult, error) {
	query := `
		SELECT horse_id, horse_name, glue_produced, cummies_earned,
			bonus_material, bonus_amount, eulogy, created_at
		FROM glue_factory
		WHERE stable_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, stableID)
	if err != nil {
		return nil, fmt.Errorf("get stable glue history: %w", err)
	}
	defer rows.Close()

	var results []*models.GlueFactoryResult
	for rows.Next() {
		g := &models.GlueFactoryResult{}
		err := rows.Scan(
			&g.HorseID,
			&g.HorseName,
			&g.GlueProduced,
			&g.CummiesEarned,
			&g.BonusMaterial,
			&g.BonusAmount,
			&g.Eulogy,
			&g.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("get stable glue history scan: %w", err)
		}
		results = append(results, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get stable glue history rows: %w", err)
	}
	return results, nil
}

// GetTotalGlueProduced returns the total glue produced across all stables.
func (r *GlueFactoryRepo) GetTotalGlueProduced(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(glue_produced), 0) FROM glue_factory`,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get total glue produced: %w", err)
	}
	return total, nil
}
