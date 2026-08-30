package ports

import (
	"context"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// AuditStore is intentionally separate from RuleStore so moderation can record
// a deletion result without exposing SQL details to Telegram handlers.
type GroupStore interface {
	EnsureGroup(ctx context.Context, chatID int64, title string) error
}

type Clock interface {
	Now() time.Time
}

type AuditRecorder interface {
	Record(ctx context.Context, entry domain.NewAuditEntry) error
}
