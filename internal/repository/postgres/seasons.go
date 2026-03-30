package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.SeasonRepository = (*SeasonRepo)(nil)

// SeasonRepo implements repository.SeasonRepository backed by PostgreSQL.
type SeasonRepo struct {
	db *DB
}

// NewSeasonRepo returns a new SeasonRepo.
func NewSeasonRepo(db *DB) *SeasonRepo {
	return &SeasonRepo{db: db}
}

const seasonCols = `id, name, started_at, ended_at, active, champions`

func scanSeason(sc interface{ Scan(dest ...any) error }) (*models.Season, error) {
	season := &models.Season{}
	var (
		endedAt       sql.NullTime
		championsJSON []byte
	)
	err := sc.Scan(
		&season.ID,
		&season.Name,
		&season.StartedAt,
		&endedAt,
		&season.Active,
		&championsJSON,
	)
	if err != nil {
		return nil, err
	}
	if endedAt.Valid {
		season.EndedAt = endedAt.Time
	}
	if len(championsJSON) > 0 {
		if err := json.Unmarshal(championsJSON, &season.Champions); err != nil {
			return nil, fmt.Errorf("unmarshal champions: %w", err)
		}
	}
	if season.Champions == nil {
		season.Champions = []models.SeasonChampion{}
	}
	return season, nil
}

// CreateSeason persists a new season.
func (r *SeasonRepo) CreateSeason(ctx context.Context, season *models.Season) error {
	championsJSON, err := json.Marshal(season.Champions)
	if err != nil {
		return fmt.Errorf("marshal champions: %w", err)
	}
	var endedAt sql.NullTime
	if !season.EndedAt.IsZero() {
		endedAt = sql.NullTime{Time: season.EndedAt, Valid: true}
	}
	query := `
		INSERT INTO seasons (id, name, started_at, ended_at, active, champions)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err = r.db.db.ExecContext(ctx, query,
		season.ID,
		season.Name,
		season.StartedAt,
		endedAt,
		season.Active,
		championsJSON,
	)
	if err != nil {
		return fmt.Errorf("create season: %w", err)
	}
	return nil
}

// GetCurrentSeason retrieves the active season.
func (r *SeasonRepo) GetCurrentSeason(ctx context.Context) (*models.Season, error) {
	query := `SELECT ` + seasonCols + ` FROM seasons WHERE active = TRUE ORDER BY id DESC LIMIT 1`
	season, err := scanSeason(r.db.db.QueryRowContext(ctx, query))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("active season not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get current season: %w", err)
	}
	return season, nil
}

// ListSeasons returns all seasons, oldest first.
func (r *SeasonRepo) ListSeasons(ctx context.Context) ([]*models.Season, error) {
	query := `SELECT ` + seasonCols + ` FROM seasons ORDER BY id ASC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list seasons: %w", err)
	}
	defer rows.Close()

	var seasons []*models.Season
	for rows.Next() {
		season, err := scanSeason(rows)
		if err != nil {
			return nil, fmt.Errorf("list seasons scan: %w", err)
		}
		seasons = append(seasons, season)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list seasons rows: %w", err)
	}
	return seasons, nil
}

// UpdateSeason saves changes to an existing season.
func (r *SeasonRepo) UpdateSeason(ctx context.Context, season *models.Season) error {
	championsJSON, err := json.Marshal(season.Champions)
	if err != nil {
		return fmt.Errorf("marshal champions: %w", err)
	}
	var endedAt sql.NullTime
	if !season.EndedAt.IsZero() {
		endedAt = sql.NullTime{Time: season.EndedAt, Valid: true}
	}
	query := `
		UPDATE seasons SET
			name = $2,
			started_at = $3,
			ended_at = $4,
			active = $5,
			champions = $6
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		season.ID,
		season.Name,
		season.StartedAt,
		endedAt,
		season.Active,
		championsJSON,
	)
	if err != nil {
		return fmt.Errorf("update season: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update season rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("season not found: %d", season.ID)
	}
	return nil
}
