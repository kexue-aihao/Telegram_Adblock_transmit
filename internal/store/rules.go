package store

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
)

var ErrRuleNotFound = errors.New("moderation rule not found")
var ErrRuleLimitExceeded = errors.New("moderation rule quota exceeded")

var errRuleRepositoryNil = errors.New("rule repository is nil")

// GlobalRuleChatID is the sentinel chat that owns every rule since rules are
// shared across all groups. Migration 0005 merges the old per-chat rules into
// this row; the code below ignores the chatID parameters callers still pass
// (the port signatures stayed stable to limit ripple) and always operates on
// the global set.
const GlobalRuleChatID int64 = -1

const globalRuleChatTitle = "全局规则"

// RuleRepository is the PostgreSQL implementation of ports.RuleStore. A
// pgxpool.Pool is safe for concurrent use by update handlers and the polling
// loop.
type RuleRepository struct {
	pool *pgxpool.Pool
}

func NewRuleRepository(pool *pgxpool.Pool) *RuleRepository {
	return &RuleRepository{pool: pool}
}

func (r *RuleRepository) LoadEnabled(ctx context.Context) (map[int64][]domain.Rule, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, chat_id, pattern, enabled, created_by, created_at, updated_at
		FROM moderation_rules
		WHERE enabled = TRUE AND chat_id = $1
		ORDER BY id ASC`, GlobalRuleChatID)
	if err != nil {
		return nil, fmt.Errorf("load enabled rules: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]domain.Rule)
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan enabled rule: %w", scanErr)
		}
		result[GlobalRuleChatID] = append(result[GlobalRuleChatID], rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled rules: %w", err)
	}
	return result, nil
}

func (r *RuleRepository) Add(ctx context.Context, input domain.NewRule) (domain.Rule, error) {
	if err := r.validate(); err != nil {
		return domain.Rule{}, err
	}
	if _, err := rules.ValidatePattern(input.Pattern); err != nil {
		return domain.Rule{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Rule{}, fmt.Errorf("begin add rule transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize quota checks and inserts for the global rule set. Without a
	// transaction-scoped lock, concurrent writers could both observe available
	// quota and exceed the limits.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, GlobalRuleChatID); err != nil {
		return domain.Rule{}, fmt.Errorf("lock global rule quota: %w", err)
	}
	// The sentinel row holds the FK for global rules; created by migration 0005
	// but upserted again here so a partially migrated database still works.
	if _, err = tx.Exec(ctx, `
		INSERT INTO chat_groups (chat_id, title)
		VALUES ($1, $2)
		ON CONFLICT (chat_id) DO UPDATE SET
			title = COALESCE(EXCLUDED.title, chat_groups.title),
			updated_at = NOW()`, GlobalRuleChatID, globalRuleChatTitle); err != nil {
		return domain.Rule{}, fmt.Errorf("ensure global chat group: %w", err)
	}

	var ruleCount, patternChars int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(char_length(pattern)), 0)
		FROM moderation_rules WHERE chat_id = $1`, GlobalRuleChatID).Scan(&ruleCount, &patternChars); err != nil {
		return domain.Rule{}, fmt.Errorf("check moderation rule quota: %w", err)
	}
	newPatternChars := int64(utf8.RuneCountInString(input.Pattern))
	if ruleCount >= domain.MaxRulesPerChat || patternChars+newPatternChars > domain.MaxPatternTotalLength {
		return domain.Rule{}, fmt.Errorf("%w: max %d rules and %d pattern characters", ErrRuleLimitExceeded, domain.MaxRulesPerChat, domain.MaxPatternTotalLength)
	}

	var result domain.Rule
	err = tx.QueryRow(ctx, `
		INSERT INTO moderation_rules (chat_id, pattern, enabled, created_by)
		VALUES ($1, $2, TRUE, $3)
		RETURNING id, chat_id, pattern, enabled, created_by, created_at, updated_at`,
		GlobalRuleChatID, input.Pattern, input.CreatedBy).Scan(
		&result.ID, &result.ChatID, &result.Pattern, &result.Enabled,
		&result.CreatedBy, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return domain.Rule{}, fmt.Errorf("insert moderation rule: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Rule{}, fmt.Errorf("commit add rule: %w", err)
	}
	return result, nil
}

func (r *RuleRepository) List(ctx context.Context, chatID int64) ([]domain.Rule, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	_ = chatID // rules are global; the chat_id parameter is ignored
	rows, err := r.pool.Query(ctx, `
		SELECT id, chat_id, pattern, enabled, created_by, created_at, updated_at
		FROM moderation_rules
		WHERE chat_id = $1
		ORDER BY id ASC`, GlobalRuleChatID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Rule, 0)
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan rule: %w", scanErr)
		}
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rules: %w", err)
	}
	return result, nil
}

func (r *RuleRepository) Remove(ctx context.Context, chatID, ruleID int64) error {
	if err := r.validate(); err != nil {
		return err
	}
	_ = chatID // rules are global
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM moderation_rules WHERE chat_id = $1 AND id = $2`, GlobalRuleChatID, ruleID)
	if err != nil {
		return fmt.Errorf("remove rule %d: %w", ruleID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

// UpdatePattern validates and rewrites one rule's pattern, re-checking the
// global total-pattern quota against the other rules. It mirrors Add's
// transaction shape (advisory lock + quota check) so the limits stay
// guaranteed even with concurrent panel and command writes.
func (r *RuleRepository) UpdatePattern(ctx context.Context, chatID, ruleID int64, pattern string) (domain.Rule, error) {
	if err := r.validate(); err != nil {
		return domain.Rule{}, err
	}
	if _, err := rules.ValidatePattern(pattern); err != nil {
		return domain.Rule{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Rule{}, fmt.Errorf("begin update rule transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, GlobalRuleChatID); err != nil {
		return domain.Rule{}, fmt.Errorf("lock global rule quota: %w", err)
	}

	var otherPatternChars int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(char_length(pattern)), 0)
		FROM moderation_rules WHERE chat_id = $1 AND id <> $2`, GlobalRuleChatID, ruleID).Scan(&otherPatternChars); err != nil {
		return domain.Rule{}, fmt.Errorf("check moderation rule quota: %w", err)
	}
	newPatternChars := int64(utf8.RuneCountInString(pattern))
	if otherPatternChars+newPatternChars > domain.MaxPatternTotalLength {
		return domain.Rule{}, fmt.Errorf("%w: max %d pattern characters", ErrRuleLimitExceeded, domain.MaxPatternTotalLength)
	}

	var result domain.Rule
	err = tx.QueryRow(ctx, `
		UPDATE moderation_rules
		SET pattern = $1, updated_at = NOW()
		WHERE chat_id = $2 AND id = $3
		RETURNING id, chat_id, pattern, enabled, created_by, created_at, updated_at`,
		pattern, GlobalRuleChatID, ruleID).Scan(
		&result.ID, &result.ChatID, &result.Pattern, &result.Enabled,
		&result.CreatedBy, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Rule{}, ErrRuleNotFound
		}
		return domain.Rule{}, fmt.Errorf("update rule %d: %w", ruleID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Rule{}, fmt.Errorf("commit update rule: %w", err)
	}
	return result, nil
}

// ListChats lists every known chat. Rule counts are no longer reported since
// moderation rules are global; the dashboard totals come from StatsOverview.
func (r *RuleRepository) ListChats(ctx context.Context) ([]domain.ChatSummary, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT chat_id, COALESCE(title, '')
		FROM chat_groups
		ORDER BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("list chat groups: %w", err)
	}
	defer rows.Close()
	chats := make([]domain.ChatSummary, 0)
	for rows.Next() {
		var chat domain.ChatSummary
		if err := rows.Scan(&chat.ID, &chat.Title); err != nil {
			return nil, fmt.Errorf("scan chat group: %w", err)
		}
		chats = append(chats, chat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat groups: %w", err)
	}
	return chats, nil
}

func (r *RuleRepository) SetEnabled(ctx context.Context, chatID, ruleID int64, enabled bool) error {
	if err := r.validate(); err != nil {
		return err
	}
	_ = chatID // rules are global
	tag, err := r.pool.Exec(ctx, `
		UPDATE moderation_rules
		SET enabled = $1, updated_at = NOW()
		WHERE chat_id = $2 AND id = $3`, enabled, GlobalRuleChatID, ruleID)
	if err != nil {
		return fmt.Errorf("set rule %d enabled=%t: %w", ruleID, enabled, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func (r *RuleRepository) validate() error {
	if r == nil || r.pool == nil {
		return errRuleRepositoryNil
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRule(row rowScanner) (domain.Rule, error) {
	var result domain.Rule
	err := row.Scan(
		&result.ID, &result.ChatID, &result.Pattern, &result.Enabled,
		&result.CreatedBy, &result.CreatedAt, &result.UpdatedAt)
	return result, err
}

// nullableTitle retains the previous chat-title helper for callers that still
// pass titles; the global rule set no longer uses it.
func nullableTitle(title string) any {
	if title == "" {
		return nil
	}
	return title
}

// Compile-time assertions keep accidental interface drift visible.
var _ ports.RuleStore = (*RuleRepository)(nil)
var _ ports.ChatStore = (*RuleRepository)(nil)
