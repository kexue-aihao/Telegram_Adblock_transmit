package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/migrations"
)

// Each audit test owns its schema, including migrations. No pre-existing data
// or deployment schema is modified, even when several tests share a server.
func isolatedAuditPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("test_audit_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop audit test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestPanelAuditBuiltinHitsIntegration(t *testing.T) {
	pool := isolatedAuditPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := NewAuditRepository(pool)
	chatID := int64(-100101)
	userID := int64(102)
	threadID := 23
	details := &domain.BuiltinDetails{
		LibraryVersion: "2.0.0",
		Hits: []domain.BuiltinHit{
			{ID: "ad_money_laundering", Name: "洗钱洗资广告", Category: "资金清洗", Evidence: []string{"资金清洗主题", "承接交易"}},
			{ID: "ad_keyword_link", Name: "推广与链接", Category: "导流", Evidence: []string{"明确推广", "隐藏链接"}},
		},
	}
	inputs := []domain.NewAuditEntry{
		// Legacy rows have hit IDs but no detailed explanation.
		{ChatID: chatID, ChatTitle: "内置审计测试", UserID: &userID, MessageID: 7,
			Content: "旧版内置命中", BuiltinHits: []string{"ad_invite_link"}, DeleteSucceeded: true},
		{ChatID: chatID, ChatTitle: "内置审计测试", UserID: &userID, MessageThreadID: &threadID, MessageID: 8,
			Content: strings.Repeat("原文Ａ\u200b😀\n", 40), BuiltinHits: []string{"ad_money_laundering", "ad_keyword_link"},
			BuiltinDetails: details, DeleteSucceeded: false, DeletionError: "insufficient rights"},
		// Custom-only hits must still store empty builtin arrays, not SQL NULL.
		{ChatID: chatID, ChatTitle: "内置审计测试", UserID: &userID, MessageID: 9,
			Content: "自定义规则", MatchedRuleIDs: []int64{88}, DeleteSucceeded: true},
	}
	for _, entry := range inputs {
		if err := repo.Record(ctx, entry); err != nil {
			t.Fatalf("Record message %d: %v", entry.MessageID, err)
		}
	}
	// Reapplying startup migrations must preserve both old and new events.
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, rows []domain.AuditEntry) {
		t.Helper()
		if len(rows) != len(inputs) {
			t.Fatalf("got %d audit rows, want %d", len(rows), len(inputs))
		}
		for _, got := range rows {
			index := slices.IndexFunc(inputs, func(entry domain.NewAuditEntry) bool { return entry.MessageID == got.MessageID })
			if index < 0 {
				t.Fatalf("unexpected audit: %+v", got)
			}
			want := inputs[index]
			if !slices.Equal(got.BuiltinHits, want.BuiltinHits) || !slices.Equal(got.MatchedRuleIDs, want.MatchedRuleIDs) || !reflect.DeepEqual(got.BuiltinDetails, want.BuiltinDetails) {
				t.Fatalf("message %d hit details changed: %+v", got.MessageID, got)
			}
			hash := sha256.Sum256([]byte(want.Content))
			if got.ContentSHA256 != hex.EncodeToString(hash[:]) || got.ContentSummary != truncateRunes(want.Content, domain.AuditSummaryLimit) {
				t.Fatalf("message %d original text hash or summary changed", got.MessageID)
			}
			if utf8.RuneCountInString(got.ContentSummary) > domain.AuditSummaryLimit || got.DeleteSucceeded != want.DeleteSucceeded || got.DeletionError != want.DeletionError || !reflect.DeepEqual(got.MessageThreadID, want.MessageThreadID) {
				t.Fatalf("message %d audit metadata changed: %+v", got.MessageID, got)
			}
		}
	}
	recent, err := repo.ListRecent(ctx, chatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	check(t, recent)
	page, err := repo.ListAudit(ctx, domain.AuditQuery{ChatID: &chatID})
	if err != nil || page.Total != int64(len(inputs)) {
		t.Fatalf("ListAudit: %+v, %v", page, err)
	}
	check(t, page.Items)
	var fetched []domain.AuditEntry
	for _, entry := range recent {
		got, err := repo.GetAudit(ctx, entry.ID)
		if err != nil {
			t.Fatal(err)
		}
		fetched = append(fetched, got)
	}
	check(t, fetched)
	var objects, missing int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE jsonb_typeof(builtin_details) = 'object'), COUNT(*) FILTER (WHERE builtin_details IS NULL) FROM moderation_audit_logs`).Scan(&objects, &missing); err != nil || objects != 1 || missing != 2 {
		t.Fatalf("JSONB object/legacy null storage: %d/%d, %v", objects, missing, err)
	}
}

func TestAuditDistinctMessagesIntegration(t *testing.T) {
	pool := isolatedAuditPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := NewAuditRepository(pool)
	const chatID int64 = -100202
	userID := int64(103)
	since := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	record := func(chat, user int64, message int, deleted bool) {
		t.Helper()
		if err := repo.Record(ctx, domain.NewAuditEntry{
			ChatID: chat, ChatTitle: "去重计次测试", UserID: &user, MessageID: message,
			BuiltinHits: []string{"ad_money_laundering", "ad_keyword_link"},
			Content:     "同一条消息的编辑或重投", DeleteSucceeded: deleted,
		}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int64) {
		t.Helper()
		got, err := repo.CountHits(ctx, chatID, userID, since)
		if err != nil || got != want {
			t.Fatalf("CountHits = %d, %v; want %d", got, err, want)
		}
	}
	record(chatID, userID, 1, false)
	record(chatID, userID, 1, false)
	record(chatID, userID, 1, true)
	count(1)
	record(chatID, userID, 2, true)
	record(chatID, userID, 3, true)
	count(3)
	// The same message ID in another group/user never adds to this counter.
	record(chatID-1, userID, 4, true)
	record(chatID, userID+1, 4, true)
	count(3)
	record(chatID, userID, 5, true)
	if _, err := pool.Exec(ctx, `UPDATE moderation_audit_logs SET occurred_at = $1 WHERE message_id = 5`, since.Add(-time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	count(3)
	// The window is inclusive at its start, preserving the existing policy.
	record(chatID, userID, 6, true)
	if _, err := pool.Exec(ctx, `UPDATE moderation_audit_logs SET occurred_at = $1 WHERE message_id = 6`, since); err != nil {
		t.Fatal(err)
	}
	count(4)
	rows, err := repo.ListRecent(ctx, chatID, 20)
	if err != nil || len(rows) != 8 {
		t.Fatalf("edits must remain independently auditable: %d, %v", len(rows), err)
	}
}
