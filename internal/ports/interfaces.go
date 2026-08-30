package ports

import (
	"context"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

type RuleStore interface {
	LoadEnabled(ctx context.Context) (map[int64][]domain.Rule, error)
	Add(ctx context.Context, rule domain.NewRule) (domain.Rule, error)
	List(ctx context.Context, chatID int64) ([]domain.Rule, error)
	Remove(ctx context.Context, chatID, ruleID int64) error
	SetEnabled(ctx context.Context, chatID, ruleID int64, enabled bool) error
}

type RuleCache interface {
	Match(chatID int64, content string) []int64
	Replace(chatID int64, rules []domain.CompiledRule)
	Remove(chatID int64)
}

type AuditStore interface {
	Record(ctx context.Context, entry domain.NewAuditEntry) error
	ListRecent(ctx context.Context, chatID int64, limit int) ([]domain.AuditEntry, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type TelegramClient interface {
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
	SendMessage(ctx context.Context, chatID int64, threadID *int, text string) error
	IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error)
}
