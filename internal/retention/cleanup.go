package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

func Run(ctx context.Context, audit ports.AuditStore, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	cleanup := func() {
		before := time.Now().Add(-time.Duration(domain.AuditRetentionDays) * 24 * time.Hour)
		count, err := audit.DeleteExpired(ctx, before)
		if err != nil {
			logger.Error("audit retention cleanup failed", "error", err)
			return
		}
		if count > 0 {
			logger.Info("expired audit records removed", "count", count)
		}
	}
	cleanup()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
