package moderation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type fakeCache struct {
	matched []int64
	queries []string
}

func (f *fakeCache) Match(_ int64, content string) []int64 {
	f.queries = append(f.queries, content)
	return append([]int64(nil), f.matched...)
}
func (*fakeCache) Replace(int64, []domain.CompiledRule) {}
func (*fakeCache) Remove(int64)                         {}

type fakeAudit struct {
	entries   []domain.NewAuditEntry
	records   []domain.AuditEntry
	hitsSince time.Time
}

func (f *fakeAudit) Record(_ context.Context, entry domain.NewAuditEntry) error {
	f.entries = append(f.entries, entry)
	f.records = append(f.records, domain.AuditEntry{
		ID: int64(len(f.records) + 1), ChatID: entry.ChatID, ChatTitle: entry.ChatTitle,
		MessageThreadID: entry.MessageThreadID, UserID: entry.UserID, MessageID: entry.MessageID,
		MatchedRuleIDs: entry.MatchedRuleIDs, BuiltinHits: entry.BuiltinHits, BuiltinDetails: entry.BuiltinDetails,
		ContentSummary: entry.Content, DeleteSucceeded: entry.DeleteSucceeded, DeletionError: entry.DeletionError,
		OccurredAt: time.Now(),
	})
	return nil
}
func (f *fakeAudit) ListRecent(_ context.Context, chatID int64, limit int) ([]domain.AuditEntry, error) {
	var entries []domain.AuditEntry
	for i := len(f.records) - 1; i >= 0 && len(entries) < limit; i-- {
		if f.records[i].ChatID == chatID {
			entries = append(entries, f.records[i])
		}
	}
	return entries, nil
}
func (f *fakeAudit) CountHits(_ context.Context, chatID, userID int64, since time.Time) (int64, error) {
	f.hitsSince = since
	seen := make(map[int]bool)
	for _, entry := range f.records {
		if entry.ChatID == chatID && entry.UserID != nil && *entry.UserID == userID && !entry.OccurredAt.Before(since) {
			seen[entry.MessageID] = true
		}
	}
	return int64(len(seen)), nil
}
func (*fakeAudit) DeleteExpired(context.Context, time.Time) (int64, error) { return 0, nil }

func auditWithPriorHits(message domain.ModerationMessage, count int) *fakeAudit {
	audit := &fakeAudit{}
	for i := 0; i < count; i++ {
		_ = audit.Record(context.Background(), domain.NewAuditEntry{
			ChatID: message.ChatID, UserID: message.UserID, MessageID: message.MessageID + i + 1,
			MatchedRuleIDs: []int64{1}, Content: "earlier advertisement", DeleteSucceeded: true,
		})
	}
	return audit
}

type fakeTelegram struct {
	admin       bool
	adminErr    error
	deleteErr   error
	deleteCalls []int
	banCalls    []int64
	banErr      error
	sends       []fakeSend
}
type fakeSend struct {
	chatID   int64
	threadID *int
	text     string
}

func (f *fakeTelegram) DeleteMessage(_ context.Context, _ int64, messageID int) error {
	f.deleteCalls = append(f.deleteCalls, messageID)
	return f.deleteErr
}
func (f *fakeTelegram) SendMessage(_ context.Context, chatID int64, threadID *int, text string) error {
	f.sends = append(f.sends, fakeSend{chatID: chatID, threadID: threadID, text: text})
	return nil
}
func (f *fakeTelegram) IsGroupAdmin(context.Context, int64, int64) (bool, error) {
	return f.admin, f.adminErr
}
func (f *fakeTelegram) BanChatMember(_ context.Context, _ int64, userID int64) error {
	f.banCalls = append(f.banCalls, userID)
	return f.banErr
}

type fakeRules struct {
	addCalls int
}

func (*fakeRules) LoadEnabled(context.Context) (map[int64][]domain.Rule, error) { return nil, nil }
func (f *fakeRules) Add(context.Context, domain.NewRule) (domain.Rule, error) {
	f.addCalls++
	return domain.Rule{ID: 1, Enabled: true}, nil
}
func (*fakeRules) List(context.Context, int64) ([]domain.Rule, error)   { return nil, nil }
func (*fakeRules) Remove(context.Context, int64, int64) error           { return nil }
func (*fakeRules) SetEnabled(context.Context, int64, int64, bool) error { return nil }
func (*fakeRules) UpdatePattern(context.Context, int64, int64, string) (domain.Rule, error) {
	return domain.Rule{}, nil
}

func testMessage() domain.ModerationMessage {
	userID := int64(42)
	threadID := 77
	return domain.ModerationMessage{
		ChatID: 100, ChatTitle: "Test", ChatType: "supergroup", MessageID: 9,
		MessageThreadID: &threadID, UserID: &userID, Text: "buy spam now",
	}
}

func TestProcessDeletesAuditsThenNoticesInTopic(t *testing.T) {
	tg := &fakeTelegram{}
	audit := &fakeAudit{}
	cache := &fakeCache{matched: []int64{2, 5}}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)

	deleted, err := svc.Process(context.Background(), testMessage())
	if err != nil || !deleted {
		t.Fatalf("Process() = %v, %v", deleted, err)
	}
	if len(tg.deleteCalls) != 1 || len(audit.entries) != 1 || len(tg.sends) != 1 {
		t.Fatalf("calls delete=%d audit=%d sends=%d", len(tg.deleteCalls), len(audit.entries), len(tg.sends))
	}
	if got := audit.entries[0].MatchedRuleIDs; len(got) != 2 || got[0] != 2 || got[1] != 5 || !audit.entries[0].DeleteSucceeded {
		t.Fatalf("unexpected audit entry: %+v", audit.entries[0])
	}
	if tg.sends[0].threadID == nil || *tg.sends[0].threadID != 77 {
		t.Fatalf("thread ID was not preserved: %+v", tg.sends[0])
	}
}

func TestProcessDeletionFailureAuditsWithoutNotice(t *testing.T) {
	tg := &fakeTelegram{deleteErr: errors.New("forbidden")}
	audit := &fakeAudit{}
	svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{1}}, audit, tg, nil)

	deleted, err := svc.Process(context.Background(), testMessage())
	if err != nil || deleted {
		t.Fatalf("Process() = %v, %v", deleted, err)
	}
	if len(audit.entries) != 1 || audit.entries[0].DeleteSucceeded || audit.entries[0].DeletionError != "forbidden" {
		t.Fatalf("unexpected failed audit: %+v", audit.entries)
	}
	if len(tg.sends) != 0 {
		t.Fatal("a failed deletion must not send a success notice")
	}
}

func TestProcessIgnoresNonGroupsAndModeratesBotAds(t *testing.T) {
	tg := &fakeTelegram{}
	audit := &fakeAudit{}
	cache := &fakeCache{}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))

	bot := testMessage()
	bot.UserIsBot = true
	bot.Text = "@adservicebot 限时优惠，立即下单 https://t.me/+abc123"
	bot.Entities = []domain.MessageEntityInfo{{Type: "mention", Username: "adservicebot"}}
	if deleted, err := svc.HandleUpdate(context.Background(), bot); err != nil || !deleted {
		t.Fatalf("bot advertisement was not moderated: %v, %v", deleted, err)
	}
	if len(audit.entries) != 1 || !slices.Contains(audit.entries[0].BuiltinHits, builtin.HitBotMention) {
		t.Fatalf("external bot mention was not recorded as a builtin hit: %+v", audit.entries)
	}
	dm := testMessage()
	dm.ChatType = "private"
	if deleted, err := svc.Process(context.Background(), dm); err != nil || deleted {
		t.Fatalf("private message was moderated: %v, %v", deleted, err)
	}
	if len(cache.queries) != 0 || len(tg.deleteCalls) != 1 {
		t.Fatalf("unexpected moderation calls: cache=%v deletes=%v", cache.queries, tg.deleteCalls)
	}
}

func TestNonAdminManagementCommandCannotBypassModeration(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	audit := &fakeAudit{}
	cache := &fakeCache{matched: []int64{8}}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)

	message := testMessage()
	message.Text = "/rule_test spam"
	if deleted, err := svc.HandleCommand(context.Background(), message); err != nil || !deleted {
		t.Fatalf("non-admin command was not moderated: %v, %v", deleted, err)
	}
	if len(tg.sends) != 2 || tg.sends[0].text != PermissionNotice || tg.sends[1].text != ModerationNotice {
		t.Fatalf("expected permission and moderation notices, got %+v", tg.sends)
	}
	if len(audit.entries) != 1 || len(tg.deleteCalls) != 1 {
		t.Fatal("non-admin command should be audited and deleted when it matches")
	}
}

func TestAdminCommandIsExempt(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	audit := &fakeAudit{}
	cache := &fakeCache{matched: []int64{8}}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)

	message := testMessage()
	message.Text = "/rule_test spam"
	if deleted, err := svc.HandleCommand(context.Background(), message); err != nil || deleted {
		t.Fatalf("admin command was moderated: %v, %v", deleted, err)
	}
	if len(cache.queries) != 1 || len(tg.deleteCalls) != 0 {
		t.Fatal("admin management commands must not delete the command message")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		content, name, args string
		ok                  bool
	}{
		{"/RULE_ADD@MyBot foo bar", "rule_add", "foo bar", true},
		{" /adlog ", "adlog", "", true},
		{"hello", "", "", false},
	}
	for _, tc := range tests {
		name, args, ok := ParseCommand(tc.content)
		if name != tc.name || args != tc.args || ok != tc.ok {
			t.Errorf("ParseCommand(%q) = %q, %q, %v", tc.content, name, args, ok)
		}
	}
}

func TestCommandForOtherBotIsModeratedWithoutExecutingCommand(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	cache := &fakeCache{matched: []int64{8}}
	svc := NewService(&fakeRules{}, cache, &fakeAudit{}, tg, nil)
	svc.SetBotUsername("MyBot")
	message := testMessage()
	message.Text = "/rule_test@OtherBot spam"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
		t.Fatalf("command for another bot escaped moderation: %v, %v", deleted, err)
	}
	if !slices.Equal(cache.queries, []string{message.Text}) || len(tg.sends) != 1 || tg.sends[0].text != ModerationNotice {
		t.Fatalf("command was executed instead of moderated: cache=%v sends=%v", cache.queries, tg.sends)
	}
}

func TestCommandForThisBotIsHandled(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	cache := &fakeCache{matched: []int64{8}}
	svc := NewService(&fakeRules{}, cache, &fakeAudit{}, tg, nil)
	svc.SetBotUsername("MyBot")
	message := testMessage()
	message.Text = "/rule_test@mybot spam"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("command for this bot failed: %v, %v", deleted, err)
	}
	if len(cache.queries) != 1 || len(tg.sends) != 1 {
		t.Fatalf("command for this bot was not handled: cache=%v sends=%v", cache.queries, tg.sends)
	}
}

func TestStartAndHelpAnswerAnywhere(t *testing.T) {
	for _, command := range []string{"/start", "/help", "/START", "/help@MyBot"} {
		tg := &fakeTelegram{admin: false}
		svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
		svc.SetBotUsername("MyBot")

		group := testMessage()
		group.Text = command
		if deleted, err := svc.HandleUpdate(context.Background(), group); err != nil || deleted {
			t.Fatalf("%s in group failed: %v, %v", command, deleted, err)
		}
		if len(tg.sends) != 1 {
			t.Fatalf("%s in group sent %d messages, want 1", command, len(tg.sends))
		}
		if got := tg.sends[0].text; got != HelpText() {
			t.Fatalf("%s reply != HelpText: %d bytes", command, len(got))
		}

		dm := testMessage()
		dm.ChatType = "private"
		dm.Text = command
		if deleted, err := svc.HandleUpdate(context.Background(), dm); err != nil || deleted {
			t.Fatalf("%s in private chat failed: %v, %v", command, deleted, err)
		}
		if len(tg.sends) != 2 {
			t.Fatalf("%s in private chat did not reply (sends=%d)", command, len(tg.sends))
		}
	}
}

func TestStartDirectedAtOtherBotIsIgnored(t *testing.T) {
	tg := &fakeTelegram{}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBotUsername("MyBot")
	message := testMessage()
	message.Text = "/start@OtherBot"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("start for another bot was handled: %v, %v", deleted, err)
	}
	if len(tg.sends) != 0 {
		t.Fatal("start for another bot produced a reply")
	}
}

func TestBuiltinFilterDeletesAndAuditsOnSight(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	audit := &fakeAudit{}
	cache := &fakeCache{}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))

	message := testMessage()
	message.Text = "限时优惠，立即下单 https://t.me/+abc123"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
		t.Fatalf("builtin filter did not delete: %v, %v", deleted, err)
	}
	if len(audit.entries) != 1 || len(audit.entries[0].BuiltinHits) == 0 {
		t.Fatalf("builtin hits not audited: %+v", audit.entries)
	}
	entry := audit.entries[0]
	if !entry.DeleteSucceeded || entry.MatchedRuleIDs != nil {
		t.Fatalf("unexpected audit entry: %+v", entry)
	}
	if entry.BuiltinDetails == nil || entry.BuiltinDetails.LibraryVersion == "" {
		t.Fatalf("builtin details missing from audit: %+v", entry)
	}
	detailedIDs := make([]string, 0, len(entry.BuiltinDetails.Hits))
	for _, hit := range entry.BuiltinDetails.Hits {
		detailedIDs = append(detailedIDs, hit.ID)
		if hit.Name == "" || len(hit.Evidence) == 0 {
			t.Fatalf("builtin explanation missing: %+v", hit)
		}
	}
	if !slices.Equal(detailedIDs, entry.BuiltinHits) {
		t.Fatalf("audit IDs and details disagree: ids=%v details=%v", entry.BuiltinHits, detailedIDs)
	}
	// The built-in filter fires before the per-group rule cache is consulted.
	if len(cache.queries) != 0 {
		t.Fatalf("rule cache consulted for a builtin hit: %v", cache.queries)
	}
	if len(tg.sends) != 1 || tg.sends[0].text != ModerationNotice {
		t.Fatalf("expected moderation notice, got %+v", tg.sends)
	}
}

type fakeBuiltinSettings struct{}

func (*fakeBuiltinSettings) GetBuiltinSettings(context.Context) (domain.BuiltinSettings, error) {
	return domain.BuiltinSettings{}, ports.ErrBuiltinSettingsNotFound
}
func (*fakeBuiltinSettings) SaveBuiltinSettings(context.Context, domain.BuiltinSettings) error {
	return nil
}

func TestBuiltinUpdatesAffectModerationWithoutDisablingCustomRules(t *testing.T) {
	ctx := context.Background()
	checker, err := builtin.NewManaged(ctx, true, &fakeBuiltinSettings{})
	if err != nil {
		t.Fatal(err)
	}
	cache := &fakeCache{}
	audit := &fakeAudit{}
	service := NewService(&fakeRules{}, cache, audit, &fakeTelegram{}, nil)
	service.SetBuiltinFilter(checker)
	message := testMessage()
	message.Text = "限时优惠，立即下单 https://t.me/+abc123"
	if deleted, err := service.Process(ctx, message); err != nil || !deleted {
		t.Fatalf("initial detection: %v, %v", deleted, err)
	}
	updates := make(map[string]bool)
	for _, id := range audit.entries[0].BuiltinHits {
		updates[id] = false
	}
	if _, err := checker.Update(ctx, nil, updates); err != nil {
		t.Fatal(err)
	}
	if deleted, err := service.Process(ctx, message); err != nil || deleted {
		t.Fatalf("per-rule update not applied: %v, %v", deleted, err)
	}
	off := false
	if _, err := checker.Update(ctx, &off, nil); err != nil {
		t.Fatal(err)
	}
	cache.matched = []int64{42}
	if deleted, err := service.Process(ctx, message); err != nil || !deleted {
		t.Fatalf("custom rule affected: %v, %v", deleted, err)
	}
	last := audit.entries[len(audit.entries)-1]
	if !slices.Equal(last.MatchedRuleIDs, []int64{42}) || len(last.BuiltinHits) != 0 {
		t.Fatalf("wrong audit attribution: %+v", last)
	}
}

func TestBuiltinFilterSkipWhenDisabled(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	audit := &fakeAudit{}
	cache := &fakeCache{}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)
	svc.SetBuiltinFilter(builtin.New(false))

	// With the filter off, the same invite text is judged by the rule cache
	// (which does not match) and must be left untouched.
	message := testMessage()
	message.Text = "限时优惠，立即下单 https://t.me/+abc123"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("disabled builtin filter changed behavior: %v, %v", deleted, err)
	}
	if len(cache.queries) != 1 || len(audit.entries) != 0 {
		t.Fatalf("disabled filter touched the moderation path: cache=%v audit=%v", cache.queries, audit.entries)
	}
}

func TestThreeStrikePolicyBansNonAdmin(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	audit := auditWithPriorHits(testMessage(), 2)
	svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{1}}, audit, tg, nil)
	svc.SetSpamPolicy(3, 24*time.Hour)

	message := testMessage()
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
		t.Fatalf("strike hit not deleted: %v, %v", deleted, err)
	}
	if len(tg.banCalls) != 1 || tg.banCalls[0] != *message.UserID {
		t.Fatalf("ban calls = %v, want single ban of user %d", tg.banCalls, *message.UserID)
	}
	// The strike window was passed through to the count query.
	if audit.hitsSince.IsZero() {
		t.Fatal("CountHits was not called with a window")
	}
}

func TestThreeStrikePolicySkipsAdminsAndUnderLimit(t *testing.T) {
	// Three hits by an admin must not trigger a ban.
	adminTG := &fakeTelegram{admin: true}
	svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{1}}, auditWithPriorHits(testMessage(), 2), adminTG, nil)
	svc.SetSpamPolicy(3, 24*time.Hour)
	if _, err := svc.HandleUpdate(context.Background(), testMessage()); err != nil {
		t.Fatal(err)
	}
	if len(adminTG.banCalls) != 0 {
		t.Fatalf("admin user was banned: %v", adminTG.banCalls)
	}

	// Two hits are under the limit and must not trigger a ban.
	lightTG := &fakeTelegram{admin: false}
	svc2 := NewService(&fakeRules{}, &fakeCache{matched: []int64{1}}, auditWithPriorHits(testMessage(), 1), lightTG, nil)
	svc2.SetSpamPolicy(3, 24*time.Hour)
	if _, err := svc2.HandleUpdate(context.Background(), testMessage()); err != nil {
		t.Fatal(err)
	}
	if len(lightTG.banCalls) != 0 {
		t.Fatalf("sub-limit user was banned: %v", lightTG.banCalls)
	}
}

func TestHelpTextCoversAllManagementCommands(t *testing.T) {
	text := HelpText()
	for _, info := range BotMenu {
		if !strings.Contains(text, "/"+info.Name) {
			t.Errorf("help text missing /%s", info.Name)
		}
	}
}

func TestChunkLinesSplitsLongUTF8Line(t *testing.T) {
	input := strings.Repeat("广告", 2500)
	chunks := chunkLines([]string{input}, messageChunkSize)
	if len(chunks) < 2 {
		t.Fatal("long line was not split")
	}
	var joined strings.Builder
	for _, chunk := range chunks {
		if len(chunk) > messageChunkSize || !utf8.ValidString(chunk) {
			t.Fatalf("invalid chunk length or UTF-8: bytes=%d", len(chunk))
		}
		joined.WriteString(chunk)
	}
	if joined.String() != input {
		t.Fatal("split chunks changed content")
	}
}
