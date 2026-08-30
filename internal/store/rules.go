package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
)

var ErrRuleNotFound = errors.New("moderation rule not found")

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
	if _, err := rules.ValidatePattern(input.Pattern); err != nil {
		return domain.Rule{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Rule{}, fmt.Errorf("begin add rule transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `
		INSERT INTO chat_groups (chat_id, title)
		VALUES ($1, $2)
		ON CONFLICT (chat_id) DO UPDATE SET
			title = COALESCE(EXCLUDED.title, chat_groups.title),
			updated_at = NOW()`, input.ChatID, nullableTitle(input.ChatTitle)); err != nil {
		return domain.Rule{}, fmt.Errorf("ensure chat group: %w", err)
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
var _ interface {
	LoadEnabled(context.Context) (map[int64][]domain.Rule, error)
	Add(context.Context, domain.NewRule) (domain.Rule, error)
	List(context.Context, int64) ([]domain.Rule, error)
	Remove(context.Context, int64, int64) error
	SetEnabled(context.Context, int64, int64, bool) error
} = (*RuleRepository)(nil)
