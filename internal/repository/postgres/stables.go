package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.StableRepository = (*StableRepo)(nil)

// StableRepo implements repository.StableRepository backed by PostgreSQL.
type StableRepo struct {
	db *DB
}

// NewStableRepo returns a new StableRepo.
func NewStableRepo(db *DB) *StableRepo {
	return &StableRepo{db: db}
}

// CreateStable persists a new stable record.
func (r *StableRepo) CreateStable(ctx context.Context, stable *models.Stable) error {
	query := `
		INSERT INTO stables (id, name, owner_id, cummies, created_at, total_earnings, total_races)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.db.ExecContext(ctx, query,
		stable.ID,
		stable.Name,
		stable.OwnerID,
		stable.Cummies,
		stable.CreatedAt,
		stable.TotalEarnings,
		stable.TotalRaces,
	)
	if err != nil {
		return fmt.Errorf("create stable: %w", err)
	}
	return nil
}

// GetStable retrieves a stable by ID (without populating the Horses slice).
func (r *StableRepo) GetStable(ctx context.Context, id string) (*models.Stable, error) {
	query := `
		SELECT id, name, owner_id, cummies, created_at, total_earnings, total_races
		FROM stables WHERE id = $1`
	s := &models.Stable{}
	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID,
		&s.Name,
		&s.OwnerID,
		&s.Cummies,
		&s.CreatedAt,
		&s.TotalEarnings,
		&s.TotalRaces,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stable not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get stable: %w", err)
	}
	return s, nil
}

// GetStableByOwner retrieves the stable belonging to a given owner.
func (r *StableRepo) GetStableByOwner(ctx context.Context, ownerID string) (*models.Stable, error) {
	query := `
		SELECT id, name, owner_id, cummies, created_at, total_earnings, total_races
		FROM stables WHERE owner_id = $1`
	s := &models.Stable{}
	err := r.db.db.QueryRowContext(ctx, query, ownerID).Scan(
		&s.ID,
		&s.Name,
		&s.OwnerID,
		&s.Cummies,
		&s.CreatedAt,
		&s.TotalEarnings,
		&s.TotalRaces,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("stable not found for owner: %s", ownerID)
	}
	if err != nil {
		return nil, fmt.Errorf("get stable by owner: %w", err)
	}
	return s, nil
}

// ListStables returns all stables.
func (r *StableRepo) ListStables(ctx context.Context) ([]*models.Stable, error) {
	query := `
		SELECT id, name, owner_id, cummies, created_at, total_earnings, total_races
		FROM stables ORDER BY created_at`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list stables: %w", err)
	}
	defer rows.Close()

	var stables []*models.Stable
	for rows.Next() {
		s := &models.Stable{}
		if err := rows.Scan(
			&s.ID,
			&s.Name,
			&s.OwnerID,
			&s.Cummies,
			&s.CreatedAt,
			&s.TotalEarnings,
			&s.TotalRaces,
		); err != nil {
			return nil, fmt.Errorf("list stables scan: %w", err)
		}
		stables = append(stables, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stables rows: %w", err)
	}
	return stables, nil
}

// UpdateStable saves changes to an existing stable record.
func (r *StableRepo) UpdateStable(ctx context.Context, stable *models.Stable) error {
	query := `
		UPDATE stables
		SET name = $2, owner_id = $3, cummies = $4, total_earnings = $5, total_races = $6
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		stable.ID,
		stable.Name,
		stable.OwnerID,
		stable.Cummies,
		stable.TotalEarnings,
		stable.TotalRaces,
	)
	if err != nil {
		return fmt.Errorf("update stable: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update stable rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("stable not found: %s", stable.ID)
	}
	return nil
}

// DeleteStable removes a stable by ID.
func (r *StableRepo) DeleteStable(ctx context.Context, id string) error {
	query := `DELETE FROM stables WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete stable: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete stable rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("stable not found: %s", id)
	}
	return nil
}
