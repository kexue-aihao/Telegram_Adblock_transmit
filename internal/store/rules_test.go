package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestNullableTitle(t *testing.T) {
	if got := nullableTitle(""); got != nil {
		t.Fatalf("nullableTitle(\"\") = %#v, want nil", got)
	}
	if got := nullableTitle("group"); got != "group" {
		t.Fatalf("nullableTitle(group) = %#v, want group", got)
	}
}

// TestRuleRepositoryIntegration runs only when TEST_DATABASE_URL points at an
// isolated PostgreSQL database. It intentionally does not create or drop
// tables: migrations are owned by deployment and the test database must be
// provisioned separately.
func TestRuleRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL repository integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	// The schema must be present before this test. Use a unique, negative chat
	// ID to avoid colliding with real Telegram IDs in a shared test database.
	const chatID int64 = -9223372036854770000
	const creatorID int64 = 101
	repo := NewRuleRepository(pool)
	rule, err := repo.Add(ctx, domain.NewRule{ChatID: chatID, ChatTitle: "integration", Pattern: "spam", CreatedBy: creatorID})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Remove(context.Background(), chatID, rule.ID) }()
	if !rule.Enabled || rule.ChatID != chatID {
		t.Fatalf("unexpected added rule: %+v", rule)
	}
	listed, err := repo.List(ctx, chatID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("List() = %v, %v; want one rule", listed, err)
	}
	if err := repo.SetEnabled(ctx, chatID, rule.ID, false); err != nil {
		t.Fatal(err)
	}
	enabled, err := repo.LoadEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled[chatID]) != 0 {
		t.Fatalf("disabled rule returned from LoadEnabled: %v", enabled[chatID])
	}
	if err := repo.Remove(ctx, chatID, rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Remove(ctx, chatID, rule.ID); err != ErrRuleNotFound {
		t.Fatalf("second Remove() error = %v, want ErrRuleNotFound", err)
	}
}
