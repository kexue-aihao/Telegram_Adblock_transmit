package rules

import (
	"strings"
	"testing"
)

func TestCompilePatternIsCaseInsensitive(t *testing.T) {
	re, err := CompilePattern("free.*claim")
	if err != nil {
		t.Fatalf("CompilePattern() error = %v", err)
	}
	if !re.MatchString("FREE CLAIM") {
		t.Fatal("compiled rule should match regardless of case")
	}
}

func TestCompilePatternRejectsEmptyAndOverlongPatterns(t *testing.T) {
	for _, pattern := range []string{"", strings.Repeat("a", 513)} {
		if _, err := CompilePattern(pattern); err == nil {
			t.Errorf("CompilePattern(%q) expected an error", pattern)
		}
	}
}

func TestCompilePatternRejectsPythonBackreferenceAndLookaround(t *testing.T) {
	for _, pattern := range []string{`(foo)\1`, `(?=https?://)spam`, `(?<=foo)bar`} {
		if _, err := CompilePattern(pattern); err == nil {
			t.Errorf("CompilePattern(%q) expected RE2 compatibility error", pattern)
		}
	}
}

func TestCompilePatternPreservesAlternationScope(t *testing.T) {
	re, err := CompilePattern(`foo|bar`)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("BAR") || !re.MatchString("foo") {
		t.Fatal("alternation should match both alternatives")
	}
}

func TestCompilePatternAllowsEscapedUnsupportedTokens(t *testing.T) {
	if _, err := CompilePattern(`\(\?=`); err != nil {
		t.Fatalf("escaped lookahead token should be matchable as literal text: %v", err)
	}
}
