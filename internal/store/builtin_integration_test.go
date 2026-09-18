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

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/migrations"
)

func TestBuiltinSettingsRepositoryIntegration(t *testing.T) {
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
	schema := fmt.Sprintf("test_builtin_%d", time.Now().UnixNano())
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
	repo := NewBuiltinSettingsRepository(pool)
	if _, err := repo.GetBuiltinSettings(ctx); !errors.Is(err, ports.ErrBuiltinSettingsNotFound) {
		t.Fatalf("missing settings: %v", err)
	}
	checker, err := builtin.NewManaged(ctx, true, repo)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := checker.Update(ctx, &off, map[string]bool{builtin.HitBotMention: false}); err != nil {
		t.Fatal(err)
	}
	restarted, err := builtin.NewManaged(ctx, true, NewBuiltinSettingsRepository(pool))
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Enabled() || !slices.Contains(restarted.Settings().DisabledRules, builtin.HitBotMention) {
		t.Fatal("restart lost database overrides")
	}
	on := true
	if _, err := restarted.Update(ctx, &on, map[string]bool{builtin.HitBotMention: true}); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.GetBuiltinSettings(ctx)
	if err != nil || !saved.Enabled || len(saved.DisabledRules) != 0 {
		t.Fatalf("re-enable round trip: %+v, %v", saved, err)
	}
}
