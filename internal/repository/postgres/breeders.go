package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.BreedingStallionRepository = (*BreedingStallionRepo)(nil)

// BreedingStallionRepo implements repository.BreedingStallionRepository backed by PostgreSQL.
type BreedingStallionRepo struct {
	db *DB
}

// NewBreedingStallionRepo returns a new BreedingStallionRepo.
func NewBreedingStallionRepo(db *DB) *BreedingStallionRepo {
	return &BreedingStallionRepo{db: db}
}

// breederCols is the canonical column list for breeding_stallions queries.
const breederCols = `
	horse_id, horse_name, owner_id, stable_id,
	breed_count, total_earnings, fee, cooldown_hours,
	active, assigned_at`

// scanBreeder scans a single breeding_stallions row.
func scanBreeder(sc interface{ Scan(dest ...any) error }) (*models.BreedingStallion, error) {
	b := &models.BreedingStallion{}
	err := sc.Scan(
		&b.HorseID,
		&b.HorseName,
		&b.OwnerID,
		&b.StableID,
		&b.BreedCount,
		&b.TotalEarnings,
		&b.Fee,
		&b.CooldownHours,
		&b.Active,
		&b.AssignedAt,
	)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AssignBreeder persists a new breeding stallion record.
func (r *BreedingStallionRepo) AssignBreeder(ctx context.Context, breeder *models.BreedingStallion) error {
	query := `
		INSERT INTO breeding_stallions (
			horse_id, horse_name, owner_id, stable_id,
			breed_count, total_earnings, fee, cooldown_hours,
			active, assigned_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	_, err := r.db.db.ExecContext(ctx, query,
		breeder.HorseID,
		breeder.HorseName,
		breeder.OwnerID,
		breeder.StableID,
		breeder.BreedCount,
		breeder.TotalEarnings,
		breeder.Fee,
		breeder.CooldownHours,
		breeder.Active,
		breeder.AssignedAt,
	)
	if err != nil {
		return fmt.Errorf("assign breeder: %w", err)
	}
	return nil
}

// GetBreeder retrieves a breeding stallion by horse ID.
func (r *BreedingStallionRepo) GetBreeder(ctx context.Context, horseID string) (*models.BreedingStallion, error) {
	query := `SELECT ` + breederCols + ` FROM breeding_stallions WHERE horse_id = $1`
	b, err := scanBreeder(r.db.db.QueryRowContext(ctx, query, horseID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("breeder not found: %s", horseID)
	}
	if err != nil {
		return nil, fmt.Errorf("get breeder: %w", err)
	}
	return b, nil
}

// ListActiveBreedersByOwner returns all active breeders owned by a user.
func (r *BreedingStallionRepo) ListActiveBreedersByOwner(ctx context.Context, ownerID string) ([]*models.BreedingStallion, error) {
	query := `SELECT ` + breederCols + ` FROM breeding_stallions
		WHERE owner_id = $1 AND active = TRUE
		ORDER BY assigned_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list active breeders by owner: %w", err)
	}
	defer rows.Close()

	var breeders []*models.BreedingStallion
	for rows.Next() {
		b, err := scanBreeder(rows)
		if err != nil {
			return nil, fmt.Errorf("list active breeders by owner scan: %w", err)
		}
		breeders = append(breeders, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active breeders by owner rows: %w", err)
	}
	return breeders, nil
}

// ListAllActiveBreeders returns all active breeding stallions.
func (r *BreedingStallionRepo) ListAllActiveBreeders(ctx context.Context) ([]*models.BreedingStallion, error) {
	query := `SELECT ` + breederCols + ` FROM breeding_stallions
		WHERE active = TRUE
		ORDER BY total_earnings DESC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all active breeders: %w", err)
	}
	defer rows.Close()

	var breeders []*models.BreedingStallion
	for rows.Next() {
		b, err := scanBreeder(rows)
		if err != nil {
			return nil, fmt.Errorf("list all active breeders scan: %w", err)
		}
		breeders = append(breeders, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all active breeders rows: %w", err)
	}
	return breeders, nil
}

// UpdateBreeder saves changes to an existing breeding stallion record.
func (r *BreedingStallionRepo) UpdateBreeder(ctx context.Context, breeder *models.BreedingStallion) error {
	query := `
		UPDATE breeding_stallions SET
			horse_name = $2, owner_id = $3, stable_id = $4,
			breed_count = $5, total_earnings = $6, fee = $7,
			cooldown_hours = $8, active = $9
		WHERE horse_id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		breeder.HorseID,
		breeder.HorseName,
		breeder.OwnerID,
		breeder.StableID,
		breeder.BreedCount,
		breeder.TotalEarnings,
		breeder.Fee,
		breeder.CooldownHours,
		breeder.Active,
	)
	if err != nil {
		return fmt.Errorf("update breeder: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update breeder rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("breeder not found: %s", breeder.HorseID)
	}
	return nil
}

// DeactivateBreeder marks a breeding stallion as inactive.
func (r *BreedingStallionRepo) DeactivateBreeder(ctx context.Context, horseID string) error {
	query := `UPDATE breeding_stallions SET active = FALSE WHERE horse_id = $1`
	result, err := r.db.db.ExecContext(ctx, query, horseID)
	if err != nil {
		return fmt.Errorf("deactivate breeder: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deactivate breeder rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("breeder not found: %s", horseID)
	}
	return nil
}
