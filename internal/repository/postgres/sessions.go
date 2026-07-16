package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.SessionRepository = (*SessionRepo)(nil)

// SessionRepo implements repository.SessionRepository on PostgreSQL/SQLite.
type SessionRepo struct {
	db *DB
}

// NewSessionRepo returns a new SessionRepo.
func NewSessionRepo(db *DB) *SessionRepo {
	return &SessionRepo{db: db}
}

// CreateSession inserts a new session row. Upsert: JWTs carry second-granular
// iat/exp claims, so two logins by the same player within the same second
// issue byte-identical tokens — the same token_hash must refresh the existing
// row instead of failing the primary-key constraint (works on both PostgreSQL
// and SQLite).
func (r *SessionRepo) CreateSession(ctx context.Context, session *models.Session) error {
	_, err := r.db.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, player_id, created_at, expires_at, last_seen)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (token_hash) DO UPDATE SET
			expires_at = excluded.expires_at,
			last_seen = excluded.last_seen`,
		session.TokenHash,
		session.PlayerID,
		session.CreatedAt,
		session.ExpiresAt,
		session.LastSeen,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by token hash. Returns nil, nil when absent.
func (r *SessionRepo) GetSession(ctx context.Context, tokenHash string) (*models.Session, error) {
	s := &models.Session{}
	err := r.db.db.QueryRowContext(ctx, `
		SELECT token_hash, player_id, created_at, expires_at, last_seen
		FROM sessions WHERE token_hash = $1`,
		tokenHash,
	).Scan(&s.TokenHash, &s.PlayerID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

// TouchSession validates and refreshes a session in a single statement: the
// UPDATE only matches a row that exists and has not expired, so a zero
// rows-affected result means "reject this token".
func (r *SessionRepo) TouchSession(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	res, err := r.db.db.ExecContext(ctx, `
		UPDATE sessions SET last_seen = $2
		WHERE token_hash = $1 AND expires_at > $2`,
		tokenHash, now,
	)
	if err != nil {
		return false, fmt.Errorf("touch session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("touch session rows affected: %w", err)
	}
	return n > 0, nil
}

// DeleteSession removes a single session.
func (r *SessionRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.db.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsForPlayer removes every session belonging to a player.
func (r *SessionRepo) DeleteSessionsForPlayer(ctx context.Context, playerID string) error {
	if _, err := r.db.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE player_id = $1`, playerID); err != nil {
		return fmt.Errorf("delete sessions for player: %w", err)
	}
	return nil
}

// DeleteExpiredSessions purges all sessions past their expiry.
func (r *SessionRepo) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions rows affected: %w", err)
	}
	return n, nil
}
