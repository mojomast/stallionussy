package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.MarketRepository = (*MarketRepo)(nil)

// MarketRepo implements repository.MarketRepository backed by PostgreSQL.
type MarketRepo struct {
	db *DB
}

// NewMarketRepo returns a new MarketRepo.
func NewMarketRepo(db *DB) *MarketRepo {
	return &MarketRepo{db: db}
}

// CreateListing persists a new stud market listing.
func (r *MarketRepo) CreateListing(ctx context.Context, listing *models.StudListing) error {
	query := `
		INSERT INTO stud_listings (
			id, horse_id, horse_name, owner_id, price,
			pedigree, sappho_score, active, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.db.ExecContext(ctx, query,
		listing.ID,
		listing.HorseID,
		listing.HorseName,
		listing.OwnerID,
		listing.Price,
		listing.Pedigree,
		listing.SapphoScore,
		listing.Active,
		listing.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create listing: %w", err)
	}
	return nil
}

// GetListing retrieves a listing by ID.
func (r *MarketRepo) GetListing(ctx context.Context, id string) (*models.StudListing, error) {
	query := `
		SELECT id, horse_id, horse_name, owner_id, price,
			pedigree, sappho_score, active, created_at
		FROM stud_listings WHERE id = $1`
	l := &models.StudListing{}
	var pedigree sql.NullString
	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&l.ID,
		&l.HorseID,
		&l.HorseName,
		&l.OwnerID,
		&l.Price,
		&pedigree,
		&l.SapphoScore,
		&l.Active,
		&l.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("listing not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get listing: %w", err)
	}
	l.Pedigree = pedigree.String
	return l, nil
}

// ListActiveListings returns all currently active listings.
func (r *MarketRepo) ListActiveListings(ctx context.Context) ([]*models.StudListing, error) {
	query := `
		SELECT id, horse_id, horse_name, owner_id, price,
			pedigree, sappho_score, active, created_at
		FROM stud_listings WHERE active = true ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active listings: %w", err)
	}
	defer rows.Close()

	var listings []*models.StudListing
	for rows.Next() {
		l := &models.StudListing{}
		var pedigree sql.NullString
		if err := rows.Scan(
			&l.ID,
			&l.HorseID,
			&l.HorseName,
			&l.OwnerID,
			&l.Price,
			&pedigree,
			&l.SapphoScore,
			&l.Active,
			&l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list active listings scan: %w", err)
		}
		l.Pedigree = pedigree.String
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active listings rows: %w", err)
	}
	return listings, nil
}

// UpdateListing saves changes to an existing listing.
func (r *MarketRepo) UpdateListing(ctx context.Context, listing *models.StudListing) error {
	query := `
		UPDATE stud_listings SET
			horse_id = $2, horse_name = $3, owner_id = $4, price = $5,
			pedigree = $6, sappho_score = $7, active = $8
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		listing.ID,
		listing.HorseID,
		listing.HorseName,
		listing.OwnerID,
		listing.Price,
		listing.Pedigree,
		listing.SapphoScore,
		listing.Active,
	)
	if err != nil {
		return fmt.Errorf("update listing: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update listing rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("listing not found: %s", listing.ID)
	}
	return nil
}

// DeleteListing removes a listing by ID.
func (r *MarketRepo) DeleteListing(ctx context.Context, id string) error {
	query := `DELETE FROM stud_listings WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete listing: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete listing rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("listing not found: %s", id)
	}
	return nil
}
