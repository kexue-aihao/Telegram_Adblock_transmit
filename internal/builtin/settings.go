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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Pattern     string `json:"pattern,omitempty"`
	Enabled     bool   `json:"enabled"`
	Effective   bool   `json:"effective"`
}

type Status struct {
	Enabled bool       `json:"enabled"`
	Rules   []RuleInfo `json:"rules"`
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
	settings := c.settings.Load()
	if settings == nil {
		return domain.BuiltinSettings{}
	}
	return domain.BuiltinSettings{Enabled: settings.Enabled, DisabledRules: slices.Clone(settings.DisabledRules)}
}

func catalog() []RuleInfo {
	items := []RuleInfo{
		{ID: HitInviteLinkShort, Name: "群组邀请链接", Description: "识别 t.me/+ 开头的群组邀请链接。"},
		{ID: HitInviteLinkJoinchat, Name: "Joinchat 邀请链接", Description: "识别 t.me/joinchat/ 开头的群组邀请链接。"},
		{ID: HitShortLink, Name: "短链接", Description: "识别 bit.ly、tinyurl.com、t.co 等内置短链接域名。"},
		{ID: HitAdKeywordWithLink, Name: "广告关键词与链接", Description: "广告关键词后 40 个字符内出现 t.me/ 或 HTTP 链接时命中。"},
		{ID: HitBotMention, Name: "机器人提及广告", Description: "消息包含机器人提及，并同时包含广告关键词或链接时命中。根据 Telegram 消息实体识别机器人。"},
		{ID: HitChannelForwardWithLink, Name: "频道转发链接", Description: "消息转发来源为频道，并包含文本链接或 Telegram 链接实体时命中。"},
	}
	for i := range items {
		for _, rule := range database {
			if rule.id == items[i].ID {
				items[i].Pattern = rule.pattern.String()
			}
		}
	}
	return items
}

func status(settings domain.BuiltinSettings) Status {
	result := Status{Enabled: settings.Enabled, Rules: catalog()}
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
	known := catalog()
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
