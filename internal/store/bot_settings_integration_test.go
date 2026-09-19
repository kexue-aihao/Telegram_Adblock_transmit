package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
	"github.com/kexue-aihao/telegram-adblock-transmit/migrations"
)

func TestBotSettingsRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL repository integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("test_bot_settings_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE") }()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repo := NewBotSettingsRepository(pool)
	if _, err := repo.GetBotSettings(ctx); !errors.Is(err, ports.ErrBotSettingsNotFound) {
		t.Fatalf("missing settings: %v", err)
	}

	// Environment defaults apply until something is saved.
	manager, err := settings.NewManager(ctx, domain.BotSettings{BioCheckEnabled: true, OwnerUserIDs: []int64{42}}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if current := manager.Settings(); !current.BioCheckEnabled || !current.IsOwner(42) {
		t.Fatalf("environment defaults were not used: %+v", current)
	}

	on := true
	saved, err := manager.Update(ctx, domain.BotSettingsPatch{CrossGroupManagement: &on, OwnerUserIDs: &[]int64{7, 42}})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.CrossGroupManagement || len(saved.OwnerUserIDs) != 2 {
		t.Fatalf("saved settings = %+v", saved)
	}
	// A new process must observe the stored row, not the environment defaults.
	reloaded, err := settings.NewManager(ctx, domain.BotSettings{}, repo)
	if err != nil {
		t.Fatal(err)
	}
	current := reloaded.Settings()
	if !current.CrossGroupManagement || !current.BioCheckEnabled || !slices.Equal(current.OwnerUserIDs, []int64{7, 42}) {
		t.Fatalf("stored settings were not loaded: %+v", current)
	}
	// An empty owner list round-trips as an empty array, never as NULL.
	if _, err := reloaded.Update(ctx, domain.BotSettingsPatch{OwnerUserIDs: &[]int64{}}); err != nil {
		t.Fatal(err)
	}
	cleared, err := repo.GetBotSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.OwnerUserIDs) != 0 {
		t.Fatalf("owner list did not clear: %v", cleared.OwnerUserIDs)
	}
}
