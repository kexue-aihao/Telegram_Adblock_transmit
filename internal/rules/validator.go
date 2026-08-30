package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// InvalidPatternError indicates that a rule cannot be used by Go's RE2
// regular-expression engine. RE2 deliberately rejects constructs such as
// backreferences and look-around assertions to guarantee linear-time matching.
type InvalidPatternError struct {
	Reason string
}

func (e *InvalidPatternError) Error() string { return e.Reason }

// CompilePattern validates a user supplied expression and compiles it with
// case-insensitive matching enabled. The non-capturing wrapper keeps an
// expression's alternation scoped to the rule while allowing inline RE2
// modifiers inside the expression.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, &InvalidPatternError{Reason: "正则表达式不能为空。"}
	}
	if utf8.RuneCountInString(pattern) > domain.MaxPatternLength {
		return nil, &InvalidPatternError{Reason: fmt.Sprintf("正则表达式不能超过 %d 个字符。", domain.MaxPatternLength)}
	}
	if construct := unsupportedConstruct(pattern); construct != "" {
		return nil, &InvalidPatternError{Reason: fmt.Sprintf("Go RE2 不支持 %s；请改用 RE2 兼容语法。", construct)}
	}

	compiled, err := regexp.Compile("(?i)(?:" + pattern + ")")
	if err != nil {
		return nil, &InvalidPatternError{Reason: fmt.Sprintf("正则表达式无效（Go RE2）：%v", err)}
	}
	return compiled, nil
}

// unsupportedConstruct gives administrators a useful migration hint instead
// of exposing a low-level parser error for common Python/PCRE-only features.
func unsupportedConstruct(pattern string) string {
	for _, token := range []struct {
		fragment string
		name     string
	}{
		{`(?=`, "正向前瞻"},
		{`(?!`, "负向前瞻"},
		{`(?<=`, "正向后瞻"},
		{`(?<!`, "负向后瞻"},
		{`(?(`, "条件表达式"},
		{`\k<`, "命名反向引用"},
		{`(?P=`, "命名反向引用"},
	} {
		if containsUnescaped(pattern, token.fragment) {
			return token.name
		}
	}
	// A backreference is an unescaped backslash followed by a digit. RE2
	// treats it as an invalid escape sequence, so identify it explicitly.
	for i := 0; i+1 < len(pattern); i++ {
		if pattern[i] != '\\' || pattern[i+1] < '1' || pattern[i+1] > '9' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && pattern[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return "反向引用"
		}
	}
	return ""
}

func containsUnescaped(pattern, fragment string) bool {
	for offset := 0; offset < len(pattern); {
		relative := strings.Index(pattern[offset:], fragment)
		if relative < 0 {
			return false
		}
		index := offset + relative
		backslashes := 0
		for j := index - 1; j >= 0 && pattern[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return true
		}
		offset = index + len(fragment)
	}
	return false
}

// ValidatePattern is an alias useful to callers that only need validation.
// It returns the compiled expression so callers can avoid compiling twice.
func ValidatePattern(pattern string) (*regexp.Regexp, error) {
	return CompilePattern(pattern)
}
