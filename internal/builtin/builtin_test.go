package builtin

import (
	"slices"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func msg(text string, modifiers ...func(*domain.ModerationMessage)) domain.ModerationMessage {
	m := domain.ModerationMessage{ChatType: "supergroup", ChatID: -100, MessageID: 1, Text: text}
	for _, modify := range modifiers {
		modify(&m)
	}
	return m
}

func withEntities(entities ...domain.MessageEntityInfo) func(*domain.ModerationMessage) {
	return func(m *domain.ModerationMessage) { m.Entities = append(m.Entities, entities...) }
}

func withForward(forward domain.ForwardInfo) func(*domain.ModerationMessage) {
	return func(m *domain.ModerationMessage) { m.Forward = &forward }
}

func TestRegexDatabaseHits(t *testing.T) {
	c := New(true)
	cases := []struct {
		text string
		want string
	}{
		{"进群 https://t.me/+abc123", HitInviteLinkShort},
		{"加入 https://t.me/joinchat/XYZ123", HitInviteLinkJoinchat},
		{"点我 bit.ly/3abc", HitShortLink},
		{"免费领取 https://t.me/abc", HitAdKeywordWithLink},
	}
	for _, tc := range cases {
		hits := c.Detect(msg(tc.text))
		if !slices.Contains(hits, tc.want) {
			t.Errorf("Detect(%q) = %v, want it to contain %s", tc.text, hits, tc.want)
		}
	}
}

func TestBotMentionRequiresAdSignal(t *testing.T) {
	c := New(true)
	// text_mention of a bot + a link -> kill.
	with := msg("加 @somespambot https://t.me/xyz",
		withEntities(domain.MessageEntityInfo{Type: "text_mention", Username: "somespambot", IsBot: true}))
	if hits := c.Detect(with); !slices.Contains(hits, HitBotMention) {
		t.Fatalf("Detect(bot mention + link) = %v, want %s", hits, HitBotMention)
	}
	// mention username ending in "bot" + ad keyword -> kill.
	byName := msg("@freescorebot 扫码领红包",
		withEntities(domain.MessageEntityInfo{Type: "mention", Username: "freescorebot"}))
	if hits := c.Detect(byName); !slices.Contains(hits, HitBotMention) {
		t.Fatalf("Detect(bot username + keyword) = %v, want %s", hits, HitBotMention)
	}
	// A bot mention with no link and no ad word must stay untouched.
	benign := msg("有问题可以问 @helpbot",
		withEntities(domain.MessageEntityInfo{Type: "mention", Username: "helpbot"}))
	if hits := c.Detect(benign); len(hits) != 0 {
		t.Fatalf("Detect(benign bot mention) = %v, want none", hits)
	}
	// Mentioning a human with a link is not an ad.
	human := msg("看下 @xiaoming 这个链接 https://example.com",
		withEntities(domain.MessageEntityInfo{Type: "mention", Username: "xiaoming"}))
	if hits := c.Detect(human); len(hits) != 0 {
		t.Fatalf("Detect(human mention + link) = %v, want none", hits)
	}
}

func TestChannelForwardRequiresLink(t *testing.T) {
	c := New(true)
	forwarded := msg("something shared",
		withEntities(domain.MessageEntityInfo{Type: "url"}),
		withForward(domain.ForwardInfo{Type: "channel", SourceID: -1001234, SourceTitle: "广告频道"}))
	if hits := c.Detect(forwarded); !slices.Contains(hits, HitChannelForwardWithLink) {
		t.Fatalf("Detect(channel forward + url entity) = %v, want %s", hits, HitChannelForwardWithLink)
	}
	// Channel forward without a link stays untouched.
	noLink := msg("普通转发", withForward(domain.ForwardInfo{Type: "channel", SourceID: -1001234}))
	if hits := c.Detect(noLink); len(hits) != 0 {
		t.Fatalf("Detect(channel forward, no link) = %v, want none", hits)
	}
	// User forward with a link is not treated as channel-forward spam.
	userForward := msg("看看这个 t.me/xxx", withForward(domain.ForwardInfo{Type: "user", SourceID: 123}))
	if hits := c.Detect(userForward); len(hits) != 0 {
		t.Fatalf("Detect(user forward + link) = %v, want none (no ad keywords/invite)", hits)
	}
}

func TestDetectDisablesAndDeduplicates(t *testing.T) {
	off := New(false)
	if hits := off.Detect(msg("https://t.me/+abc")); len(hits) != 0 {
		t.Fatalf("disabled checker returned hits: %v", hits)
	}
	on := New(true)
	// Same invite text matches only the short-invite rule once.
	text := "进群 https://t.me/+abc 加我"
	hits := on.Detect(msg(text))
	if len(hits) != 1 || hits[0] != HitInviteLinkShort {
		t.Fatalf("Detect(%q) = %v, want exactly [%s]", text, hits, HitInviteLinkShort)
	}
}