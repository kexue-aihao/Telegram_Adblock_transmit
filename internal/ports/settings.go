package ports

import (
	"context"
	"errors"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

var ErrBotSettingsNotFound = errors.New("bot settings not found")

// BotSettingsStore persists the runtime switches edited from the panel or from
// chat. A missing row means the environment defaults still apply.
type BotSettingsStore interface {
	GetBotSettings(context.Context) (domain.BotSettings, error)
	SaveBotSettings(context.Context, domain.BotSettings) error
}
