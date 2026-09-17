package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// TestPanelRepositoryIntegration runs only when TEST_DATABASE_URL points at an
// isolated PostgreSQL database (see scripts/integration.ps1, which applies the
// embedded migrations beforehand).
func TestPanelRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL repository integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	// Unique negative chat IDs avoid collisions with real Telegram IDs.
	const chatID int64 = -9223372036854770001
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_groups WHERE chat_id = $1`, chatID)
	}
	cleanup()
	defer cleanup()

	ruleRepo := NewRuleRepository(pool)
	auditRepo := NewAuditRepository(pool)

	// Seed two rules so UpdatePattern has an "other rules" quota to check.
	rule1, err := ruleRepo.Add(ctx, domain.NewRule{ChatID: chatID, ChatTitle: "面板集成", Pattern: "免费", CreatedBy: -1})
	if err != nil {
		t.Fatalf("seed rule 1: %v", err)
	}
	rule2, err := ruleRepo.Add(ctx, domain.NewRule{ChatID: chatID, ChatTitle: "面板集成", Pattern: "领取", CreatedBy: -1})
	if err != nil {
		t.Fatalf("seed rule 2: %v", err)
	}

	// UpdatePattern changes the pattern and touches updated_at.
	before := rule1.UpdatedAt
	time.Sleep(time.Millisecond)
	updated, err := ruleRepo.UpdatePattern(ctx, chatID, rule1.ID, "广告")
	if err != nil {
		t.Fatalf("UpdatePattern: %v", err)
	}
	if updated.Pattern != "广告" || !updated.UpdatedAt.After(before) {
		t.Fatalf("UpdatePattern result = %+v", updated)
	}

	// UpdatePattern on a missing rule → ErrRuleNotFound.
	if _, err := ruleRepo.UpdatePattern(ctx, chatID, rule1.ID+9999, "x"); err != ErrRuleNotFound {
		t.Fatalf("UpdatePattern missing rule error = %v, want ErrRuleNotFound", err)
	}

	// UpdatePattern beyond the total-pattern quota → ErrRuleLimitExceeded and
	// the pattern must be unchanged afterwards.
	newPattern := strings.Repeat("超长", domain.MaxPatternLength/2)
	_, err = ruleRepo.UpdatePattern(ctx, chatID, rule2.ID, newPattern)
	if err != ErrRuleLimitExceeded {
		t.Fatalf("UpdatePattern over-quota error = %v, want ErrRuleLimitExceeded", err)
	}
	verify, err := ruleRepo.List(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range verify {
		if rule.ID == rule2.ID && rule.Pattern != "领取" {
			t.Fatalf("over-quota update changed the pattern: %+v", rule)
		}
	}

	// ListChats reports the seeded chat with its counts.
	chats, err := ruleRepo.ListChats(ctx)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	var found *domain.ChatSummary
	for i := range chats {
		if chats[i].ID == chatID {
			found = &chats[i]
		}
	}
	if found == nil {
		t.Fatal("ListChats did not return the seeded chat")
	}
	if found.RuleCount != 2 || found.EnabledCount != 2 || found.Title != "面板集成" {
		t.Fatalf("ListChats summary = %+v", found)
	}

	// Seed audit history spread over two distinct days for stats assertions.
	userID := int64(102)
	now := time.Now().UTC()
	if err := auditRepo.Record(ctx, domain.NewAuditEntry{
		ChatID: chatID, ChatTitle: "面板集成", UserID: &userID, MessageID: 1,
		MatchedRuleIDs: []int64{rule1.ID}, Content: "免费领取", DeleteSucceeded: true,
	}); err != nil {
		t.Fatalf("seed audit 1: %v", err)
	}
	if err := auditRepo.Record(ctx, domain.NewAuditEntry{
		ChatID: chatID, ChatTitle: "面板集成", UserID: &userID, MessageID: 2,
		MatchedRuleIDs: []int64{rule1.ID, rule2.ID}, Content: "广告轰炸",
		DeleteSucceeded: false, DeletionError: "message not found",
	}); err != nil {
		t.Fatalf("seed audit 2: %v", err)
	}

	// ListAudit: default page returns everything, newest first.
	page, err := auditRepo.ListAudit(ctx, domain.AuditQuery{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("ListAudit default = total %d, items %d; want 2/2", page.Total, len(page.Items))
	}

	// ListAudit: filter by success only and by rule id.
	failTrue := true
	failed, err := auditRepo.ListAudit(ctx, domain.AuditQuery{Success: &failTrue})
	if err != nil || failed.Total != 1 || failed.Items[0].MessageID != 2 {
		t.Fatalf("ListAudit success-filter = %+v, %v", failed, err)
	}
	ruleFilter := rule1.ID
	matched, err := auditRepo.ListAudit(ctx, domain.AuditQuery{RuleID: &ruleFilter})
	if err != nil || matched.Total != 2 {
		t.Fatalf("ListAudit rule-filter total = %d, %v", matched.Total, err)
	}

	// ListAudit: pagination splits the two-row page.
	small, err := auditRepo.ListAudit(ctx, domain.AuditQuery{Page: 1, PageSize: 1})
	if err != nil || len(small.Items) != 1 || small.Total != 2 {
		t.Fatalf("ListAudit page1 = %+v, %v", small, err)
	}

	// GetAudit: found and missing (ErrAuditNotFound).
	entry, err := auditRepo.GetAudit(ctx, page.Items[0].ID)
	if err != nil || entry.ID != page.Items[0].ID {
		t.Fatalf("GetAudit = %+v, %v", entry, err)
	}
	if _, err := auditRepo.GetAudit(ctx, -1); err != ErrAuditNotFound {
		t.Fatalf("GetAudit missing error = %v, want ErrAuditNotFound", err)
	}

	// StatsOverview has nonzero counters for the seeded data.
	overview, err := auditRepo.StatsOverview(ctx)
	if err != nil {
		t.Fatalf("StatsOverview: %v", err)
	}
	if overview.HitsToday != 2 || overview.FailedToday != 1 || overview.TotalRules < 2 {
		t.Fatalf("StatsOverview = %+v", overview)
	}

	// StatsByDay returns exactly `days` points, one of which carries today's hits.
	days, err := auditRepo.StatsByDay(ctx, 5, nil)
	if err != nil {
		t.Fatalf("StatsByDay: %v", err)
	}
	if len(days) != 5 {
		t.Fatalf("StatsByDay length = %d, want 5", len(days))
	}
	today := days[len(days)-1]
	if today.Date.Format("2006-01-02") != now.Format("2006-01-02") {
		t.Fatalf("StatsByDay last date = %s, want %s", today.Date.Format("2006-01-02"), now.Format("2006-01-02"))
	}
	if today.Hits < 2 {
		t.Fatalf("StatsByDay hits today = %d, want >= 2", today.Hits)
	}
	if days[0].Hits != 0 {
		t.Fatalf("StatsByDay zero-fill failed: %+v", days[0])
	}

	// Panel settings round-trip: absent row returns a sentinel, saves upsert
	// the single row, and a second save overwrites the first.
	settingsRepo := NewPanelSettingsRepository(pool)
	_, err = settingsRepo.GetPanelSettings(ctx)
	if !errors.Is(err, ports.ErrPanelSettingsNotFound) {
		t.Fatalf("GetPanelSettings on empty table = %v, want ErrPanelSettingsNotFound", err)
	}
	if err := settingsRepo.SavePanelSettings(ctx, domain.PanelCredentials{Username: "owner", PasswordHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
		t.Fatalf("SavePanelSettings (1): %v", err)
	}
	if err := settingsRepo.SavePanelSettings(ctx, domain.PanelCredentials{Username: "owner2", PasswordHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}); err != nil {
		t.Fatalf("SavePanelSettings (2): %v", err)
	}
	creds, err := settingsRepo.GetPanelSettings(ctx)
	if err != nil {
		t.Fatalf("GetPanelSettings after save: %v", err)
	}
	if creds.Username != "owner2" || creds.PasswordHash != strings.Repeat("b", 64) {
		t.Fatalf("GetPanelSettings = %+v, want updated owner2", creds)
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM panel_settings WHERE id = 1`)

	// Assertions keep interface drift visible at compile time.
	var _ ports.PanelSettingsStore = settingsRepo
	var _ interface {
		ListAudit(context.Context, domain.AuditQuery) (domain.AuditPage, error)
		GetAudit(context.Context, int64) (domain.AuditEntry, error)
		StatsOverview(context.Context) (domain.AuditStatsOverview, error)
		StatsByDay(context.Context, int, *int64) ([]domain.AuditDayStat, error)
	} = auditRepo
	var _ interface {
		ListChats(context.Context) ([]domain.ChatSummary, error)
		UpdatePattern(context.Context, int64, int64, string) (domain.Rule, error)
	} = ruleRepo
}

func TestPanelAuditBuiltinHitsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL repository integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const chatID int64 = -9223372036854770002
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_groups WHERE chat_id = $1`, chatID)
	}
	cleanup()
	defer cleanup()

	auditRepo := NewAuditRepository(pool)
	entry := domain.NewAuditEntry{
		ChatID: chatID, ChatTitle: "内置测试", MessageID: 7,
		Content: "进群 https://t.me/+abc", BuiltinHits: []string{"ad_invite_link", "ad_bot_mention"},
		DeleteSucceeded: true,
	}
	if err := auditRepo.Record(ctx, entry); err != nil {
		t.Fatalf("Record with builtin hits: %v", err)
	}
	recent, err := auditRepo.ListRecent(ctx, chatID, 5)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("ListRecent len = %d, want 1", len(recent))
	}
	if !slices.Equal(recent[0].BuiltinHits, entry.BuiltinHits) {
		t.Fatalf("BuiltinHits = %v, want %v", recent[0].BuiltinHits, entry.BuiltinHits)
	}
}
