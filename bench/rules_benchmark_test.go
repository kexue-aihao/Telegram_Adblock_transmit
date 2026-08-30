package bench

import (
	"strconv"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
)

// BenchmarkCacheMatch measures the hot path used for every group message.
// Rules are compiled once and matching performs no database work.
func BenchmarkCacheMatch(b *testing.B) {
	cache := rules.NewMemoryCache()
	compiled, err := rules.CompileRules(benchmarkRules(64))
	if err != nil {
		b.Fatal(err)
	}
	cache.Replace(-100, compiled)
	content := "normal discussion with no promotional content"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Match(-100, content)
	}
}

// BenchmarkCacheMatchParallel provides a repeatable concurrent read baseline
// for the RWMutex-protected cache used by the polling loop.
func BenchmarkCacheMatchParallel(b *testing.B) {
	cache := rules.NewMemoryCache()
	compiled, err := rules.CompileRules(benchmarkRules(64))
	if err != nil {
		b.Fatal(err)
	}
	cache.Replace(-100, compiled)
	content := "normal discussion with no promotional content"

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cache.Match(-100, content)
		}
	})
}

// BenchmarkCompileRules is intentionally separate from matching: compilation
// happens on startup and after rule-management commands, never per message.
func BenchmarkCompileRules(b *testing.B) {
	input := benchmarkRules(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rules.CompileRules(input); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRules(count int) []domain.Rule {
	result := make([]domain.Rule, count)
	for i := range result {
		result[i] = domain.Rule{
			ID:      int64(i + 1),
			ChatID:  -100,
			Pattern: "(?i)advertisement-" + strconv.Itoa(i),
			Enabled: true,
		}
	}
	return result
}
