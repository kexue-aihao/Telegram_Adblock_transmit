package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

type AuditRepository struct {
	pool *pgxpool.Pool
}

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository { return &AuditRepository{pool: pool} }

func (r *AuditRepository) Record(ctx context.Context, entry domain.NewAuditEntry) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("audit repository is nil")
	}
	if err := r.ensureGroup(ctx, entry.ChatID, entry.ChatTitle); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(entry.Content))
	summary := truncateRunes(entry.Content, domain.AuditSummaryLimit)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO moderation_audit_logs
		(chat_id, message_thread_id, user_id, message_id, matched_rule_ids,
		 content_sha256, content_summary, delete_succeeded, deletion_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ChatID, entry.MessageThreadID, nullableUserID(entry.UserID), entry.MessageID,
		entry.MatchedRuleIDs, hex.EncodeToString(hash[:]), summary,
		entry.DeleteSucceeded, nullableString(entry.DeletionError))
	if err != nil {
		return fmt.Errorf("record moderation audit: %w", err)
	}
	return nil
}

func (r *AuditRepository) ListRecent(ctx context.Context, chatID int64, limit int) ([]domain.AuditEntry, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("audit repository is nil")
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, chat_id, message_thread_id, user_id, message_id,
		       matched_rule_ids, content_sha256, content_summary,
		       delete_succeeded, COALESCE(deletion_error, ''), occurred_at
		FROM moderation_audit_logs
		WHERE chat_id = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("list moderation audit: %w", err)
	}
	defer rows.Close()
	entries := make([]domain.AuditEntry, 0, limit)
	for rows.Next() {
		var entry domain.AuditEntry
		if err := rows.Scan(&entry.ID, &entry.ChatID, &entry.MessageThreadID, &entry.UserID,
			&entry.MessageID, &entry.MatchedRuleIDs, &entry.ContentSHA256, &entry.ContentSummary,
			&entry.DeleteSucceeded, &entry.DeletionError, &entry.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan moderation audit: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate moderation audit: %w", err)
	}
	return entries, nil
}

func (r *AuditRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("audit repository is nil")
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM moderation_audit_logs WHERE occurred_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired audit: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *AuditRepository) ensureGroup(ctx context.Context, chatID int64, title string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO chat_groups (chat_id, title) VALUES ($1, $2)
		ON CONFLICT (chat_id) DO UPDATE SET
		  title = COALESCE(EXCLUDED.title, chat_groups.title), updated_at = NOW()`,
		chatID, nullableString(title))
	if err != nil {
		return fmt.Errorf("ensure chat group for audit: %w", err)
	}
	return nil
}

func nullableUserID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

var _ interface {
	Record(context.Context, domain.NewAuditEntry) error
	ListRecent(context.Context, int64, int) ([]domain.AuditEntry, error)
	DeleteExpired(context.Context, time.Time) (int64, error)
} = (*AuditRepository)(nil)
