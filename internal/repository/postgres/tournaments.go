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
var _ repository.TournamentRepository = (*TournamentRepo)(nil)

// TournamentRepo implements repository.TournamentRepository backed by PostgreSQL.
type TournamentRepo struct {
	db *DB
}

// NewTournamentRepo returns a new TournamentRepo.
func NewTournamentRepo(db *DB) *TournamentRepo {
	return &TournamentRepo{db: db}
}

// scanTournament scans a single tournament row. Standings and Races are stored
// as JSONB and unmarshalled here.
func scanTournament(sc interface{ Scan(dest ...any) error }) (*models.Tournament, error) {
	t := &models.Tournament{}
	var (
		standingsJSON []byte
		racesJSON     []byte
	)

	err := sc.Scan(
		&t.ID,
		&t.Name,
		&t.TrackType,
		&t.Rounds,
		&t.CurrentRound,
		&t.EntryFee,
		&t.PrizePool,
		&standingsJSON,
		&racesJSON,
		&t.Status,
		&t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSONB standings.
	if len(standingsJSON) > 0 {
		if err := json.Unmarshal(standingsJSON, &t.Standings); err != nil {
			return nil, fmt.Errorf("unmarshal standings: %w", err)
		}
	}
	if t.Standings == nil {
		t.Standings = []models.TournamentEntry{}
	}

	// Unmarshal JSONB races.
	if len(racesJSON) > 0 {
		if err := json.Unmarshal(racesJSON, &t.Races); err != nil {
			return nil, fmt.Errorf("unmarshal races: %w", err)
		}
	}
	if t.Races == nil {
		t.Races = []string{}
	}

	return t, nil
}

// CreateTournament persists a new tournament. Standings and Races are marshalled
// to JSONB.
func (r *TournamentRepo) CreateTournament(ctx context.Context, tournament *models.Tournament) error {
	standingsJSON, err := json.Marshal(tournament.Standings)
	if err != nil {
		return fmt.Errorf("marshal standings: %w", err)
	}
	racesJSON, err := json.Marshal(tournament.Races)
	if err != nil {
		return fmt.Errorf("marshal races: %w", err)
	}

	query := `
		INSERT INTO tournaments (
			id, name, track_type, rounds, current_round,
			entry_fee, prize_pool, standings, races, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err = r.db.db.ExecContext(ctx, query,
		tournament.ID,
		tournament.Name,
		tournament.TrackType,
		tournament.Rounds,
		tournament.CurrentRound,
		tournament.EntryFee,
		tournament.PrizePool,
		standingsJSON,
		racesJSON,
		tournament.Status,
		tournament.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create tournament: %w", err)
	}
	return nil
}

// GetTournament retrieves a tournament by ID.
func (r *TournamentRepo) GetTournament(ctx context.Context, id string) (*models.Tournament, error) {
	query := `
		SELECT id, name, track_type, rounds, current_round,
			entry_fee, prize_pool, standings, races, status, created_at
		FROM tournaments WHERE id = $1`
	t, err := scanTournament(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tournament not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get tournament: %w", err)
	}
	return t, nil
}

// ListTournaments returns all tournaments.
func (r *TournamentRepo) ListTournaments(ctx context.Context) ([]*models.Tournament, error) {
	query := `
		SELECT id, name, track_type, rounds, current_round,
			entry_fee, prize_pool, standings, races, status, created_at
		FROM tournaments ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tournaments: %w", err)
	}
	defer rows.Close()

	var tournaments []*models.Tournament
	for rows.Next() {
		t, err := scanTournament(rows)
		if err != nil {
			return nil, fmt.Errorf("list tournaments scan: %w", err)
		}
		tournaments = append(tournaments, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tournaments rows: %w", err)
	}
	return tournaments, nil
}

// UpdateTournament saves changes to an existing tournament.
func (r *TournamentRepo) UpdateTournament(ctx context.Context, tournament *models.Tournament) error {
	standingsJSON, err := json.Marshal(tournament.Standings)
	if err != nil {
		return fmt.Errorf("marshal standings: %w", err)
	}
	racesJSON, err := json.Marshal(tournament.Races)
	if err != nil {
		return fmt.Errorf("marshal races: %w", err)
	}

	query := `
		UPDATE tournaments SET
			name = $2, track_type = $3, rounds = $4, current_round = $5,
			entry_fee = $6, prize_pool = $7, standings = $8, races = $9,
			status = $10
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		tournament.ID,
		tournament.Name,
		tournament.TrackType,
		tournament.Rounds,
		tournament.CurrentRound,
		tournament.EntryFee,
		tournament.PrizePool,
		standingsJSON,
		racesJSON,
		tournament.Status,
	)
	if err != nil {
		return fmt.Errorf("update tournament: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update tournament rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tournament not found: %s", tournament.ID)
	}
	return nil
}
