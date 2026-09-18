package ports

import (
	"context"
	"errors"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

var ErrBuiltinSettingsNotFound = errors.New("builtin settings not found")

type BuiltinSettingsStore interface {
	GetBuiltinSettings(context.Context) (domain.BuiltinSettings, error)
	SaveBuiltinSettings(context.Context, domain.BuiltinSettings) error
}
