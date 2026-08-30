package rules

import (
	"sort"
	"sync"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// MemoryCache stores precompiled rules per chat. Matching only takes a read
// lock, so the moderation hot path never needs a database round trip.
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

// Replace atomically replaces all rules for a chat. The input is copied so a
// caller cannot mutate the cache while another goroutine is matching.
func (c *MemoryCache) Replace(chatID int64, rules []domain.CompiledRule) {
	compiled := append([]domain.CompiledRule(nil), rules...)
	sort.SliceStable(compiled, func(i, j int) bool { return compiled[i].ID < compiled[j].ID })
	c.mu.Lock()
	if len(compiled) == 0 {
		delete(c.rules, chatID)
	} else {
		c.rules[chatID] = compiled
	}
	c.mu.Unlock()
}

func (c *MemoryCache) Remove(chatID int64) {
	c.mu.Lock()
	delete(c.rules, chatID)
	c.mu.Unlock()
}

// Match returns every enabled rule ID that matches content, in ascending rule
// ID order. Invalid expressions are skipped defensively; persisted rules are
// validated before insertion and should therefore never reach this branch.
func (c *MemoryCache) Match(chatID int64, content string) []int64 {
	c.mu.RLock()
	rules := append([]domain.CompiledRule(nil), c.rules[chatID]...)
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
