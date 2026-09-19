package moderation

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
)

// The notice names the layer that deleted the message. The built-in library
// wins because administrators cannot inspect its patterns themselves, and
// because the built-in filter is evaluated before the rule cache.
func TestDeletionNoticeNamesTheMatchingLayer(t *testing.T) {
	cases := []struct {
		name        string
		filter      *builtin.Checker
		text        string
		matched     []int64
		wantNotice  string
		wantBuiltin bool
	}{
		{"rule only", nil, "限时优惠，立即下单", []int64{3}, ModerationNotice, false},
		{"builtin only", builtin.New(true), "承接洗资业务，联系 @example_agent", nil, BuiltinNotice, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := &fakeTelegram{}
			audit := &fakeAudit{}
			svc := NewService(&fakeRules{}, &fakeCache{matched: tc.matched}, audit, tg, nil)
			svc.SetBuiltinFilter(tc.filter)
			svc.SetNoticeTTL(0) // keep the notice so the test can read it

			message := testMessage()
			message.Text = tc.text
			if deleted, err := svc.Process(context.Background(), message); err != nil || !deleted {
				t.Fatalf("Process() = %v, %v", deleted, err)
			}
			sends := tg.sentMessages()
			if len(sends) != 1 || sends[0].text != tc.wantNotice {
				t.Fatalf("notice = %+v, want %q", sends, tc.wantNotice)
			}
			if len(audit.entries) != 1 {
				t.Fatalf("unexpected audit entries: %+v", audit.entries)
			}
			if gotBuiltin := len(audit.entries[0].BuiltinHits) > 0; gotBuiltin != tc.wantBuiltin {
				t.Fatalf("builtin hits = %v, want builtin=%v", audit.entries[0].BuiltinHits, tc.wantBuiltin)
			}
		})
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before the deadline")
}

// The notice is removed after the configured delay, and only the notice: the
// advertisement itself was deleted first, with its own message ID.
func TestDeletionNoticeIsRemovedAfterTheTTL(t *testing.T) {
	tg := &fakeTelegram{}
	svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{3}}, &fakeAudit{}, tg, nil)
	svc.SetNoticeTTL(20 * time.Millisecond)

	if deleted, err := svc.Process(context.Background(), testMessage()); err != nil || !deleted {
		t.Fatalf("Process() = %v, %v", deleted, err)
	}
	notices := tg.sentMessages()
	if len(notices) != 1 {
		t.Fatalf("expected one notice, got %+v", notices)
	}
	noticeID := notices[0].messageID
	if slices.Contains(tg.deletedMessageIDs(), noticeID) {
		t.Fatal("the notice was removed before its delay elapsed")
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return slices.Contains(tg.deletedMessageIDs(), noticeID)
	})
	if deletes := tg.deletedMessageIDs(); !slices.Contains(deletes, 9) {
		t.Fatalf("the advertisement itself was not deleted: %v", deletes)
	}
}

func TestNoticeTTLZeroKeepsNotices(t *testing.T) {
	tg := &fakeTelegram{}
	svc := NewService(&fakeRules{}, &fakeCache{matched: []int64{3}}, &fakeAudit{}, tg, nil)
	svc.SetNoticeTTL(0)

	if deleted, err := svc.Process(context.Background(), testMessage()); err != nil || !deleted {
		t.Fatalf("Process() = %v, %v", deleted, err)
	}
	time.Sleep(60 * time.Millisecond)
	noticeID := tg.sentMessages()[0].messageID
	if slices.Contains(tg.deletedMessageIDs(), noticeID) {
		t.Fatal("notice cleanup ran although the TTL is disabled")
	}
}

// Command replies are answers an administrator asked for; only the automated
// deletion notice is transient.
func TestCommandRepliesAreNotRemovedAutomatically(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetNoticeTTL(10 * time.Millisecond)

	message := testMessage()
	message.Text = "/rule_list"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(tg.sentMessages()) != 1 {
		t.Fatalf("expected one command reply, got %+v", tg.sentMessages())
	}
	time.Sleep(60 * time.Millisecond)
	if deletes := tg.deletedMessageIDs(); len(deletes) != 0 {
		t.Fatalf("command replies must not be removed automatically: %v", deletes)
	}
}
