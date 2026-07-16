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

// scanPokerTable scans a full poker table row, including the Hold'em fields
// (H-10): game_type, community cards, blinds, seat pointers, side pots, hand
// round and action deadline. These used to be dropped on every DB round-trip,
// so a restart corrupted any in-progress Hold'em hand into a legacy draw
// table with the players' chips already deducted.
func scanPokerTable(sc interface{ Scan(dest ...any) error }) (*models.PokerTable, error) {
	t := &models.PokerTable{}
	var seatsJSON, logJSON, communityJSON, sidePotsJSON []byte
	var deckSeed int64
	var startedAt, actionDeadline sql.NullTime
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
		&t.GameType,
		&communityJSON,
		&t.SmallBlind,
		&t.BigBlind,
		&t.CurrentBet,
		&t.DealerSeat,
		&t.ActionSeat,
		&t.MinRaise,
		&sidePotsJSON,
		&t.Round,
		&actionDeadline,
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
	if len(communityJSON) > 0 {
		if err := json.Unmarshal(communityJSON, &t.CommunityCards); err != nil {
			return nil, fmt.Errorf("unmarshal community cards: %w", err)
		}
	}
	if len(sidePotsJSON) > 0 {
		if err := json.Unmarshal(sidePotsJSON, &t.SidePots); err != nil {
			return nil, fmt.Errorf("unmarshal side pots: %w", err)
		}
	}
	if startedAt.Valid {
		t.StartedAt = startedAt.Time
	}
	if actionDeadline.Valid {
		t.ActionDeadline = actionDeadline.Time
	}
	if t.Seats == nil {
		t.Seats = []models.PokerSeat{}
	}
	if t.Log == nil {
		t.Log = []string{}
	}
	return t, nil
}

// marshalPokerTableJSON marshals the JSONB payloads shared by the create and
// update statements.
func marshalPokerTableJSON(table *models.PokerTable) (seats, log, community, sidePots []byte, err error) {
	if seats, err = json.Marshal(table.Seats); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal poker seats: %w", err)
	}
	if log, err = json.Marshal(table.Log); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal poker log: %w", err)
	}
	if community, err = json.Marshal(table.CommunityCards); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal community cards: %w", err)
	}
	if sidePots, err = json.Marshal(table.SidePots); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal side pots: %w", err)
	}
	return seats, log, community, sidePots, nil
}

func (r *CasinoRepo) CreatePokerTable(ctx context.Context, table *models.PokerTable) error {
	seatsJSON, logJSON, communityJSON, sidePotsJSON, err := marshalPokerTableJSON(table)
	if err != nil {
		return err
	}
	_, err = r.db.db.ExecContext(ctx, `
		INSERT INTO poker_tables (
			id, name, created_by, stake_currency, buy_in, max_players, status,
			pot, deck_seed, seats, log, started_at, created_at, updated_at,
			game_type, community_cards, small_blind, big_blind, current_bet,
			dealer_seat, action_seat, min_raise, side_pots, hand_round, action_deadline
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,
			$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
	`, table.ID, table.Name, table.CreatedBy, table.StakeCurrency, table.BuyIn,
		table.MaxPlayers, table.Status, table.Pot, int64(table.DeckSeed), seatsJSON,
		logJSON, nullableTime(table.StartedAt), table.CreatedAt, table.UpdatedAt,
		table.GameType, communityJSON, table.SmallBlind, table.BigBlind, table.CurrentBet,
		table.DealerSeat, table.ActionSeat, table.MinRaise, sidePotsJSON, table.Round,
		nullableTime(table.ActionDeadline))
	if err != nil {
		return fmt.Errorf("create poker table: %w", err)
	}
	return nil
}

func (r *CasinoRepo) GetPokerTable(ctx context.Context, id string) (*models.PokerTable, error) {
	query := `SELECT id, name, created_by, stake_currency, buy_in, max_players, status, pot, deck_seed, seats, log, started_at, created_at, updated_at, game_type, community_cards, small_blind, big_blind, current_bet, dealer_seat, action_seat, min_raise, side_pots, hand_round, action_deadline FROM poker_tables WHERE id = $1`
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
	rows, err := r.db.db.QueryContext(ctx, `SELECT id, name, created_by, stake_currency, buy_in, max_players, status, pot, deck_seed, seats, log, started_at, created_at, updated_at, game_type, community_cards, small_blind, big_blind, current_bet, dealer_seat, action_seat, min_raise, side_pots, hand_round, action_deadline FROM poker_tables ORDER BY updated_at DESC LIMIT $1`, limit)
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
	seatsJSON, logJSON, communityJSON, sidePotsJSON, err := marshalPokerTableJSON(table)
	if err != nil {
		return err
	}
	result, err := r.db.db.ExecContext(ctx, `
		UPDATE poker_tables
		SET name = $2, created_by = $3, stake_currency = $4, buy_in = $5,
			max_players = $6, status = $7, pot = $8, deck_seed = $9,
			seats = $10, log = $11, started_at = $12, updated_at = $13,
			game_type = $14, community_cards = $15, small_blind = $16,
			big_blind = $17, current_bet = $18, dealer_seat = $19,
			action_seat = $20, min_raise = $21, side_pots = $22,
			hand_round = $23, action_deadline = $24
		WHERE id = $1
	`, table.ID, table.Name, table.CreatedBy, table.StakeCurrency, table.BuyIn,
		table.MaxPlayers, table.Status, table.Pot, int64(table.DeckSeed), seatsJSON,
		logJSON, nullableTime(table.StartedAt), table.UpdatedAt,
		table.GameType, communityJSON, table.SmallBlind, table.BigBlind,
		table.CurrentBet, table.DealerSeat, table.ActionSeat, table.MinRaise,
		sidePotsJSON, table.Round, nullableTime(table.ActionDeadline))
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

// GetJackpotState loads the persisted progressive slot jackpot (M-2).
// Returns sql.ErrNoRows via a zero-value result if no row exists yet.
func (r *CasinoRepo) GetJackpotState(ctx context.Context) (int64, string, int64, error) {
	var pool, lastAmount int64
	var lastWinner string
	err := r.db.db.QueryRowContext(ctx,
		`SELECT pool, last_winner, last_amount FROM casino_jackpot WHERE id = 1`,
	).Scan(&pool, &lastWinner, &lastAmount)
	if err == sql.ErrNoRows {
		return 0, "", 0, nil
	}
	if err != nil {
		return 0, "", 0, fmt.Errorf("get jackpot state: %w", err)
	}
	return pool, lastWinner, lastAmount, nil
}

// SaveJackpotState upserts the progressive slot jackpot state (M-2).
func (r *CasinoRepo) SaveJackpotState(ctx context.Context, pool int64, lastWinner string, lastAmount int64) error {
	_, err := r.db.db.ExecContext(ctx, `
		INSERT INTO casino_jackpot (id, pool, last_winner, last_amount, updated_at)
		VALUES (1, $1, $2, $3, NOW())
		ON CONFLICT (id) DO UPDATE SET
			pool = EXCLUDED.pool,
			last_winner = EXCLUDED.last_winner,
			last_amount = EXCLUDED.last_amount,
			updated_at = NOW()`,
		pool, lastWinner, lastAmount)
	if err != nil {
		return fmt.Errorf("save jackpot state: %w", err)
	}
	return nil
}
