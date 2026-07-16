package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetAppConfig reads a value from the app_config key/value store.
// Returns "" (no error) when the key does not exist.
func (d *DB) GetAppConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx,
		`SELECT value FROM app_config WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get app config %q: %w", key, err)
	}
	return value, nil
}

// SetAppConfig upserts a value in the app_config key/value store.
func (d *DB) SetAppConfig(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO app_config (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("set app config %q: %w", key, err)
	}
	return nil
}
