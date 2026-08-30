package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
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
	entries []domain.NewAuditEntry
}

func (f *fakeAudit) Record(_ context.Context, entry domain.NewAuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}
func (*fakeAudit) ListRecent(context.Context, int64, int) ([]domain.AuditEntry, error) {
	return nil, nil
}
func (*fakeAudit) DeleteExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type fakeTelegram struct {
	admin       bool
	adminErr    error
	deleteErr   error
	deleteCalls []int
	sends       []fakeSend
}
type fakeSend struct {
	chatID   int64
	threadID *int
	text     string
}

func (f *fakeTelegram) DeleteMessage(context.Context, int64, int) error {
	f.deleteCalls = append(f.deleteCalls, 1)
	return f.deleteErr
}
func (f *fakeTelegram) SendMessage(_ context.Context, chatID int64, threadID *int, text string) error {
	f.sends = append(f.sends, fakeSend{chatID: chatID, threadID: threadID, text: text})
	return nil
}
func (f *fakeTelegram) IsGroupAdmin(context.Context, int64, int64) (bool, error) {
	return f.admin, f.adminErr
}

type fakeRules struct{}

func (*fakeRules) LoadEnabled(context.Context) (map[int64][]domain.Rule, error) { return nil, nil }
func (*fakeRules) Add(context.Context, domain.NewRule) (domain.Rule, error) {
	return domain.Rule{ID: 1, Enabled: true}, nil
}
func (*fakeRules) List(context.Context, int64) ([]domain.Rule, error)   { return nil, nil }
func (*fakeRules) Remove(context.Context, int64, int64) error           { return nil }
func (*fakeRules) SetEnabled(context.Context, int64, int64, bool) error { return nil }

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

func TestProcessIgnoresBotsAndNonGroups(t *testing.T) {
	tg := &fakeTelegram{}
	audit := &fakeAudit{}
	cache := &fakeCache{matched: []int64{1}}
	svc := NewService(&fakeRules{}, cache, audit, tg, nil)

	bot := testMessage()
	bot.UserIsBot = true
	if deleted, err := svc.Process(context.Background(), bot); err != nil || deleted {
		t.Fatalf("bot message was moderated: %v, %v", deleted, err)
	}
	dm := testMessage()
	dm.ChatType = "private"
	if deleted, err := svc.Process(context.Background(), dm); err != nil || deleted {
		t.Fatalf("private message was moderated: %v, %v", deleted, err)
	}
	if len(cache.queries) != 0 || len(tg.deleteCalls) != 0 {
		t.Fatal("ignored messages must not touch the moderation path")
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

func TestCommandForOtherBotIsIgnored(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	cache := &fakeCache{matched: []int64{8}}
	svc := NewService(&fakeRules{}, cache, &fakeAudit{}, tg, nil)
	svc.SetBotUsername("MyBot")
	message := testMessage()
	message.Text = "/rule_test@OtherBot spam"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("command for another bot was handled: %v, %v", deleted, err)
	}
	if len(cache.queries) != 0 || len(tg.sends) != 0 {
		t.Fatalf("command for another bot had side effects: cache=%v sends=%v", cache.queries, tg.sends)
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
