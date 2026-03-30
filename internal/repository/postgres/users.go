package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mojomast/stallionussy/internal/models"
	"github.com/mojomast/stallionussy/internal/repository"
)

// Compile-time interface check.
var _ repository.UserRepository = (*UserRepo)(nil)

// UserRepo implements repository.UserRepository backed by PostgreSQL.
type UserRepo struct {
	db *DB
}

// NewUserRepo returns a new UserRepo.
func NewUserRepo(db *DB) *UserRepo {
	return &UserRepo{db: db}
}

// CreateUser persists a new user record.
func (r *UserRepo) CreateUser(ctx context.Context, user *models.User) error {
	query := `
		INSERT INTO users (id, username, password_hash, display_name, token_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.db.ExecContext(ctx, query,
		user.ID,
		user.Username,
		user.PasswordHash,
		user.DisplayName,
		user.TokenVersion,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUserByID retrieves a user by their unique ID.
func (r *UserRepo) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	query := `
		SELECT id, username, password_hash, display_name, token_version, created_at, updated_at
		FROM users WHERE id = $1`
	u := &models.User{}
	err := r.db.db.QueryRowContext(ctx, query, id).Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.DisplayName,
		&u.TokenVersion,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

// GetUserByUsername retrieves a user by their unique username (case-insensitive).
func (r *UserRepo) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `
		SELECT id, username, password_hash, display_name, token_version, created_at, updated_at
		FROM users WHERE LOWER(username) = LOWER($1)`
	u := &models.User{}
	err := r.db.db.QueryRowContext(ctx, query, username).Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.DisplayName,
		&u.TokenVersion,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

// UpdateUser saves changes to an existing user record.
func (r *UserRepo) UpdateUser(ctx context.Context, user *models.User) error {
	query := `
		UPDATE users
		SET username = $2, password_hash = $3, display_name = $4, token_version = $5, updated_at = $6
		WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query,
		user.ID,
		user.Username,
		user.PasswordHash,
		user.DisplayName,
		user.TokenVersion,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found: %s", user.ID)
	}
	return nil
}

// GetTokenVersion returns the current token_version for a user.
func (r *UserRepo) GetTokenVersion(ctx context.Context, userID string) (int, error) {
	query := `SELECT token_version FROM users WHERE id = $1`
	var version int
	err := r.db.db.QueryRowContext(ctx, query, userID).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("user not found: %s", userID)
	}
	if err != nil {
		return 0, fmt.Errorf("get token version: %w", err)
	}
	return version, nil
}

// IncrementTokenVersion bumps the user's token_version by 1, invalidating
// all previously issued JWTs.
func (r *UserRepo) IncrementTokenVersion(ctx context.Context, userID string) error {
	query := `UPDATE users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1`
	result, err := r.db.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("increment token version: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment token version rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}
	return nil
}
