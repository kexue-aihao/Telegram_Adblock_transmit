package builtin

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

var ErrUnknownRule = errors.New("unknown builtin rule")

type RuleInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Conditions  []string `json:"conditions"`
	Pattern     string   `json:"pattern,omitempty"`
	Enabled     bool     `json:"enabled"`
	Effective   bool     `json:"effective"`
}

type Status struct {
	Enabled        bool       `json:"enabled"`
	LibraryVersion string     `json:"library_version"`
	Rules          []RuleInfo `json:"rules"`
}

// NewManaged restores overrides even when the WebUI itself is disabled.
func NewManaged(ctx context.Context, enabled bool, store ports.BuiltinSettingsStore) (*Checker, error) {
	if store == nil {
		return nil, errors.New("builtin settings store is required")
	}
	c := New(enabled)
	c.store = store
	settings, err := store.GetBuiltinSettings(ctx)
	if errors.Is(err, ports.ErrBuiltinSettingsNotFound) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load builtin settings: %w", err)
	}
	settings.DisabledRules = slices.Clone(settings.DisabledRules)
	c.settings.Store(&settings)
	return c, nil
}

func (c *Checker) Settings() domain.BuiltinSettings {
	if c == nil {
		return domain.BuiltinSettings{}
	}
	settings := c.settings.Load()
	if settings == nil {
		return domain.BuiltinSettings{}
	}
	return domain.BuiltinSettings{Enabled: settings.Enabled, DisabledRules: slices.Clone(settings.DisabledRules)}
}

func status(settings domain.BuiltinSettings) Status {
	result := Status{Enabled: settings.Enabled, LibraryVersion: LibraryVersion, Rules: Catalog()}
	for i := range result.Rules {
		result.Rules[i].Enabled = !slices.Contains(settings.DisabledRules, result.Rules[i].ID)
		result.Rules[i].Effective = settings.Enabled && result.Rules[i].Enabled
	}
	return result
}

func (c *Checker) Status() Status { return status(c.Settings()) }

// Update serializes partial writes and publishes a snapshot only after persistence
// succeeds. Detection keeps using the previous snapshot while the database writes.
func (c *Checker) Update(ctx context.Context, enabled *bool, rules map[string]bool) (Status, error) {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()
	next := c.Settings()
	known := Catalog()
	for id := range rules {
		if !slices.ContainsFunc(known, func(rule RuleInfo) bool { return rule.ID == id }) {
			return Status{}, fmt.Errorf("%w: %s", ErrUnknownRule, id)
		}
	}
	if enabled != nil {
		next.Enabled = *enabled
	}
	for id, on := range rules {
		next.DisabledRules = slices.DeleteFunc(next.DisabledRules, func(disabled string) bool { return disabled == id })
		if !on {
			next.DisabledRules = append(next.DisabledRules, id)
		}
	}
	slices.Sort(next.DisabledRules)
	if c.store == nil {
		return Status{}, errors.New("builtin settings store is unavailable")
	}
	if err := c.store.SaveBuiltinSettings(ctx, next); err != nil {
		return Status{}, fmt.Errorf("persist builtin settings: %w", err)
	}
	c.settings.Store(&next)
	return status(next), nil
}
