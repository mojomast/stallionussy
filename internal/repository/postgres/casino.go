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

var _ repository.CasinoRepository = (*CasinoRepo)(nil)

type CasinoRepo struct {
	db *DB
}

func NewCasinoRepo(db *DB) *CasinoRepo {
	return &CasinoRepo{db: db}
}

func scanPokerTable(sc interface{ Scan(dest ...any) error }) (*models.PokerTable, error) {
	t := &models.PokerTable{}
	var seatsJSON, logJSON []byte
	var deckSeed int64
	var startedAt sql.NullTime
	err := sc.Scan(
		&t.ID,
		&t.Name,
		&t.CreatedBy,
		&t.StakeCurrency,
		&t.BuyIn,
		&t.MaxPlayers,
		&t.Status,
		&t.Pot,
		&deckSeed,
		&seatsJSON,
		&logJSON,
		&startedAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.DeckSeed = uint64(deckSeed)
	if len(seatsJSON) > 0 {
		if err := json.Unmarshal(seatsJSON, &t.Seats); err != nil {
			return nil, fmt.Errorf("unmarshal poker seats: %w", err)
		}
	}
	if len(logJSON) > 0 {
		if err := json.Unmarshal(logJSON, &t.Log); err != nil {
			return nil, fmt.Errorf("unmarshal poker log: %w", err)
		}
	}
	if startedAt.Valid {
		t.StartedAt = startedAt.Time
	}
	if t.Seats == nil {
		t.Seats = []models.PokerSeat{}
	}
	if t.Log == nil {
		t.Log = []string{}
	}
	return t, nil
}

func (r *CasinoRepo) CreatePokerTable(ctx context.Context, table *models.PokerTable) error {
	seatsJSON, err := json.Marshal(table.Seats)
	if err != nil {
		return fmt.Errorf("marshal poker seats: %w", err)
	}
	logJSON, err := json.Marshal(table.Log)
	if err != nil {
		return fmt.Errorf("marshal poker log: %w", err)
	}
	_, err = r.db.db.ExecContext(ctx, `
		INSERT INTO poker_tables (
			id, name, created_by, stake_currency, buy_in, max_players, status,
			pot, deck_seed, seats, log, started_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, table.ID, table.Name, table.CreatedBy, table.StakeCurrency, table.BuyIn,
		table.MaxPlayers, table.Status, table.Pot, int64(table.DeckSeed), seatsJSON,
		logJSON, nullableTime(table.StartedAt), table.CreatedAt, table.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create poker table: %w", err)
	}
	return nil
}

func (r *CasinoRepo) GetPokerTable(ctx context.Context, id string) (*models.PokerTable, error) {
	query := `SELECT id, name, created_by, stake_currency, buy_in, max_players, status, pot, deck_seed, seats, log, started_at, created_at, updated_at FROM poker_tables WHERE id = $1`
	t, err := scanPokerTable(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("poker table not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get poker table: %w", err)
	}
	return t, nil
}

func (r *CasinoRepo) ListPokerTables(ctx context.Context, limit int) ([]*models.PokerTable, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.db.QueryContext(ctx, `SELECT id, name, created_by, stake_currency, buy_in, max_players, status, pot, deck_seed, seats, log, started_at, created_at, updated_at FROM poker_tables ORDER BY updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list poker tables: %w", err)
	}
	defer rows.Close()
	var tables []*models.PokerTable
	for rows.Next() {
		t, err := scanPokerTable(rows)
		if err != nil {
			return nil, fmt.Errorf("list poker tables scan: %w", err)
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list poker tables rows: %w", err)
	}
	return tables, nil
}

func (r *CasinoRepo) UpdatePokerTable(ctx context.Context, table *models.PokerTable) error {
	seatsJSON, err := json.Marshal(table.Seats)
	if err != nil {
		return fmt.Errorf("marshal poker seats: %w", err)
	}
	logJSON, err := json.Marshal(table.Log)
	if err != nil {
		return fmt.Errorf("marshal poker log: %w", err)
	}
	result, err := r.db.db.ExecContext(ctx, `
		UPDATE poker_tables
		SET name = $2, created_by = $3, stake_currency = $4, buy_in = $5,
			max_players = $6, status = $7, pot = $8, deck_seed = $9,
			seats = $10, log = $11, started_at = $12, updated_at = $13
		WHERE id = $1
	`, table.ID, table.Name, table.CreatedBy, table.StakeCurrency, table.BuyIn,
		table.MaxPlayers, table.Status, table.Pot, int64(table.DeckSeed), seatsJSON,
		logJSON, nullableTime(table.StartedAt), table.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update poker table: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update poker table rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("poker table not found: %s", table.ID)
	}
	return nil
}

func (r *CasinoRepo) RecordSlotSpin(ctx context.Context, spin *models.SlotSpin) error {
	symbolsJSON, err := json.Marshal(spin.Symbols)
	if err != nil {
		return fmt.Errorf("marshal slot symbols: %w", err)
	}
	_, err = r.db.db.ExecContext(ctx, `
		INSERT INTO slot_spins (id, stable_id, user_id, wager_amount, payout_amount, multiplier, symbols, summary, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, spin.ID, spin.StableID, spin.UserID, spin.WagerAmount, spin.PayoutAmount, spin.Multiplier, symbolsJSON, spin.Summary, spin.CreatedAt)
	if err != nil {
		return fmt.Errorf("record slot spin: %w", err)
	}
	return nil
}

func (r *CasinoRepo) ListSlotSpinsByUser(ctx context.Context, userID string, limit int) ([]*models.SlotSpin, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.db.QueryContext(ctx, `SELECT id, stable_id, user_id, wager_amount, payout_amount, multiplier, symbols, summary, created_at FROM slot_spins WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list slot spins: %w", err)
	}
	defer rows.Close()
	var spins []*models.SlotSpin
	for rows.Next() {
		spin := &models.SlotSpin{}
		var symbolsJSON []byte
		if err := rows.Scan(&spin.ID, &spin.StableID, &spin.UserID, &spin.WagerAmount, &spin.PayoutAmount, &spin.Multiplier, &symbolsJSON, &spin.Summary, &spin.CreatedAt); err != nil {
			return nil, fmt.Errorf("list slot spins scan: %w", err)
		}
		if len(symbolsJSON) > 0 {
			if err := json.Unmarshal(symbolsJSON, &spin.Symbols); err != nil {
				return nil, fmt.Errorf("unmarshal slot symbols: %w", err)
			}
		}
		spins = append(spins, spin)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list slot spins rows: %w", err)
	}
	return spins, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
