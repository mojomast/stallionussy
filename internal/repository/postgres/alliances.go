package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.AllianceRepository = (*AllianceRepo)(nil)

// AllianceRepo implements repository.AllianceRepository backed by PostgreSQL.
type AllianceRepo struct {
	db *DB
}

// NewAllianceRepo returns a new AllianceRepo.
func NewAllianceRepo(db *DB) *AllianceRepo {
	return &AllianceRepo{db: db}
}

// allianceCols is the canonical column list for alliance queries.
const allianceCols = `id, name, tag, leader_id, motto, treasury, created_at`

// scanAlliance scans a single alliances row.
func scanAlliance(sc interface{ Scan(dest ...any) error }) (*models.Alliance, error) {
	a := &models.Alliance{}
	err := sc.Scan(
		&a.ID,
		&a.Name,
		&a.Tag,
		&a.LeaderID,
		&a.Motto,
		&a.Treasury,
		&a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// memberCols is the canonical column list for alliance_members queries.
const memberCols = `alliance_id, user_id, username, stable_id, role, joined_at`

// scanMember scans a single alliance_members row.
func scanMember(sc interface{ Scan(dest ...any) error }) (*models.AllianceMember, error) {
	m := &models.AllianceMember{}
	var role string
	err := sc.Scan(
		&m.AllianceID,
		&m.UserID,
		&m.Username,
		&m.StableID,
		&role,
		&m.JoinedAt,
	)
	if err != nil {
		return nil, err
	}
	m.Role = models.AllianceRole(role)
	return m, nil
}

// CreateAlliance persists a new alliance.
func (r *AllianceRepo) CreateAlliance(ctx context.Context, alliance *models.Alliance) error {
	query := `
		INSERT INTO alliances (id, name, tag, leader_id, motto, treasury, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.db.ExecContext(ctx, query,
		alliance.ID,
		alliance.Name,
		alliance.Tag,
		alliance.LeaderID,
		alliance.Motto,
		alliance.Treasury,
		alliance.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create alliance: %w", err)
	}
	return nil
}

// GetAlliance retrieves an alliance by ID, including its members.
func (r *AllianceRepo) GetAlliance(ctx context.Context, id string) (*models.Alliance, error) {
	query := `SELECT ` + allianceCols + ` FROM alliances WHERE id = $1`
	a, err := scanAlliance(r.db.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("alliance not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get alliance: %w", err)
	}

	// Load members.
	members, err := r.ListMembers(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get alliance members: %w", err)
	}
	a.Members = make([]models.AllianceMember, len(members))
	for i, m := range members {
		a.Members[i] = *m
	}

	return a, nil
}

// ListAlliances returns all alliances (without members populated — use GetAlliance for full detail).
func (r *AllianceRepo) ListAlliances(ctx context.Context) ([]*models.Alliance, error) {
	query := `SELECT ` + allianceCols + ` FROM alliances ORDER BY created_at DESC`
	rows, err := r.db.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list alliances: %w", err)
	}
	defer rows.Close()

	var alliances []*models.Alliance
	for rows.Next() {
		a, err := scanAlliance(rows)
		if err != nil {
			return nil, fmt.Errorf("list alliances scan: %w", err)
		}
		alliances = append(alliances, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alliances rows: %w", err)
	}
	return alliances, nil
}

// UpdateAlliance saves changes to an existing alliance record.
func (r *AllianceRepo) UpdateAlliance(ctx context.Context, alliance *models.Alliance) error {
	query := `
		UPDATE alliances SET
			name = $2, tag = $3, leader_id = $4, motto = $5, treasury = $6
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		alliance.ID,
		alliance.Name,
		alliance.Tag,
		alliance.LeaderID,
		alliance.Motto,
		alliance.Treasury,
	)
	if err != nil {
		return fmt.Errorf("update alliance: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update alliance rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("alliance not found: %s", alliance.ID)
	}
	return nil
}

// DeleteAlliance removes an alliance. Members are cascade-deleted by the FK.
func (r *AllianceRepo) DeleteAlliance(ctx context.Context, id string) error {
	query := `DELETE FROM alliances WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete alliance: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete alliance rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("alliance not found: %s", id)
	}
	return nil
}

// AddMember adds a member to an alliance.
func (r *AllianceRepo) AddMember(ctx context.Context, member *models.AllianceMember) error {
	query := `
		INSERT INTO alliance_members (alliance_id, user_id, username, stable_id, role, joined_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.db.db.ExecContext(ctx, query,
		member.AllianceID,
		member.UserID,
		member.Username,
		member.StableID,
		string(member.Role),
		member.JoinedAt,
	)
	if err != nil {
		return fmt.Errorf("add alliance member: %w", err)
	}
	return nil
}

// RemoveMember removes a member from an alliance.
func (r *AllianceRepo) RemoveMember(ctx context.Context, allianceID, userID string) error {
	query := `DELETE FROM alliance_members WHERE alliance_id = $1 AND user_id = $2`
	result, err := r.db.db.ExecContext(ctx, query, allianceID, userID)
	if err != nil {
		return fmt.Errorf("remove alliance member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove alliance member rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("member %s not found in alliance %s", userID, allianceID)
	}
	return nil
}

// GetMember retrieves a specific member record within an alliance.
func (r *AllianceRepo) GetMember(ctx context.Context, allianceID, userID string) (*models.AllianceMember, error) {
	query := `SELECT ` + memberCols + ` FROM alliance_members WHERE alliance_id = $1 AND user_id = $2`
	m, err := scanMember(r.db.db.QueryRowContext(ctx, query, allianceID, userID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("member not found: %s in alliance %s", userID, allianceID)
	}
	if err != nil {
		return nil, fmt.Errorf("get alliance member: %w", err)
	}
	return m, nil
}

// GetMemberByUser finds which alliance a user belongs to (if any).
// Returns nil, nil if the user is not in any alliance.
func (r *AllianceRepo) GetMemberByUser(ctx context.Context, userID string) (*models.AllianceMember, error) {
	query := `SELECT ` + memberCols + ` FROM alliance_members WHERE user_id = $1 LIMIT 1`
	m, err := scanMember(r.db.db.QueryRowContext(ctx, query, userID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get member by user: %w", err)
	}
	return m, nil
}

// ListMembers returns all members of a given alliance.
func (r *AllianceRepo) ListMembers(ctx context.Context, allianceID string) ([]*models.AllianceMember, error) {
	query := `SELECT ` + memberCols + ` FROM alliance_members WHERE alliance_id = $1 ORDER BY joined_at`
	rows, err := r.db.db.QueryContext(ctx, query, allianceID)
	if err != nil {
		return nil, fmt.Errorf("list alliance members: %w", err)
	}
	defer rows.Close()

	var members []*models.AllianceMember
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, fmt.Errorf("list alliance members scan: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alliance members rows: %w", err)
	}
	return members, nil
}

// UpdateMember saves changes to a member record (e.g. role promotion).
func (r *AllianceRepo) UpdateMember(ctx context.Context, member *models.AllianceMember) error {
	query := `
		UPDATE alliance_members SET
			username = $3, stable_id = $4, role = $5
		WHERE alliance_id = $1 AND user_id = $2`
	result, err := r.db.db.ExecContext(ctx, query,
		member.AllianceID,
		member.UserID,
		member.Username,
		member.StableID,
		string(member.Role),
	)
	if err != nil {
		return fmt.Errorf("update alliance member: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update alliance member rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("member %s not found in alliance %s", member.UserID, member.AllianceID)
	}
	return nil
}
