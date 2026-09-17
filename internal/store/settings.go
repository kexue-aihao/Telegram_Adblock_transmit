package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// PanelSettingsRepository persists the WebUI login credentials changed from
// the panel settings page. The single row is identified by id = 1; a missing
// row means the panel should keep using the environment-provided credentials.
type PanelSettingsRepository struct {
	pool *pgxpool.Pool
}

// NewPanelSettingsRepository builds a repository over an existing connection
// pool.
func NewPanelSettingsRepository(pool *pgxpool.Pool) *PanelSettingsRepository {
	return &PanelSettingsRepository{pool: pool}
}

var _ ports.PanelSettingsStore = (*PanelSettingsRepository)(nil)

// GetPanelSettings loads the stored credentials. It returns
// ports.ErrPanelSettingsNotFound when no row has been created yet.
func (r *PanelSettingsRepository) GetPanelSettings(ctx context.Context) (domain.PanelCredentials, error) {
	if r == nil || r.pool == nil {
		return domain.PanelCredentials{}, errors.New("panel settings repository is nil")
	}
	var creds domain.PanelCredentials
	err := r.pool.QueryRow(ctx,
		`SELECT username, password_hash FROM panel_settings WHERE id = 1`,
	).Scan(&creds.Username, &creds.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PanelCredentials{}, ports.ErrPanelSettingsNotFound
	}
	if err != nil {
		return domain.PanelCredentials{}, fmt.Errorf("read panel settings: %w", err)
	}
	return creds, nil
}

// SavePanelSettings upserts the single settings row, refreshing updated_at so
// later installs can tell when credentials were last changed.
func (r *PanelSettingsRepository) SavePanelSettings(ctx context.Context, creds domain.PanelCredentials) error {
	if r == nil || r.pool == nil {
		return errors.New("panel settings repository is nil")
	}
	if creds.Username == "" || len(creds.PasswordHash) != 64 {
		return errors.New("panel credentials must include a username and a 64-char password hash")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO panel_settings (id, username, password_hash, updated_at)
		VALUES (1, $1, $2, NOW())
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			password_hash = EXCLUDED.password_hash,
			updated_at = NOW()`, creds.Username, creds.PasswordHash)
	if err != nil {
		return fmt.Errorf("save panel settings: %w", err)
	}
	return nil
}
