package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// DeriveError explains why an observed message cannot become a rule.
type DeriveError struct{ Reason string }

func (e *DeriveError) Error() string { return e.Reason }

const (
	// deriveMinGap is the slack allowed between two phrases so a repost that
	// adds emoji, spacing or punctuation still matches.
	deriveMinGap = 4
	// deriveMaxGap bounds that slack when the observed text already had a long
	// separator run, such as a decorated divider line.
	deriveMaxGap = 12
	// deriveMinLiteral is the shortest literal content a derived rule may keep.
	// A pattern made only of links, digits, emoji or two characters would match
	// far too much to be useful.
	deriveMinLiteral = 4
)

// deriveLinkPattern replaces an observed link. Spammers rotate domains between
// reposts, so the link itself is never kept verbatim; the surrounding literal
// phrases carry the rule's specificity.
var deriveLinkPattern = `(?:https?://|tg://|www\.|t\.me/|telegram\.me/)\S*|[a-z0-9][a-z0-9.-]*\.[a-z]{2,63}(?:/\S*)?`

// deriveURLPattern recognizes a link at the current scan position.
var deriveURLPattern = regexp.MustCompile(`(?i)^(?:https?://|tg://|www\.|t\.me/|telegram\.me/|telegram\.dog/|[a-z0-9][a-z0-9.-]*\.[a-z]{2,63}(?:/|$))[^\s\p{Z}]*`)

// hanNumeralRunes are the CJK numerals used for amounts (三百, 一万). A run of
// two or more is generalized so a changed amount still matches. Single
// characters stay literal because they appear inside ordinary words.
const hanNumeralRunes = "〇零一二三四五六七八九十百千万亿两"

// hanNumeralPattern replaces a CJK amount.
const hanNumeralPattern = `[〇零一二三四五六七八九十百千万亿两]+`

// hanNumeralWords are ordinary words written entirely with numeral
// characters; generalizing them would change what the rule means.
var hanNumeralWords = map[string]bool{"千万": true, "万一": true}

// DerivePattern converts an observed advertising message into a Go RE2 pattern
// for the per-group rule store.
//
// The conversion keeps the message's literal phrases in order, replaces digits
// with \p{Nd}+, replaces links with a generic link matcher and allows a small
// amount of inserted decoration between phrases. Mentions, amounts and links
// are the parts of an advertisement that change between reposts; the wording is
// what stays. The returned pattern always matches the text it was derived from,
// or DerivePattern reports an error instead of storing a rule that misses its
// own sample. truncated reports that the tail was dropped to fit the pattern
// length limit.
func DerivePattern(text string) (pattern string, truncated bool, err error) {
	parts := deriveParts(text)
	if len(parts) == 0 {
		return "", false, &DeriveError{Reason: "被回复的消息没有可转换的文字内容。"}
	}
	literals := 0
	for _, part := range parts {
		literals += part.literal
	}
	if literals < deriveMinLiteral {
		return "", false, &DeriveError{Reason: fmt.Sprintf("被回复的消息可匹配文字不足 %d 个字符（可能只有链接、数字或表情），请用 /rule_add 手动编写规则。", deriveMinLiteral)}
	}

	var builder strings.Builder
	remaining := domain.MaxPatternLength
	kept, keptLiteral := 0, 0
	for _, part := range parts {
		gap := fmt.Sprintf(`[\s\S]{0,%d}`, deriveGap(part.gap))
		gapRunes := 0
		if kept > 0 {
			gapRunes = utf8.RuneCountInString(gap)
		}
		partRunes := utf8.RuneCountInString(part.pattern)
		if gapRunes+partRunes <= remaining {
			if kept > 0 {
				builder.WriteString(gap)
			}
			builder.WriteString(part.pattern)
			remaining -= gapRunes + partRunes
			kept++
			keptLiteral += part.literal
			continue
		}
		// The tail does not fit. A literal phrase is cut at a rune boundary so
		// the rule still covers the beginning of the advertisement.
		if room := remaining - gapRunes; room > 2 && part.text != "" {
			trimmed := trimLiteral(part.text, room)
			if trimmed != "" {
				if kept > 0 {
					builder.WriteString(gap)
				}
				builder.WriteString(escapeLiteral(trimmed))
				kept++
				keptLiteral += utf8.RuneCountInString(trimmed)
			}
		}
		truncated = true
		break
	}
	if kept == 0 {
		return "", false, &DeriveError{Reason: "这条消息的开头没有可用于匹配的文字，请用 /rule_add 手动编写规则。"}
	}
	if keptLiteral < deriveMinLiteral {
		return "", false, &DeriveError{Reason: fmt.Sprintf("这条消息的可匹配文字不足 %d 个字符，请用 /rule_add 手动编写规则。", deriveMinLiteral)}
	}
	pattern = builder.String()

	compiled, err := ValidatePattern(pattern)
	if err != nil {
		return "", false, err
	}
	if !compiled.MatchString(text) {
		return "", false, &DeriveError{Reason: "生成的规则无法匹配原消息，请用 /rule_add 手动编写规则。"}
	}
	return pattern, truncated, nil
}

// derivePart is one literal or generalized piece of the observed message.
// literal counts the characters that carry literal meaning, gap counts the
// separator runes observed before it, and text keeps the original phrase so a
// part can be trimmed at a rune boundary when a rule reaches its length limit.
type derivePart struct {
	pattern string
	text    string
	literal int
	gap     int
}

func deriveGap(observed int) int {
	if observed <= deriveMinGap {
		return deriveMinGap
	}
	if observed > deriveMaxGap {
		return deriveMaxGap
	}
	return observed
}

func deriveParts(text string) []derivePart {
	runes := []rune(text)
	var (
		parts      []derivePart
		literal    []rune
		gap        int
		tokenStart = true
	)
	emit := func(pattern string, literalCount int) {
		parts = append(parts, derivePart{pattern: pattern, literal: literalCount, gap: gap})
		gap = 0
	}
	flush := func() {
		if len(literal) == 0 {
			return
		}
		phrase := string(literal)
		parts = append(parts, derivePart{pattern: escapeLiteral(phrase), text: phrase, literal: len(literal), gap: gap})
		gap = 0
		literal = literal[:0]
	}
	for i := 0; i < len(runes); {
		r := runes[i]
		// A link is only recognized where a token can start, which keeps the
		// scan linear on long messages.
		if tokenStart {
			if match := deriveURLPattern.FindString(string(runes[i:])); match != "" {
				flush()
				emit("(?:"+deriveLinkPattern+")", 0)
				i += utf8.RuneCountInString(match)
				tokenStart = false
				continue
			}
		}
		tokenStart = false
		switch {
		case r == '@':
			end := i + 1
			for end < len(runes) && isUsernameRune(runes[end]) {
				end++
			}
			if end == i+1 {
				flush()
				gap++
				i++
				continue
			}
			flush()
			// A handle is kept verbatim: generalizing it would turn
			// "联系 @someone" into a rule that deletes every contact post.
			emit(escapeLiteral(string(runes[i:end])), end-i)
			i = end
		case isDeriveSeparator(r):
			flush()
			gap++
			tokenStart = true
			i++
		case unicode.IsDigit(r):
			flush()
			for i < len(runes) && unicode.IsDigit(runes[i]) {
				i++
			}
			emit(`\p{Nd}+`, 0)
		case strings.ContainsRune(hanNumeralRunes, r):
			end := i
			for end < len(runes) && strings.ContainsRune(hanNumeralRunes, runes[end]) {
				end++
			}
			run := string(runes[i:end])
			if utf8.RuneCountInString(run) >= 2 && !hanNumeralWords[run] {
				flush()
				emit(hanNumeralPattern, 0)
			} else {
				literal = append(literal, runes[i:end]...)
			}
			i = end
		default:
			literal = append(literal, r)
			i++
		}
	}
	flush()
	return parts
}

// isDeriveSeparator treats spacing, punctuation, symbols and invisible format
// characters as decoration. Zero-width characters are included so a rule still
// matches a repost with or without them.
func isDeriveSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Z, r) || unicode.Is(unicode.P, r) ||
		unicode.Is(unicode.S, r) || unicode.Is(unicode.Cf, r)
}

func isUsernameRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// escapeLiteral quotes every RE2 metacharacter so observed text matches itself.
func escapeLiteral(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range text {
		if strings.ContainsRune(deriveMetacharacters, r) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// trimLiteral cuts a phrase so that its escaped form fits in room runes. The
// cut happens between source characters, never inside an escape pair.
func trimLiteral(text string, room int) string {
	var builder strings.Builder
	used := 0
	for _, r := range text {
		cost := 1
		if strings.ContainsRune(deriveMetacharacters, r) {
			cost = 2
		}
		if used+cost > room {
			break
		}
		builder.WriteRune(r)
		used += cost
	}
	return builder.String()
}

const deriveMetacharacters = `\.+*?()|[]{}^$`
