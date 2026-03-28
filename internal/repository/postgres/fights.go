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
var _ repository.HorseFightRepository = (*FightRepo)(nil)

// FightRepo implements repository.HorseFightRepository backed by PostgreSQL.
type FightRepo struct {
	db *DB
}

// NewFightRepo returns a new FightRepo.
func NewFightRepo(db *DB) *FightRepo {
	return &FightRepo{db: db}
}

// fightCols is the canonical column list for horse_fights queries.
const fightCols = `
	id, arena_type, horse1_id, horse1_name, horse1_owner_id,
	horse2_id, horse2_name, horse2_owner_id,
	winner_id, winner_name, loser_id, loser_name,
	is_fatality, is_to_death, purse, entry_fee,
	status, ko_round, total_rounds,
	fight_log, narrative, created_at`

// scanFight scans a single horse_fights row. fight_log and narrative are
// stored as JSONB and unmarshalled here.
func scanFight(sc interface{ Scan(dest ...any) error }) (*models.HorseFight, error) {
	f := &models.HorseFight{}
	var (
		fightLogJSON  []byte
		narrativeJSON []byte
	)

	err := sc.Scan(
		&f.ID,
		&f.ArenaType,
		&f.Horse1ID,
		&f.Horse1Name,
		&f.Horse1OwnerID,
		&f.Horse2ID,
		&f.Horse2Name,
		&f.Horse2OwnerID,
		&f.WinnerID,
		&f.WinnerName,
		&f.LoserID,
		&f.LoserName,
		&f.IsFatality,
		&f.IsToDeath,
		&f.Purse,
		&f.EntryFee,
		&f.Status,
		&f.KORound,
		&f.TotalRounds,
		&fightLogJSON,
		&narrativeJSON,
		&f.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// fight_log is opaque JSONB — store as raw bytes.
	f.FightLog = fightLogJSON

	// Unmarshal narrative array.
	if len(narrativeJSON) > 0 {
		if err := json.Unmarshal(narrativeJSON, &f.Narrative); err != nil {
			return nil, fmt.Errorf("unmarshal narrative: %w", err)
		}
	}
	if f.Narrative == nil {
		f.Narrative = []string{}
	}

	return f, nil
}

// CreateFight persists a new horse fight record.
func (r *FightRepo) CreateFight(ctx context.Context, fight *models.HorseFight) error {
	narrativeJSON, err := json.Marshal(fight.Narrative)
	if err != nil {
		return fmt.Errorf("marshal narrative: %w", err)
	}

	// Default fight_log to empty JSON object if nil.
	fightLogJSON := fight.FightLog
	if fightLogJSON == nil {
		fightLogJSON = []byte("{}")
	}

	query := `
		INSERT INTO horse_fights (
			id, arena_type, horse1_id, horse1_name, horse1_owner_id,
			horse2_id, horse2_name, horse2_owner_id,
			winner_id, winner_name, loser_id, loser_name,
			is_fatality, is_to_death, purse, entry_fee,
			status, ko_round, total_rounds,
			fight_log, narrative, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`
	_, err = r.db.db.ExecContext(ctx, query,
		fight.ID,
		fight.ArenaType,
		fight.Horse1ID,
		fight.Horse1Name,
		fight.Horse1OwnerID,
		fight.Horse2ID,
		fight.Horse2Name,
		fight.Horse2OwnerID,
		fight.WinnerID,
		fight.WinnerName,
		fight.LoserID,
		fight.LoserName,
		fight.IsFatality,
		fight.IsToDeath,
		fight.Purse,
		fight.EntryFee,
		fight.Status,
		fight.KORound,
		fight.TotalRounds,
		fightLogJSON,
		narrativeJSON,
		fight.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create fight: %w", err)
	}
	return nil
}

// GetFight retrieves a fight by ID.
func (r *FightRepo) GetFight(ctx context.Context, id string) (*models.HorseFight, error) {
	query := `SELECT ` + fightCols + ` FROM horse_fights WHERE id = $1`
	f, err := scanFight(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("fight not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get fight: %w", err)
	}
	return f, nil
}

// ListRecentFights returns the most recent fights, newest first.
func (r *FightRepo) ListRecentFights(ctx context.Context, limit int) ([]*models.HorseFight, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT ` + fightCols + ` FROM horse_fights ORDER BY created_at DESC LIMIT $1`
	rows, err := r.db.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent fights: %w", err)
	}
	defer rows.Close()

	var fights []*models.HorseFight
	for rows.Next() {
		f, err := scanFight(rows)
		if err != nil {
			return nil, fmt.Errorf("list recent fights scan: %w", err)
		}
		fights = append(fights, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent fights rows: %w", err)
	}
	return fights, nil
}

// ListFightsByHorse returns all fights involving a given horse.
func (r *FightRepo) ListFightsByHorse(ctx context.Context, horseID string) ([]*models.HorseFight, error) {
	query := `SELECT ` + fightCols + ` FROM horse_fights
		WHERE horse1_id = $1 OR horse2_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query, horseID)
	if err != nil {
		return nil, fmt.Errorf("list fights by horse: %w", err)
	}
	defer rows.Close()

	var fights []*models.HorseFight
	for rows.Next() {
		f, err := scanFight(rows)
		if err != nil {
			return nil, fmt.Errorf("list fights by horse scan: %w", err)
		}
		fights = append(fights, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list fights by horse rows: %w", err)
	}
	return fights, nil
}

// UpdateFight saves changes to an existing fight record.
func (r *FightRepo) UpdateFight(ctx context.Context, fight *models.HorseFight) error {
	narrativeJSON, err := json.Marshal(fight.Narrative)
	if err != nil {
		return fmt.Errorf("marshal narrative: %w", err)
	}

	fightLogJSON := fight.FightLog
	if fightLogJSON == nil {
		fightLogJSON = []byte("{}")
	}

	query := `
		UPDATE horse_fights SET
			arena_type = $2, horse1_id = $3, horse1_name = $4, horse1_owner_id = $5,
			horse2_id = $6, horse2_name = $7, horse2_owner_id = $8,
			winner_id = $9, winner_name = $10, loser_id = $11, loser_name = $12,
			is_fatality = $13, is_to_death = $14, purse = $15, entry_fee = $16,
			status = $17, ko_round = $18, total_rounds = $19,
			fight_log = $20, narrative = $21
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		fight.ID,
		fight.ArenaType,
		fight.Horse1ID,
		fight.Horse1Name,
		fight.Horse1OwnerID,
		fight.Horse2ID,
		fight.Horse2Name,
		fight.Horse2OwnerID,
		fight.WinnerID,
		fight.WinnerName,
		fight.LoserID,
		fight.LoserName,
		fight.IsFatality,
		fight.IsToDeath,
		fight.Purse,
		fight.EntryFee,
		fight.Status,
		fight.KORound,
		fight.TotalRounds,
		fightLogJSON,
		narrativeJSON,
	)
	if err != nil {
		return fmt.Errorf("update fight: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update fight rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("fight not found: %s", fight.ID)
	}
	return nil
}
