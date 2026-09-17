package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

var ErrAuditNotFound = errors.New("moderation audit entry not found")

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
		 builtin_hits, content_sha256, content_summary, delete_succeeded, deletion_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		entry.ChatID, entry.MessageThreadID, nullableUserID(entry.UserID), entry.MessageID,
		entry.MatchedRuleIDs, entry.BuiltinHits, hex.EncodeToString(hash[:]), summary,
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
		       matched_rule_ids, builtin_hits, content_sha256, content_summary,
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
		entry, err := scanAuditRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan moderation audit: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate moderation audit: %w", err)
	}
	return entries, nil
}

// auditColumns is the shared SELECT column list for audit rows, kept in sync
// with scanAuditRow. Queries that need extra columns (e.g. a window COUNT)
// prepend/append their own and scan accordingly.
const auditColumns = `id, chat_id, message_thread_id, user_id, message_id,
	matched_rule_ids, builtin_hits, content_sha256, content_summary,
	delete_succeeded, COALESCE(deletion_error, ''), occurred_at`

func scanAuditRow(row interface{ Scan(dest ...any) error }) (domain.AuditEntry, error) {
	var entry domain.AuditEntry
	err := row.Scan(&entry.ID, &entry.ChatID, &entry.MessageThreadID, &entry.UserID,
		&entry.MessageID, &entry.MatchedRuleIDs, &entry.BuiltinHits, &entry.ContentSHA256, &entry.ContentSummary,
		&entry.DeleteSucceeded, &entry.DeletionError, &entry.OccurredAt)
	return entry, err
}

// CountHits counts moderation hits recorded for a user in a chat since the
// given instant. Audit rows are written only when a rule or built-in hit
// fired, so this doubles as the strike counter for the ban policy.
func (r *AuditRepository) CountHits(ctx context.Context, chatID, userID int64, since time.Time) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("audit repository is nil")
	}
	var count int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM moderation_audit_logs
		WHERE chat_id = $1 AND user_id = $2 AND occurred_at >= $3`,
		chatID, userID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count moderation hits: %w", err)
	}
	return count, nil
}

// ListAudit returns a page of audit entries matching q. Filters are optional;
// pagination values are normalized here (page >= 1, page size 1..100, default
// 20). The total is computed in the same round trip via a window COUNT(*).
func (r *AuditRepository) ListAudit(ctx context.Context, q domain.AuditQuery) (domain.AuditPage, error) {
	if r == nil || r.pool == nil {
		return domain.AuditPage{}, fmt.Errorf("audit repository is nil")
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var query strings.Builder
	query.WriteString("SELECT " + auditColumns + ", COUNT(*) OVER() AS _total FROM moderation_audit_logs WHERE 1 = 1")
	args := make([]any, 0, 6)
	arg := func(value any) int {
		args = append(args, value)
		return len(args)
	}
	if q.ChatID != nil {
		fmt.Fprintf(&query, ` AND chat_id = $%d`, arg(*q.ChatID))
	}
	if q.Success != nil {
		fmt.Fprintf(&query, ` AND delete_succeeded = $%d`, arg(*q.Success))
	}
	if q.RuleID != nil {
		fmt.Fprintf(&query, ` AND matched_rule_ids @> ARRAY[$%d]::bigint[]`, arg(*q.RuleID))
	}
	if q.From != nil {
		fmt.Fprintf(&query, ` AND occurred_at >= $%d`, arg(*q.From))
	}
	if q.To != nil {
		fmt.Fprintf(&query, ` AND occurred_at < $%d`, arg(*q.To))
	}
	fmt.Fprintf(&query, `
		ORDER BY occurred_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, arg(pageSize), arg((page-1)*pageSize))

	rows, err := r.pool.Query(ctx, query.String(), args...)
	if err != nil {
		return domain.AuditPage{}, fmt.Errorf("list panel audit: %w", err)
	}
	defer rows.Close()
	pageResult := domain.AuditPage{Page: page, PageSize: pageSize, Items: make([]domain.AuditEntry, 0, pageSize)}
	for rows.Next() {
		var entry domain.AuditEntry
		var total int64
		if err := rows.Scan(&entry.ID, &entry.ChatID, &entry.MessageThreadID, &entry.UserID,
			&entry.MessageID, &entry.MatchedRuleIDs, &entry.BuiltinHits, &entry.ContentSHA256, &entry.ContentSummary,
			&entry.DeleteSucceeded, &entry.DeletionError, &entry.OccurredAt, &total); err != nil {
			return domain.AuditPage{}, fmt.Errorf("scan panel audit: %w", err)
		}
		pageResult.Total = total
		pageResult.Items = append(pageResult.Items, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.AuditPage{}, fmt.Errorf("iterate panel audit: %w", err)
	}
	return pageResult, nil
}

// GetAudit returns one audit entry, or ErrAuditNotFound.
func (r *AuditRepository) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	if r == nil || r.pool == nil {
		return domain.AuditEntry{}, fmt.Errorf("audit repository is nil")
	}
	row := r.pool.QueryRow(ctx, `SELECT `+auditColumns+` FROM moderation_audit_logs WHERE id = $1`, id)
	entry, err := scanAuditRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuditEntry{}, ErrAuditNotFound
		}
		return domain.AuditEntry{}, fmt.Errorf("get audit %d: %w", id, err)
	}
	return entry, nil
}

// StatsOverview aggregates the panel dashboard headline counters.
func (r *AuditRepository) StatsOverview(ctx context.Context) (domain.AuditStatsOverview, error) {
	if r == nil || r.pool == nil {
		return domain.AuditStatsOverview{}, fmt.Errorf("audit repository is nil")
	}
	var stats domain.AuditStatsOverview
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM chat_groups),
			(SELECT COUNT(*) FROM moderation_rules),
			(SELECT COUNT(*) FROM moderation_rules WHERE enabled),
			(SELECT COUNT(*) FROM moderation_audit_logs WHERE occurred_at >= date_trunc('day', now())),
			(SELECT COUNT(*) FROM moderation_audit_logs WHERE delete_succeeded AND occurred_at >= date_trunc('day', now())),
			(SELECT COUNT(*) FROM moderation_audit_logs WHERE NOT delete_succeeded AND occurred_at >= date_trunc('day', now())),
			(SELECT COUNT(*) FROM moderation_audit_logs WHERE occurred_at >= now() - interval '7 days'),
			(SELECT COUNT(*) FROM moderation_audit_logs)`).
		Scan(&stats.TotalChats, &stats.TotalRules, &stats.EnabledRules,
			&stats.HitsToday, &stats.DeletedToday, &stats.FailedToday,
			&stats.Hits7Day, &stats.TotalHits)
	if err != nil {
		return domain.AuditStatsOverview{}, fmt.Errorf("aggregate panel stats: %w", err)
	}
	return stats, nil
}

// StatsByDay returns days points of aggregated moderation actions ordered
// chronologically. Day boundaries are UTC. The result is zero-filled so the
// frontend always receives exactly days entries regardless of activity gaps.
func (r *AuditRepository) StatsByDay(ctx context.Context, days int, chatID *int64) ([]domain.AuditDayStat, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("audit repository is nil")
	}
	if days < 1 {
		days = 1
	}
	query := `
		SELECT (occurred_at AT TIME ZONE 'UTC')::date,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE delete_succeeded),
		       COUNT(*) FILTER (WHERE NOT delete_succeeded)
		FROM moderation_audit_logs
		WHERE occurred_at >= $1`
	args := []any{time.Now().UTC().AddDate(0, 0, -(days - 1))}
	if chatID != nil {
		query += ` AND chat_id = $2`
		args = append(args, *chatID)
	}
	query += `
		GROUP BY 1
		ORDER BY 1`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate panel stats by day: %w", err)
	}
	defer rows.Close()

	byDay := make(map[string]domain.AuditDayStat, days)
	for rows.Next() {
		var day time.Time
		var stat domain.AuditDayStat
		// Scan the ::date column into time.Time: pgx refuses to decode the
		// Date OID into a *string over the binary protocol, which used to make
		// the trend endpoint 500 as soon as any audit row existed.
		if err := rows.Scan(&day, &stat.Hits, &stat.Deleted, &stat.Failed); err != nil {
			return nil, fmt.Errorf("scan panel day stat: %w", err)
		}
		stat.Date = day
		byDay[day.Format("2006-01-02")] = stat
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate panel day stats: %w", err)
	}

	// Zero-fill the whole window so the chart gets exactly days points.
	stats := make([]domain.AuditDayStat, 0, days)
	today := time.Now().UTC()
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		dateStr := day.Format("2006-01-02")
		stat, ok := byDay[dateStr]
		if !ok {
			stat = domain.AuditDayStat{Date: day}
		}
		stats = append(stats, stat)
	}
	return stats, nil
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
	CountHits(context.Context, int64, int64, time.Time) (int64, error)
	DeleteExpired(context.Context, time.Time) (int64, error)
} = (*AuditRepository)(nil)
var _ ports.PanelAuditStore = (*AuditRepository)(nil)
