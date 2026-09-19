package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/telegram"
)

func TestCommandsCannotBypassBuiltinAdvertisingChecks(t *testing.T) {
	cases := []struct {
		name, prefix  string
		bot, admin    bool
		directCommand bool
	}{
		{name: "public_start", prefix: "/start"},
		{name: "public_help", prefix: "/help@MyBot"},
		{name: "admin_public_command", prefix: "/help", admin: true},
		{name: "other_bot", prefix: "/rule_add@OtherBot", admin: true},
		{name: "unknown_other_bot_command", prefix: "/anything@OtherBot"},
		{name: "unauthorized_management", prefix: "/rule_add@MyBot"},
		{name: "bot_management", prefix: "/rule_add@MyBot", bot: true, admin: true},
		{name: "bot_public", prefix: "/help", bot: true},
		{name: "direct_bot_command", prefix: "/rule_add", bot: true, admin: true, directCommand: true},
		{name: "direct_other_bot_command", prefix: "/rule_add@OtherBot", admin: true, directCommand: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := &fakeTelegram{admin: tc.admin}
			audit := &fakeAudit{}
			cache := &fakeCache{}
			ruleStore := &fakeRules{}
			svc := NewService(ruleStore, cache, audit, tg, nil)
			svc.SetBotUsername("MyBot")
			svc.SetBuiltinFilter(builtin.New(true))
			message := testMessage()
			message.UserIsBot = tc.bot
			message.Text = tc.prefix + " 承接洗资业务，联系 @example_agent"
			handle := svc.HandleUpdate
			if tc.directCommand {
				handle = svc.HandleCommand
			}
			deleted, err := handle(context.Background(), message)
			if err != nil || !deleted {
				t.Fatalf("advertising command escaped moderation: %v, %v", deleted, err)
			}
			if len(audit.entries) != 1 || !slices.Contains(audit.entries[0].BuiltinHits, "ad_money_laundering") {
				t.Fatalf("wrong audit attribution: %+v", audit.entries)
			}
			if audit.entries[0].Content != message.Text || audit.entries[0].BuiltinDetails == nil {
				t.Fatalf("original content or details missing: %+v", audit.entries[0])
			}
			if ruleStore.addCalls != 0 || len(cache.queries) != 0 {
				t.Fatalf("command executed: adds=%d queries=%v", ruleStore.addCalls, cache.queries)
			}
			for _, send := range tg.sends {
				if send.text == HelpText() {
					t.Fatal("advertising command produced a help response")
				}
			}
		})
	}
}

func TestFailedAdvertisingDeletionDoesNotRespondToPublicCommand(t *testing.T) {
	tg := &fakeTelegram{deleteErr: errors.New("missing delete permission")}
	audit := &fakeAudit{}
	svc := NewService(&fakeRules{}, &fakeCache{}, audit, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))
	message := testMessage()
	message.Text = "/start 承接洗资业务，联系 @example_agent"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("failed deletion = %v, %v", deleted, err)
	}
	if len(audit.entries) != 1 || audit.entries[0].DeleteSucceeded || len(tg.sends) != 0 {
		t.Fatalf("failed advertising command was not audited correctly: entries=%+v sends=%+v", audit.entries, tg.sends)
	}
}

func TestBenignBotCommandsDoNotExecuteManagementOrReply(t *testing.T) {
	for _, command := range []string{"/rule_add hello", "/help", "/start@MyBot", "/rule_add@OtherBot hello"} {
		t.Run(command, func(t *testing.T) {
			tg := &fakeTelegram{admin: true}
			ruleStore := &fakeRules{}
			cache := &fakeCache{}
			svc := NewService(ruleStore, cache, &fakeAudit{}, tg, nil)
			svc.SetBotUsername("MyBot")
			svc.SetBuiltinFilter(builtin.New(true))
			message := testMessage()
			message.UserIsBot, message.Text = true, command
			if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
				t.Fatalf("benign bot command = %v, %v", deleted, err)
			}
			if ruleStore.addCalls != 0 || len(tg.sends) != 0 || !slices.Equal(cache.queries, []string{command}) {
				t.Fatalf("bot command executed or was not checked: adds=%d sends=%v queries=%v", ruleStore.addCalls, tg.sends, cache.queries)
			}
		})
	}
}

func TestBuiltinChecksCaptionHiddenLinkAndButtonOnlyAdvertisements(t *testing.T) {
	cases := []struct {
		name    string
		message domain.ModerationMessage
	}{
		{
			name: "caption",
			message: domain.ModerationMessage{
				Caption: "催情药现货批发，联系 @example_agent",
			},
		},
		{
			name: "hidden_link",
			message: domain.ModerationMessage{
				Text: "承接洗资业务，联系客服",
				Entities: []domain.MessageEntityInfo{{
					Type: "text_link", Offset: 7, Length: 4, HasURL: true, URL: "https://t.me/example_agent",
				}},
			},
		},
		{
			name: "button_only",
			message: domain.ModerationMessage{
				InlineButtons: []domain.InlineButtonInfo{{Text: "催情药现货批发", URL: "https://t.me/example_agent"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := &fakeTelegram{}
			audit := &fakeAudit{}
			cache := &fakeCache{}
			svc := NewService(&fakeRules{}, cache, audit, tg, nil)
			svc.SetBuiltinFilter(builtin.New(true))
			message := testMessage()
			message.Text, message.Caption = tc.message.Text, tc.message.Caption
			message.Entities, message.InlineButtons = tc.message.Entities, tc.message.InlineButtons
			if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
				t.Fatalf("metadata advertisement escaped moderation: %v, %v", deleted, err)
			}
			if len(audit.entries) != 1 || audit.entries[0].Content != message.Content() || audit.entries[0].BuiltinDetails == nil {
				t.Fatalf("unexpected audit: %+v", audit.entries)
			}
			if len(cache.queries) != 0 || len(tg.sends) != 1 || tg.sends[0].threadID == nil || *tg.sends[0].threadID != 77 {
				t.Fatalf("unexpected custom check or notice routing: cache=%v sends=%+v", cache.queries, tg.sends)
			}
		})
	}
}

func TestBenignButtonOnlyMessageDoesNotMatchEmptyCustomPattern(t *testing.T) {
	cache := &fakeCache{matched: []int64{1}}
	tg := &fakeTelegram{}
	svc := NewService(&fakeRules{}, cache, &fakeAudit{}, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))
	message := testMessage()
	message.Text = ""
	message.InlineButtons = []domain.InlineButtonInfo{{Text: "帮助", URL: "https://example.org/help"}}
	if deleted, err := svc.Process(context.Background(), message); err != nil || deleted {
		t.Fatalf("benign metadata-only message = %v, %v", deleted, err)
	}
	if len(cache.queries) != 0 || len(tg.deleteCalls) != 0 {
		t.Fatalf("empty text reached custom rules: %+v", cache.queries)
	}
}

func TestThreeStrikePolicyCountsDistinctMessagesAcrossEditsAndRedelivery(t *testing.T) {
	cases := []struct {
		name       string
		messageIDs []int
		deleteErr  error
		wantBans   int
	}{
		{name: "edit_and_redelivery", messageIDs: []int{9, 9, 9}},
		{name: "failed_deletion_and_edits", messageIDs: []int{9, 9, 9}, deleteErr: errors.New("forbidden")},
		{name: "three_distinct_messages", messageIDs: []int{9, 10, 11}, wantBans: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := &fakeTelegram{deleteErr: tc.deleteErr}
			audit := &fakeAudit{}
			svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{1, 2}}, audit, tg, nil)
			svc.SetSpamPolicy(3, 24*time.Hour)
			for i, messageID := range tc.messageIDs {
				field := "message"
				updateID := i + 1
				if i > 0 && messageID == tc.messageIDs[0] {
					field = "edited_message"
					if i == 2 {
						updateID = 2 // Exact redelivery of the edited update.
					}
				}
				payload, err := json.Marshal(map[string]any{
					"update_id": updateID,
					field: map[string]any{
						"message_id": messageID,
						"from":       map[string]any{"id": 42, "is_bot": false},
						"chat":       map[string]any{"id": 100, "type": "supergroup"},
						"text":       fmt.Sprintf("广告版本%d", min(i, 1)),
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				message, ok, err := telegram.ParseUpdate(payload)
				if err != nil || !ok {
					t.Fatalf("ParseUpdate = %+v, %v, %v", message, ok, err)
				}
				if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
					t.Fatal(err)
				}
				if i < 2 && len(tg.banCalls) != 0 {
					t.Fatal("user banned before three distinct messages")
				}
			}
			if len(tg.banCalls) != tc.wantBans || len(audit.entries) != len(tc.messageIDs) {
				t.Fatalf("bans=%v audits=%d, want %d bans and %d attempts", tg.banCalls, len(audit.entries), tc.wantBans, len(tc.messageIDs))
			}
		})
	}
}

func TestAdlogShowsStoredBuiltinExplanationAndLegacyIDs(t *testing.T) {
	message := testMessage()
	message.Text = "/adlog"
	audit := &fakeAudit{records: []domain.AuditEntry{
		{
			ID: 1, ChatID: message.ChatID, BuiltinHits: []string{"ad_money_laundering"},
			BuiltinDetails: &domain.BuiltinDetails{
				LibraryVersion: "2.0.0",
				Hits: []domain.BuiltinHit{{
					ID: "ad_money_laundering", Name: "洗钱洗资广告", Category: "金融黑产",
					Evidence: []string{"洗资业务词", "主动承接", "联系方式"},
				}},
			},
			DeleteSucceeded: true, ContentSummary: "已隐藏内容",
		},
		{ID: 2, ChatID: message.ChatID, BuiltinHits: []string{"ad_legacy_rule"}, ContentSummary: "旧记录"},
		{ID: 3, ChatID: message.ChatID, MatchedRuleIDs: []int64{77}, ContentSummary: "自定义规则记录"},
	}}
	tg := &fakeTelegram{admin: true}
	svc := NewService(&fakeRules{}, &fakeCache{}, audit, tg, nil)
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("adlog = %v, %v", deleted, err)
	}
	var text strings.Builder
	for _, send := range tg.sends {
		text.WriteString(send.text)
	}
	for _, want := range []string{"洗钱洗资广告", "主动承接", "库版本:2.0.0", "ad_legacy_rule", "规则:77", "已删除", "删除失败"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("adlog missing %q: %s", want, text.String())
		}
	}
}
