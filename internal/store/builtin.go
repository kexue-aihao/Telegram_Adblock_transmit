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

type BuiltinSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewBuiltinSettingsRepository(pool *pgxpool.Pool) *BuiltinSettingsRepository {
	return &BuiltinSettingsRepository{pool: pool}
}

var _ ports.BuiltinSettingsStore = (*BuiltinSettingsRepository)(nil)

func (r *BuiltinSettingsRepository) GetBuiltinSettings(ctx context.Context) (domain.BuiltinSettings, error) {
	var settings domain.BuiltinSettings
	err := r.pool.QueryRow(ctx,
		"SELECT enabled, disabled_rules FROM builtin_settings WHERE id = 1",
	).Scan(&settings.Enabled, &settings.DisabledRules)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, ports.ErrBuiltinSettingsNotFound
	}
	if err != nil {
		return settings, fmt.Errorf("read builtin settings: %w", err)
	}
	return settings, nil
}

func (r *BuiltinSettingsRepository) SaveBuiltinSettings(ctx context.Context, settings domain.BuiltinSettings) error {
	disabled := settings.DisabledRules
	if disabled == nil {
		disabled = []string{}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO builtin_settings (id, enabled, disabled_rules, updated_at)
		VALUES (1, $1, $2, NOW())
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			disabled_rules = EXCLUDED.disabled_rules,
			updated_at = NOW()`, settings.Enabled, disabled)
	if err != nil {
		return fmt.Errorf("save builtin settings: %w", err)
	}
	return nil
}
