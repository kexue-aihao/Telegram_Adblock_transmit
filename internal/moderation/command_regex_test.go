package moderation

import (
	"context"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
)

func replyTo(text string) domain.ModerationMessage {
	message := testMessage()
	quotedUser := int64(7)
	message.Reply = &domain.ReplyInfo{MessageID: 5, Text: text, UserID: &quotedUser}
	return message
}

func TestRuleRegexDerivesRuleFromReply(t *testing.T) {
	for _, command := range []string{"/rule_regex", "/rule_regax", "/RULE_REGEX@MyBot"} {
		t.Run(command, func(t *testing.T) {
			tg := &fakeTelegram{admin: true}
			store := &fakeRules{}
			svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)
			svc.SetBotUsername("MyBot")

			message := replyTo("专业广告代发，广告投放支付，VCC虚拟卡稳定便捷")
			message.Text = command
			if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
				t.Fatalf("%s failed: %v, %v", command, deleted, err)
			}
			if store.addCalls != 1 || len(store.added) != 1 {
				t.Fatalf("expected one stored rule, got %+v", store.added)
			}
			stored := store.added[0]
			if stored.ChatID != message.ChatID || stored.CreatedBy != 42 {
				t.Fatalf("rule saved with wrong owner: %+v", stored)
			}
			for _, phrase := range []string{"专业广告代发", "广告投放支付", "VCC虚拟卡稳定便捷"} {
				if !strings.Contains(stored.Pattern, phrase) {
					t.Fatalf("pattern %q lost %q", stored.Pattern, phrase)
				}
			}
			if len(tg.sends) != 1 {
				t.Fatalf("expected one group reply, got %+v", tg.sends)
			}
			reply := tg.sends[0].text
			if !strings.Contains(reply, "规则 #1") || !strings.Contains(reply, stored.Pattern) {
				t.Fatalf("reply does not report the conversion: %q", reply)
			}
			if strings.Contains(reply, "专业广告代发，广告投放支付，VCC虚拟卡稳定便捷") {
				t.Fatal("reply echoed the quoted advertisement back into the group")
			}
		})
	}
}

func TestRuleRegexUsesRepliedCaption(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)

	message := testMessage()
	message.Text = "/rule_regex"
	message.Reply = &domain.ReplyInfo{MessageID: 5, Caption: "全网招代理，日入过万，名额有限"}
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if store.addCalls != 1 || !strings.Contains(store.added[0].Pattern, "全网招代理") {
		t.Fatalf("caption reply was not converted: %+v", store.added)
	}
}

func TestRuleRegexRequiresReply(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)

	for _, text := range []string{"/rule_regex", "/rule_regex 广告代发"} {
		message := testMessage()
		message.Text = text
		if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	if store.addCalls != 0 {
		t.Fatalf("stored rules without a reply: %+v", store.added)
	}
	if len(tg.sends) != 2 {
		t.Fatalf("expected a usage reply for each attempt, got %+v", tg.sends)
	}
	for _, send := range tg.sends {
		if !strings.Contains(send.text, "回复一条广告消息") {
			t.Fatalf("usage reply is unclear: %q", send.text)
		}
	}
}

func TestRuleRegexRejectsUnusableReply(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)

	message := replyTo("https://t.me/+only_link")
	message.Text = "/rule_regex"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if store.addCalls != 0 {
		t.Fatalf("stored a rule from unusable text: %+v", store.added)
	}
	if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, "无法转换为规则") {
		t.Fatalf("expected a conversion failure reply, got %+v", tg.sends)
	}
}

func TestRuleRegexRequiresGroupAdmin(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	store := &fakeRules{}
	audit := &fakeAudit{}
	svc := NewService(store, &fakeCache{}, audit, tg, nil)

	message := replyTo("专业广告代发，广告投放支付")
	message.Text = "/rule_regex"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if store.addCalls != 0 {
		t.Fatalf("non-admin created a rule: %+v", store.added)
	}
	if len(tg.sends) == 0 || !strings.HasPrefix(tg.sends[0].text, PermissionNotice) {
		t.Fatalf("expected the permission notice, got %+v", tg.sends)
	}
}

func TestRuleRegexDerivedRuleMatchesTheQuotedMessage(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)

	quoted := "苹果18只要６k，买手机可以找我，特价水货机"
	message := replyTo(quoted)
	message.Text = "/rule_regex"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if store.addCalls != 1 {
		t.Fatalf("expected a stored rule, got %+v", store.added)
	}
	compiled, err := rules.ValidatePattern(store.added[0].Pattern)
	if err != nil {
		t.Fatalf("stored rule does not compile: %v", err)
	}
	if !compiled.MatchString(quoted) {
		t.Fatalf("stored rule %q does not match its own source", store.added[0].Pattern)
	}
}
