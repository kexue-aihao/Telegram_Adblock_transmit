// Package builtin implements the shipped-in advertising "virus library":
// curated patterns and message-metadata heuristics that delete ad messages in
// every group without administrator configuration. Detection is deliberately
// balanced (see the hit definitions) to keep false positives low while still
// striking forwarded ads and @-mentioned external bots on sight.
package builtin

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// Hit identifiers are stable and shown verbatim in the audit log (prefixed
// by the panel with an "⚡内置" badge) so operators can see which built-in
// rule killed a message.
const (
	HitInviteLinkShort        = "ad_invite_link"
	HitInviteLinkJoinchat     = "ad_invite_joinchat"
	HitShortLink              = "ad_shortlink"
	HitAdKeywordWithLink      = "ad_keyword_link"
	HitBotMention             = "ad_bot_mention"
	HitChannelForwardWithLink = "ad_channel_forward_link"
)

type regexRule struct {
	id      string
	pattern *regexp.Regexp
}

func rule(id, pattern string) regexRule {
	return regexRule{id: id, pattern: regexp.MustCompile(pattern)}
}

// database is the built-in ad-killer virus library. Patterns are compiled
// once at startup; Go RE2 guarantees linear-time matching. Extend this list
// to harden the kill coverage without changing the rest of the pipeline.
var database = []regexRule{
	rule(HitInviteLinkShort, `(?i)t\.me/\+[a-z0-9_-]{2,64}`),
	rule(HitInviteLinkJoinchat, `(?i)t\.me/joinchat/[a-z0-9_-]+`),
	rule(HitShortLink, `(?i)(?:bit\.ly|goo\.gl|tinyurl\.com|rb\.gy|0rz\.tw|t\.co|is\.gd|ow\.ly|shorturl\.at)/[a-z0-9]+`),
	rule(HitAdKeywordWithLink, `(?i)(领取|返利|红包|秒到|免费|加群|扫码|客服|优惠券|福利|内部|名额|赚钱|日赚|红包群|兼职|彩票|博彩).{0,40}(t\.me/|https?://)`),
}

// adKeyword is a loose spam-vocabulary signal used to gate the metadata
// heuristics (bot mention, forwarded ads) so benign mentions of a bot or an
// ordinary forward stay untouched.
var adKeyword = regexp.MustCompile(`(?i)(领取|返利|红包|秒到|免费|加群|扫码|客服|优惠券|福利|内部|名额|赚钱|日赚|兼职|彩票|博彩|外快|导师)`)

var (
	tmeLink  = regexp.MustCompile(`(?i)t\.me/`)
	httpLink = regexp.MustCompile(`(?i)https?://`)
)

// Checker applies the built-in filter. It is safe for concurrent use.
type Checker struct {
	settings atomic.Pointer[domain.BuiltinSettings]
	updateMu sync.Mutex
	store    ports.BuiltinSettingsStore
}

// New creates a checker. The master ADFILTER_ENABLED switch lives here; a
// disabled checker makes Detect a no-op.
func New(enabled bool) *Checker {
	c := &Checker{}
	c.settings.Store(&domain.BuiltinSettings{Enabled: enabled})
	return c
}

// Enabled reports whether the built-in filter is active.
func (c *Checker) Enabled() bool { return c != nil && c.Settings().Enabled }

// Detect returns the stable, de-duplicated hit ids this message matches, or
// nil when the filter is disabled or nothing matched.
func (c *Checker) Detect(msg domain.ModerationMessage) []string {
	if c == nil {
		return nil
	}
	settings := c.Settings()
	if !settings.Enabled {
		return nil
	}
	content := msg.Content()
	var hits []string
	seen := make(map[string]bool)
	add := func(id string) {
		if !seen[id] && !slices.Contains(settings.DisabledRules, id) {
			seen[id] = true
			hits = append(hits, id)
		}
	}
	for _, r := range database {
		if r.pattern.MatchString(content) {
			add(r.id)
		}
	}
	if mentionsBot(msg.Entities) && (hasLink(content, msg.Entities) || adKeyword.MatchString(content)) {
		add(HitBotMention)
	}
	if isChannelForward(msg.Forward) && hasLink(content, msg.Entities) {
		add(HitChannelForwardWithLink)
	}
	return hits
}

// mentionsBot reports whether the message @-mentions an external bot: a
// text_mention entity with the bot flag, or a @username that ends in "bot"
// (the heuristic Telegram bots overwhelmingly use).
func mentionsBot(entities []domain.MessageEntityInfo) bool {
	for _, e := range entities {
		if e.Type == "text_mention" {
			if e.IsBot {
				return true
			}
			continue
		}
		if e.Type == "mention" && strings.HasSuffix(strings.ToLower(e.Username), "bot") {
			return true
		}
	}
	return false
}

// isChannelForward reports whether the message was forwarded from a channel
// (the most common source of forwarded spam).
func isChannelForward(forward *domain.ForwardInfo) bool {
	return forward != nil && forward.Type == "channel"
}

// hasLink reports whether the message carries any link: an explicit t.me or
// http(s) URL in the text, or a url / text_link entity.
func hasLink(content string, entities []domain.MessageEntityInfo) bool {
	if tmeLink.MatchString(content) || httpLink.MatchString(content) {
		return true
	}
	for _, e := range entities {
		if e.Type == "url" || e.Type == "text_link" {
			return true
		}
	}
	return false
}
