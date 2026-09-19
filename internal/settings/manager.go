// Package settings publishes the runtime switches the operator changes from the
// WebUI panel or from the /settings command. Like the built-in filter, it keeps
// an immutable snapshot: detection keeps using the previous snapshot while a
// write is in flight, and a failed write never changes what the bot enforces.
package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// ErrInvalidSettings marks values the panel or the chat command must reject
// instead of storing.
var ErrInvalidSettings = errors.New("invalid bot settings")

type Manager struct {
	settings atomic.Pointer[domain.BotSettings]
	updateMu sync.Mutex
	store    ports.BotSettingsStore
}

// NewManager loads persisted settings. While no row exists the environment
// defaults (BIO_CHECK_ENABLED, BOT_OWNER_IDS) stay in effect, so an upgrade
// keeps behaving exactly as before until an administrator saves something.
func NewManager(ctx context.Context, defaults domain.BotSettings, store ports.BotSettingsStore) (*Manager, error) {
	initial, err := normalize(defaults)
	if err != nil {
		return nil, err
	}
	m := &Manager{store: store}
	if store != nil {
		stored, loadErr := store.GetBotSettings(ctx)
		switch {
		case loadErr == nil:
			if initial, err = normalize(stored); err != nil {
				return nil, err
			}
		case errors.Is(loadErr, ports.ErrBotSettingsNotFound):
		default:
			return nil, fmt.Errorf("load bot settings: %w", loadErr)
		}
	}
	m.settings.Store(&initial)
	return m, nil
}

// NewMemoryManager returns a manager without persistence. Tests and the panel
// preview use it; production wiring always passes a store.
func NewMemoryManager(defaults domain.BotSettings) *Manager {
	initial, err := normalize(defaults)
	if err != nil {
		initial = domain.BotSettings{}
	}
	m := &Manager{}
	m.settings.Store(&initial)
	return m
}

// Settings returns an independent snapshot. A nil manager reports zero values
// so callers without settings configured keep the documented defaults.
func (m *Manager) Settings() domain.BotSettings {
	if m == nil {
		return domain.BotSettings{}
	}
	current := m.settings.Load()
	if current == nil {
		return domain.BotSettings{}
	}
	return current.Clone()
}

// Update persists a partial change and only then publishes it. Callers never
// observe a value that failed to save.
func (m *Manager) Update(ctx context.Context, patch domain.BotSettingsPatch) (domain.BotSettings, error) {
	if m == nil {
		return domain.BotSettings{}, errors.New("bot settings manager is nil")
	}
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	next, err := normalize(m.Settings().Apply(patch))
	if err != nil {
		return domain.BotSettings{}, err
	}
	if m.store == nil {
		return domain.BotSettings{}, errors.New("bot settings store is unavailable")
	}
	if err := m.store.SaveBotSettings(ctx, next); err != nil {
		return domain.BotSettings{}, fmt.Errorf("persist bot settings: %w", err)
	}
	m.settings.Store(&next)
	return next.Clone(), nil
}

// SetBioCheck is the shortcut used by the /settings command.
func (m *Manager) SetBioCheck(ctx context.Context, enabled bool) (domain.BotSettings, error) {
	return m.Update(ctx, domain.BotSettingsPatch{BioCheckEnabled: &enabled})
}

// normalize de-duplicates and validates the owner list so a bad entry cannot be
// stored or published.
func normalize(settings domain.BotSettings) (domain.BotSettings, error) {
	next := settings.Clone()
	owners := make([]int64, 0, len(next.OwnerUserIDs))
	seen := make(map[int64]bool, len(next.OwnerUserIDs))
	for _, id := range next.OwnerUserIDs {
		if id <= 0 {
			return domain.BotSettings{}, fmt.Errorf("%w: owner user ID must be a positive number: %d", ErrInvalidSettings, id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		owners = append(owners, id)
	}
	if len(owners) > domain.MaxBotOwners {
		return domain.BotSettings{}, fmt.Errorf("%w: at most %d bot owners are supported", ErrInvalidSettings, domain.MaxBotOwners)
	}
	slices.Sort(owners)
	next.OwnerUserIDs = owners
	return next, nil
}
