package rules

import (
	"sync"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func mustCompiled(t *testing.T, id int64, pattern string) domain.CompiledRule {
	t.Helper()
	matcher, err := CompilePattern(pattern)
	if err != nil {
		t.Fatalf("compile test rule: %v", err)
	}
	return domain.CompiledRule{Rule: domain.Rule{ID: id, Pattern: pattern, Enabled: true}, PatternMatcher: matcher}
}

func TestMemoryCacheMatchesAllRulesPerChat(t *testing.T) {
	cache := NewMemoryCache()
	cache.Replace(1, []domain.CompiledRule{
		mustCompiled(t, 20, "sale"),
		mustCompiled(t, 10, "free"),
	})

	got := cache.Match(1, "FREE sale")
	if len(got) != 2 || got[0] != 10 || got[1] != 20 {
		t.Fatalf("Match() = %v, want [10 20]", got)
	}
	if got := cache.Match(2, "FREE sale"); len(got) != 0 {
		t.Fatalf("rules leaked across chats: %v", got)
	}
}

func TestMemoryCacheReplaceAndRemove(t *testing.T) {
	cache := NewMemoryCache()
	cache.Replace(1, []domain.CompiledRule{mustCompiled(t, 1, "old")})
	cache.Replace(1, []domain.CompiledRule{mustCompiled(t, 2, "new")})
	if got := cache.Match(1, "old"); len(got) != 0 {
		t.Fatalf("stale rule remained after Replace: %v", got)
	}
	if got := cache.Match(1, "new"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("replacement not active: %v", got)
	}
	cache.Remove(1)
	if got := cache.Match(1, "new"); len(got) != 0 {
		t.Fatalf("rules remained after Remove: %v", got)
	}
}

func TestCompileRulesSkipsDisabledRules(t *testing.T) {
	compiled, err := CompileRules([]domain.Rule{
		{ID: 1, Pattern: "enabled", Enabled: true},
		{ID: 2, Pattern: "disabled", Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewMemoryCache()
	cache.Replace(1, compiled)
	if got := cache.Match(1, "enabled disabled"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("Match() = %v, want only enabled rule [1]", got)
	}
}

func TestMemoryCacheConcurrentAccess(t *testing.T) {
	cache := NewMemoryCache()
	compiled := mustCompiled(t, 1, "spam")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cache.Replace(int64(i%4), []domain.CompiledRule{compiled})
				_ = cache.Match(int64(i%4), "SPAM")
			}
		}(i)
	}
	wg.Wait()
}
