package ports

import (
	"context"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// ChatStore lists every chat known to the bot for the panel's chat pickers.
type ChatStore interface {
	ListChats(ctx context.Context) ([]domain.ChatSummary, error)
}

// PanelAuditStore is the read-side view of the audit log the WebUI needs.
// It deliberately extends - not replaces - ports.AuditStore, which stays
// unchanged for the moderation service and retention cleanup.
type PanelAuditStore interface {
	ListAudit(ctx context.Context, q domain.AuditQuery) (domain.AuditPage, error)
	GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error)
	StatsOverview(ctx context.Context) (domain.AuditStatsOverview, error)
	StatsByDay(ctx context.Context, days int, chatID *int64) ([]domain.AuditDayStat, error)
}

// RuleCacheRefresher re-synchronizes the in-process rule cache after a panel
// write so the running bot enforces changes immediately. It is implemented by
// *moderation.Service (RefreshChatCache / LoadCache).
type RuleCacheRefresher interface {
	RefreshChatCache(ctx context.Context, chatID int64) error
	LoadCache(ctx context.Context) error
}
