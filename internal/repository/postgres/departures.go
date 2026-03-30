package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

var _ repository.DepartureRepository = (*DepartureRepo)(nil)

type DepartureRepo struct {
	db *DB
}

func NewDepartureRepo(db *DB) *DepartureRepo {
	return &DepartureRepo{db: db}
}

func scanDeparture(sc interface{ Scan(dest ...any) error }) (*models.DepartureRecord, error) {
	rec := &models.DepartureRecord{}
	var snapshotJSON []byte
	var omenExpiresAt sql.NullTime
	var returnedAt sql.NullTime
	err := sc.Scan(
		&rec.ID,
		&rec.HorseID,
		&rec.HorseName,
		&rec.OwnerID,
		&rec.StableID,
		&rec.Cause,
		&rec.State,
		&snapshotJSON,
		&rec.OmenText,
		&rec.ReturnSummary,
		&rec.ReturnedHorse,
		&rec.LastRollDate,
		&rec.CreatedAt,
		&omenExpiresAt,
		&returnedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(snapshotJSON) > 0 {
		if err := json.Unmarshal(snapshotJSON, &rec.HorseSnapshot); err != nil {
			return nil, fmt.Errorf("unmarshal departure snapshot: %w", err)
		}
	}
	if omenExpiresAt.Valid {
		rec.OmenExpiresAt = omenExpiresAt.Time
	}
	if returnedAt.Valid {
		rec.ReturnedAt = returnedAt.Time
	}
	return rec, nil
}

func (r *DepartureRepo) CreateDeparture(ctx context.Context, record *models.DepartureRecord) error {
	snapshotJSON, err := json.Marshal(record.HorseSnapshot)
	if err != nil {
		return fmt.Errorf("marshal departure snapshot: %w", err)
	}
	_, err = r.db.db.ExecContext(ctx, `
		INSERT INTO departed_horses (
			id, horse_id, horse_name, owner_id, stable_id, cause, state,
			horse_snapshot, omen_text, return_summary, returned_horse,
			last_roll_date, created_at, omen_expires_at, returned_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`, record.ID, record.HorseID, record.HorseName, record.OwnerID, record.StableID,
		record.Cause, record.State, snapshotJSON, record.OmenText, record.ReturnSummary,
		record.ReturnedHorse, record.LastRollDate, record.CreatedAt, nullableTime(record.OmenExpiresAt), nullableTime(record.ReturnedAt))
	if err != nil {
		return fmt.Errorf("create departure: %w", err)
	}
	return nil
}

func (r *DepartureRepo) GetDeparture(ctx context.Context, id string) (*models.DepartureRecord, error) {
	q := `SELECT id, horse_id, horse_name, owner_id, stable_id, cause, state, horse_snapshot, omen_text, return_summary, returned_horse, last_roll_date, created_at, omen_expires_at, returned_at FROM departed_horses WHERE id = $1`
	rec, err := scanDeparture(r.db.db.QueryRowContext(ctx, q, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("departure not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get departure: %w", err)
	}
	return rec, nil
}

func (r *DepartureRepo) ListDeparturesByOwner(ctx context.Context, ownerID string, limit int) ([]*models.DepartureRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.db.QueryContext(ctx, `SELECT id, horse_id, horse_name, owner_id, stable_id, cause, state, horse_snapshot, omen_text, return_summary, returned_horse, last_roll_date, created_at, omen_expires_at, returned_at FROM departed_horses WHERE owner_id = $1 ORDER BY created_at DESC LIMIT $2`, ownerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list departures: %w", err)
	}
	defer rows.Close()
	var records []*models.DepartureRecord
	for rows.Next() {
		rec, err := scanDeparture(rows)
		if err != nil {
			return nil, fmt.Errorf("list departures scan: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list departures rows: %w", err)
	}
	return records, nil
}

func (r *DepartureRepo) UpdateDeparture(ctx context.Context, record *models.DepartureRecord) error {
	snapshotJSON, err := json.Marshal(record.HorseSnapshot)
	if err != nil {
		return fmt.Errorf("marshal departure snapshot: %w", err)
	}
	_, err = r.db.db.ExecContext(ctx, `
		UPDATE departed_horses
		SET horse_id = $2, horse_name = $3, owner_id = $4, stable_id = $5,
			cause = $6, state = $7, horse_snapshot = $8, omen_text = $9,
			return_summary = $10, returned_horse = $11, last_roll_date = $12,
			omen_expires_at = $13, returned_at = $14
		WHERE id = $1
	`, record.ID, record.HorseID, record.HorseName, record.OwnerID, record.StableID,
		record.Cause, record.State, snapshotJSON, record.OmenText, record.ReturnSummary,
		record.ReturnedHorse, record.LastRollDate, nullableTime(record.OmenExpiresAt), nullableTime(record.ReturnedAt))
	if err != nil {
		return fmt.Errorf("update departure: %w", err)
	}
	return nil
}
