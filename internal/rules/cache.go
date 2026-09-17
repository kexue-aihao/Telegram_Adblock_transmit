package rules

import (
	"sort"
	"sync"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// globalChatKey is the single cache slot: rules are shared across all groups,
// so every chat maps to the same compiled set.
const globalChatKey int64 = -1

// MemoryCache stores the precompiled, globally shared rule set. Matching only
// takes a read lock, so the moderation hot path never needs a database round
// trip. The chat_id parameters of the interface are ignored: a rule added in
// one group applies to every group.
type MemoryCache struct {
	mu    sync.RWMutex
	rules map[int64][]domain.CompiledRule
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{rules: make(map[int64][]domain.CompiledRule)}
}

// NewCache is kept as a concise constructor for callers that do not need to
// depend on the concrete cache name.
func NewCache() *MemoryCache { return NewMemoryCache() }

// NewRuleCache is an explicit alias for dependency-injection code.
func NewRuleCache() *MemoryCache { return NewMemoryCache() }

// Replace atomically replaces the global rule set. The input is copied so a
// caller cannot mutate the cache while another goroutine is matching.
func (c *MemoryCache) Replace(chatID int64, rules []domain.CompiledRule) {
	_ = chatID // rules are global
	compiled := append([]domain.CompiledRule(nil), rules...)
	sort.SliceStable(compiled, func(i, j int) bool { return compiled[i].ID < compiled[j].ID })
	c.mu.Lock()
	if len(compiled) == 0 {
		delete(c.rules, globalChatKey)
	} else {
		c.rules[globalChatKey] = compiled
	}
	c.mu.Unlock()
}

func (c *MemoryCache) Remove(chatID int64) {
	_ = chatID // rules are global
	c.mu.Lock()
	delete(c.rules, globalChatKey)
	c.mu.Unlock()
}

// Match returns every enabled rule ID that matches content, in ascending rule
// ID order; chatID is ignored because rules apply to all groups. Invalid
// expressions are skipped defensively; persisted rules are validated before
// insertion and should therefore never reach this branch.
func (c *MemoryCache) Match(chatID int64, content string) []int64 {
	_ = chatID // rules are global
	c.mu.RLock()
	rules := append([]domain.CompiledRule(nil), c.rules[globalChatKey]...)
	c.mu.RUnlock()

	matched := make([]int64, 0)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.PatternMatcher == nil {
			continue
		}
		if rule.PatternMatcher.MatchString(content) {
			matched = append(matched, rule.ID)
		}
	}
	return matched
}

// CompileRules converts domain rules into cache entries. Invalid persisted
// rules are omitted and returned as an error so startup can report corruption
// without taking down the bot.
func CompileRules(input []domain.Rule) ([]domain.CompiledRule, error) {
	result := make([]domain.CompiledRule, 0, len(input))
	for _, rule := range input {
		if !rule.Enabled {
			continue
		}
		matcher, err := CompilePattern(rule.Pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.CompiledRule{Rule: rule, PatternMatcher: matcher})
	}
	return result, nil
}
