package store

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
)

var ErrRuleNotFound = errors.New("moderation rule not found")
var ErrRuleLimitExceeded = errors.New("moderation rule quota exceeded")

var errRuleRepositoryNil = errors.New("rule repository is nil")

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
		WHERE enabled = TRUE
		ORDER BY chat_id ASC, id ASC`)
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
		result[rule.ChatID] = append(result[rule.ChatID], rule)
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
	// Serialize quota checks and inserts for the same chat. Without a
	// transaction-scoped lock, concurrent administrators could both observe
	// available quota and exceed the per-chat limits.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, input.ChatID); err != nil {
		return domain.Rule{}, fmt.Errorf("lock chat rule quota: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO chat_groups (chat_id, title)
		VALUES ($1, $2)
		ON CONFLICT (chat_id) DO UPDATE SET
			title = COALESCE(EXCLUDED.title, chat_groups.title),
			updated_at = NOW()`, input.ChatID, nullableTitle(input.ChatTitle)); err != nil {
		return domain.Rule{}, fmt.Errorf("ensure chat group: %w", err)
	}

	var ruleCount, patternChars int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(char_length(pattern)), 0)
		FROM moderation_rules WHERE chat_id = $1`, input.ChatID).Scan(&ruleCount, &patternChars); err != nil {
		return domain.Rule{}, fmt.Errorf("check moderation rule quota: %w", err)
	}
	newPatternChars := int64(utf8.RuneCountInString(input.Pattern))
	if ruleCount >= domain.MaxRulesPerChat || patternChars+newPatternChars > domain.MaxPatternTotalLength {
		return domain.Rule{}, fmt.Errorf("%w: max %d rules and %d pattern characters per chat", ErrRuleLimitExceeded, domain.MaxRulesPerChat, domain.MaxPatternTotalLength)
	}

	var result domain.Rule
	err = tx.QueryRow(ctx, `
		INSERT INTO moderation_rules (chat_id, pattern, enabled, created_by)
		VALUES ($1, $2, TRUE, $3)
		RETURNING id, chat_id, pattern, enabled, created_by, created_at, updated_at`,
		input.ChatID, input.Pattern, input.CreatedBy).Scan(
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
	rows, err := r.pool.Query(ctx, `
		SELECT id, chat_id, pattern, enabled, created_by, created_at, updated_at
		FROM moderation_rules
		WHERE chat_id = $1
		ORDER BY id ASC`, chatID)
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
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM moderation_rules WHERE chat_id = $1 AND id = $2`, chatID, ruleID)
	if err != nil {
		return fmt.Errorf("remove rule %d: %w", ruleID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func (r *RuleRepository) SetEnabled(ctx context.Context, chatID, ruleID int64, enabled bool) error {
	if err := r.validate(); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE moderation_rules
		SET enabled = $1, updated_at = NOW()
		WHERE chat_id = $2 AND id = $3`, enabled, chatID, ruleID)
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

func nullableTitle(title string) any {
	if title == "" {
		return nil
	}
	return title
}

// Compile-time assertion keeps accidental interface drift visible.
var _ ports.RuleStore = (*RuleRepository)(nil)
