package moderation

import (
	"context"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/telegram"
)

// This exercises the whole feature path with the payload shape Telegram
// actually delivers: a polled raw update whose reply_to_message holds the
// advertisement. It fails if any link in the chain (JSON decoding, command
// routing, conversion, rule storage, group reply) stops working.
func TestRuleRegexEndToEndFromRawUpdate(t *testing.T) {
	update := []byte(`{
	  "message": {
	    "message_id": 501,
	    "chat": {"id": -1001234567890, "type": "supergroup", "title": "Ops"},
	    "from": {"id": 42, "is_bot": false, "first_name": "Admin"},
	    "text": "/rule_regex",
	    "reply_to_message": {
	      "message_id": 500,
	      "chat": {"id": -1001234567890, "type": "supergroup", "title": "Ops"},
	      "from": {"id": 7, "is_bot": false, "first_name": "Spam"},
	      "text": "急招拍照兼职，日结三百，加我微信详聊"
	    }
	  }
	}`)
	message, ok, err := telegram.ParseUpdate(update)
	if err != nil || !ok {
		t.Fatalf("ParseUpdate() = %v, %v", ok, err)
	}

	telegramFake := &fakeTelegram{admin: true}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, telegramFake, nil)
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("HandleUpdate() = %v, %v", deleted, err)
	}
	if store.addCalls != 1 {
		t.Fatalf("expected one stored rule, got %+v", store.added)
	}
	stored := store.added[0]
	if stored.ChatID != -1001234567890 {
		t.Fatalf("rule stored for the wrong chat: %+v", stored)
	}
	compiled, err := rules.ValidatePattern(stored.Pattern)
	if err != nil {
		t.Fatalf("stored pattern does not compile: %v", err)
	}
	if !compiled.MatchString("急招拍照兼职，日结五百，加我微信详聊") {
		t.Fatalf("stored rule %q misses a repost with another amount", stored.Pattern)
	}
	if len(telegramFake.sends) != 1 || !strings.Contains(telegramFake.sends[0].text, stored.Pattern) {
		t.Fatalf("conversion result was not posted to the group: %+v", telegramFake.sends)
	}
}
