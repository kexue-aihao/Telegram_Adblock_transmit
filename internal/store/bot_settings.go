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

// BotSettingsRepository persists the panel's runtime switches. The single row
// is identified by id = 1; a missing row means the panel keeps using the
// environment defaults.
type BotSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewBotSettingsRepository(pool *pgxpool.Pool) *BotSettingsRepository {
	return &BotSettingsRepository{pool: pool}
}

var _ ports.BotSettingsStore = (*BotSettingsRepository)(nil)

func (r *BotSettingsRepository) GetBotSettings(ctx context.Context) (domain.BotSettings, error) {
	if r == nil || r.pool == nil {
		return domain.BotSettings{}, errors.New("bot settings repository is nil")
	}
	var settings domain.BotSettings
	err := r.pool.QueryRow(ctx,
		`SELECT bio_check_enabled, cross_group_management, owner_user_ids FROM bot_settings WHERE id = 1`,
	).Scan(&settings.BioCheckEnabled, &settings.CrossGroupManagement, &settings.OwnerUserIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BotSettings{}, ports.ErrBotSettingsNotFound
	}
	if err != nil {
		return domain.BotSettings{}, fmt.Errorf("read bot settings: %w", err)
	}
	return settings, nil
}

func (r *BotSettingsRepository) SaveBotSettings(ctx context.Context, settings domain.BotSettings) error {
	if r == nil || r.pool == nil {
		return errors.New("bot settings repository is nil")
	}
	owners := settings.OwnerUserIDs
	if owners == nil {
		owners = []int64{}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bot_settings (id, bio_check_enabled, cross_group_management, owner_user_ids, updated_at)
		VALUES (1, $1, $2, $3, NOW())
		ON CONFLICT (id) DO UPDATE SET
			bio_check_enabled = EXCLUDED.bio_check_enabled,
			cross_group_management = EXCLUDED.cross_group_management,
			owner_user_ids = EXCLUDED.owner_user_ids,
			updated_at = NOW()`, settings.BioCheckEnabled, settings.CrossGroupManagement, owners)
	if err != nil {
		return fmt.Errorf("save bot settings: %w", err)
	}
	return nil
}
