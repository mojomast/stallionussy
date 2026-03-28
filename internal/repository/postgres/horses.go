package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.HorseRepository = (*HorseRepo)(nil)

// HorseRepo implements repository.HorseRepository backed by PostgreSQL.
type HorseRepo struct {
	db *DB
}

// NewHorseRepo returns a new HorseRepo.
func NewHorseRepo(db *DB) *HorseRepo {
	return &HorseRepo{db: db}
}

// horseCols is the canonical column list for SELECT queries.
const horseCols = `
	id, name, genome, sire_id, mare_id, generation, age,
	fitness_ceiling, current_fitness, wins, losses, races,
	elo, owner_id, is_legendary, lot_number, created_at,
	lore, traits, fatigue, retired, total_earnings,
	training_xp, peak_elo, injury`

// scanHorse scans a single horse row from the given scanner. Genome and Traits
// are stored as JSONB and unmarshalled here.
func scanHorse(sc interface{ Scan(dest ...any) error }) (*models.Horse, error) {
	h := &models.Horse{}
	var (
		genomeJSON []byte
		traitsJSON []byte
		injuryJSON []byte
		sireID     sql.NullString
		mareID     sql.NullString
		lore       sql.NullString
	)

	err := sc.Scan(
		&h.ID,
		&h.Name,
		&genomeJSON,
		&sireID,
		&mareID,
		&h.Generation,
		&h.Age,
		&h.FitnessCeiling,
		&h.CurrentFitness,
		&h.Wins,
		&h.Losses,
		&h.Races,
		&h.ELO,
		&h.OwnerID,
		&h.IsLegendary,
		&h.LotNumber,
		&h.CreatedAt,
		&lore,
		&traitsJSON,
		&h.Fatigue,
		&h.Retired,
		&h.TotalEarnings,
		&h.TrainingXP,
		&h.PeakELO,
		&injuryJSON,
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSONB genome.
	if len(genomeJSON) > 0 {
		if err := json.Unmarshal(genomeJSON, &h.Genome); err != nil {
			return nil, fmt.Errorf("unmarshal genome: %w", err)
		}
	}

	// Unmarshal JSONB traits.
	if len(traitsJSON) > 0 {
		if err := json.Unmarshal(traitsJSON, &h.Traits); err != nil {
			return nil, fmt.Errorf("unmarshal traits: %w", err)
		}
	}

	// Unmarshal JSONB injury (nullable — nil means healthy).
	if len(injuryJSON) > 0 {
		h.Injury = &models.Injury{}
		if err := json.Unmarshal(injuryJSON, h.Injury); err != nil {
			return nil, fmt.Errorf("unmarshal injury: %w", err)
		}
	}

	// Handle NULLable fields.
	h.SireID = sireID.String
	h.MareID = mareID.String
	h.Lore = lore.String

	// Ensure non-nil defaults.
	if h.Genome == nil {
		h.Genome = make(models.Genome)
	}
	if h.Traits == nil {
		h.Traits = []models.Trait{}
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now()
	}

	return h, nil
}

// toNullString converts an empty string to sql.NullString{Valid: false}.
func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// CreateHorse persists a new horse. Genome, Traits, and Injury are marshalled to JSONB.
func (r *HorseRepo) CreateHorse(ctx context.Context, horse *models.Horse) error {
	genomeJSON, err := json.Marshal(horse.Genome)
	if err != nil {
		return fmt.Errorf("marshal genome: %w", err)
	}
	traitsJSON, err := json.Marshal(horse.Traits)
	if err != nil {
		return fmt.Errorf("marshal traits: %w", err)
	}
	var injuryJSON []byte
	if horse.Injury != nil {
		injuryJSON, err = json.Marshal(horse.Injury)
		if err != nil {
			return fmt.Errorf("marshal injury: %w", err)
		}
	}

	query := `
		INSERT INTO horses (
			id, name, genome, sire_id, mare_id, generation, age,
			fitness_ceiling, current_fitness, wins, losses, races,
			elo, owner_id, is_legendary, lot_number, created_at,
			lore, traits, fatigue, retired, total_earnings,
			training_xp, peak_elo, injury
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22,
			$23, $24, $25
		)`
	_, err = r.db.db.ExecContext(ctx, query,
		horse.ID,
		horse.Name,
		genomeJSON,
		toNullString(horse.SireID),
		toNullString(horse.MareID),
		horse.Generation,
		horse.Age,
		horse.FitnessCeiling,
		horse.CurrentFitness,
		horse.Wins,
		horse.Losses,
		horse.Races,
		horse.ELO,
		horse.OwnerID,
		horse.IsLegendary,
		horse.LotNumber,
		horse.CreatedAt,
		toNullString(horse.Lore),
		traitsJSON,
		horse.Fatigue,
		horse.Retired,
		horse.TotalEarnings,
		horse.TrainingXP,
		horse.PeakELO,
		injuryJSON,
	)
	if err != nil {
		return fmt.Errorf("create horse: %w", err)
	}
	return nil
}

// GetHorse retrieves a horse by ID.
func (r *HorseRepo) GetHorse(ctx context.Context, id string) (*models.Horse, error) {
	query := `SELECT ` + horseCols + ` FROM horses WHERE id = $1`
	h, err := scanHorse(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("horse not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get horse: %w", err)
	}
	return h, nil
}

// ListHorsesByStable returns all horses belonging to a given stable (owner_id).
func (r *HorseRepo) ListHorsesByStable(ctx context.Context, stableID string) ([]*models.Horse, error) {
	query := `SELECT ` + horseCols + ` FROM horses WHERE owner_id = $1 ORDER BY created_at`
	rows, err := r.db.db.QueryContext(ctx, query, stableID)
	if err != nil {
		return nil, fmt.Errorf("list horses by stable: %w", err)
	}
	defer rows.Close()

	var horses []*models.Horse
	for rows.Next() {
		h, err := scanHorse(rows)
		if err != nil {
			return nil, fmt.Errorf("list horses by stable scan: %w", err)
		}
		horses = append(horses, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list horses by stable rows: %w", err)
	}
	return horses, nil
}

// ListAllHorses returns every horse in the database.
func (r *HorseRepo) ListAllHorses(ctx context.Context) ([]*models.Horse, error) {
	query := `SELECT ` + horseCols + ` FROM horses ORDER BY created_at`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all horses: %w", err)
	}
	defer rows.Close()

	var horses []*models.Horse
	for rows.Next() {
		h, err := scanHorse(rows)
		if err != nil {
			return nil, fmt.Errorf("list all horses scan: %w", err)
		}
		horses = append(horses, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all horses rows: %w", err)
	}
	return horses, nil
}

// UpdateHorse saves changes to an existing horse record.
func (r *HorseRepo) UpdateHorse(ctx context.Context, horse *models.Horse) error {
	genomeJSON, err := json.Marshal(horse.Genome)
	if err != nil {
		return fmt.Errorf("marshal genome: %w", err)
	}
	traitsJSON, err := json.Marshal(horse.Traits)
	if err != nil {
		return fmt.Errorf("marshal traits: %w", err)
	}
	var injuryJSON []byte
	if horse.Injury != nil {
		injuryJSON, err = json.Marshal(horse.Injury)
		if err != nil {
			return fmt.Errorf("marshal injury: %w", err)
		}
	}

	query := `
		UPDATE horses SET
			name = $2, genome = $3, sire_id = $4, mare_id = $5,
			generation = $6, age = $7, fitness_ceiling = $8,
			current_fitness = $9, wins = $10, losses = $11, races = $12,
			elo = $13, owner_id = $14, is_legendary = $15, lot_number = $16,
			lore = $17, traits = $18, fatigue = $19, retired = $20,
			total_earnings = $21, training_xp = $22, peak_elo = $23,
			injury = $24
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		horse.ID,
		horse.Name,
		genomeJSON,
		toNullString(horse.SireID),
		toNullString(horse.MareID),
		horse.Generation,
		horse.Age,
		horse.FitnessCeiling,
		horse.CurrentFitness,
		horse.Wins,
		horse.Losses,
		horse.Races,
		horse.ELO,
		horse.OwnerID,
		horse.IsLegendary,
		horse.LotNumber,
		toNullString(horse.Lore),
		traitsJSON,
		horse.Fatigue,
		horse.Retired,
		horse.TotalEarnings,
		horse.TrainingXP,
		horse.PeakELO,
		injuryJSON,
	)
	if err != nil {
		return fmt.Errorf("update horse: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update horse rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("horse not found: %s", horse.ID)
	}
	return nil
}

// DeleteHorse removes a horse by ID.
func (r *HorseRepo) DeleteHorse(ctx context.Context, id string) error {
	query := `DELETE FROM horses WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete horse: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete horse rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("horse not found: %s", id)
	}
	return nil
}

// MoveHorse atomically transfers a horse from one stable to another.
// It verifies the horse currently belongs to fromStableID before moving.
func (r *HorseRepo) MoveHorse(ctx context.Context, horseID, fromStableID, toStableID string) error {
	query := `UPDATE horses SET owner_id = $1 WHERE id = $2 AND owner_id = $3`
	result, err := r.db.db.ExecContext(ctx, query, toStableID, horseID, fromStableID)
	if err != nil {
		return fmt.Errorf("move horse: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("move horse rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("horse %s not found in stable %s", horseID, fromStableID)
	}
	return nil
}
