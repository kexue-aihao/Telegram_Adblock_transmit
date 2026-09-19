package moderation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/profile"
)

type fakeProfiles struct {
	bio    string
	err    error
	calls  []int64
	onRead func()
}

func (f *fakeProfiles) GetUserBio(_ context.Context, id int64) (string, error) {
	f.calls = append(f.calls, id)
	if f.onRead != nil {
		f.onRead()
	}
	return f.bio, f.err
}

func TestBioModerationEligibilityAndFallback(t *testing.T) {
	const ad = "承接洗资业务，联系 @example_agent"
	for _, tc := range []struct {
		name        string
		configure   func(*Service, *domain.ModerationMessage, *fakeProfiles, *fakeTelegram, *fakeCache)
		wantDeleted bool
		wantQueries int
		wantBioHit  bool
	}{
		{name: "profile_ad", wantDeleted: true, wantQueries: 1, wantBioHit: true},
		{name: "caption", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text, m.Caption = "", "看我主页"
		}, wantDeleted: true, wantQueries: 1, wantBioHit: true},
		{name: "feature_off", configure: func(s *Service, _ *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			s.SetUserProfileReader(nil)
		}},
		{name: "library_off", configure: func(s *Service, _ *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			s.SetBuiltinFilter(builtin.New(false))
		}},
		{name: "no_library", configure: func(s *Service, _ *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			s.SetBuiltinFilter(nil)
		}},
		{name: "normal_message", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text = "大家好"
		}},
		{name: "warning", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text = "警惕看我主页这种引流"
		}},
		{name: "bot", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.UserIsBot = true
		}},
		{name: "anonymous", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			id := int64(-100)
			m.SenderChatID = &id
		}},
		{name: "no_user", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.UserID = nil
		}},
		{name: "invalid_user", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			*m.UserID = 0
		}},
		{name: "private", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.ChatType = "private"
		}},
		{name: "forward", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Forward = &domain.ForwardInfo{Type: "user"}
		}},
		{name: "empty_bio", configure: func(_ *Service, _ *domain.ModerationMessage, p *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			p.bio = ""
		}, wantQueries: 1},
		{name: "ordinary_link", configure: func(_ *Service, _ *domain.ModerationMessage, p *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			p.bio = "我的频道 https://t.me/example_channel"
		}, wantQueries: 1},
		{name: "unavailable", configure: func(_ *Service, _ *domain.ModerationMessage, p *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			p.err = errors.New("forbidden")
		}, wantQueries: 1},
		{name: "timeout", configure: func(_ *Service, _ *domain.ModerationMessage, p *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			p.err = context.DeadlineExceeded
		}, wantQueries: 1},
		{name: "rate_limit", configure: func(_ *Service, _ *domain.ModerationMessage, p *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			p.err = profile.ErrLookupSkipped
		}, wantQueries: 1},
		{name: "body_builtin", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text = ad + "。看我主页"
		}, wantDeleted: true},
		{name: "body_regex", configure: func(_ *Service, _ *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, c *fakeCache) {
			c.matched = []int64{7}
		}, wantDeleted: true},
		{name: "admin_management", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, tg *fakeTelegram, _ *fakeCache) {
			m.Text = "/rule_test 看我主页"
			tg.admin = true
		}},
		{name: "public_command", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text = "/help 看我主页"
		}, wantDeleted: true, wantQueries: 1, wantBioHit: true},
		{name: "unauthorized_command", configure: func(_ *Service, m *domain.ModerationMessage, _ *fakeProfiles, _ *fakeTelegram, _ *fakeCache) {
			m.Text = "/rule_test 看我主页"
		}, wantDeleted: true, wantQueries: 1, wantBioHit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg, audit, cache := &fakeTelegram{}, &fakeAudit{}, &fakeCache{}
			profiles := &fakeProfiles{bio: ad}
			svc := NewService(&fakeRules{}, cache, audit, tg, nil)
			svc.SetBuiltinFilter(builtin.New(true))
			svc.SetUserProfileReader(profiles)
			message := testMessage()
			message.Text = "看我主页"
			if tc.configure != nil {
				tc.configure(svc, &message, profiles, tg, cache)
			}
			deleted, err := svc.HandleUpdate(context.Background(), message)
			if err != nil || deleted != tc.wantDeleted || len(profiles.calls) != tc.wantQueries {
				t.Fatalf("result: deleted=%v error=%v queries=%v", deleted, err, profiles.calls)
			}
			if deleted {
				if len(audit.entries) != 1 || len(tg.deleteCalls) != 1 || audit.entries[0].Content != message.Content() {
					t.Fatalf("wrong enforcement: %+v", audit.entries)
				}
				if tc.wantBioHit {
					details := audit.entries[0].BuiltinDetails
					if details == nil || !slices.Contains(audit.entries[0].BuiltinHits, builtin.HitMoneyLaundering) || !strings.Contains(builtinAuditSummary(audit.records[0]), "证据来源：用户简介") {
						t.Fatalf("missing profile attribution: %+v", audit.entries[0])
					}
					if notice := tg.sends[len(tg.sends)-1]; notice.text != ModerationNotice || notice.threadID == nil || *notice.threadID != 77 {
						t.Fatalf("wrong notice routing: %+v", notice)
					}
				}
			} else if len(audit.entries) != 0 || len(tg.deleteCalls) != 0 {
				t.Fatal("non-hit received enforcement")
			}
			for _, input := range cache.queries {
				if input == ad {
					t.Fatal("custom regex received bio")
				}
			}
		})
	}
}

func TestCachedBioUsesCurrentSettings(t *testing.T) {
	ctx := context.Background()
	checker, err := builtin.NewManaged(ctx, true, &fakeBuiltinSettings{})
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeProfiles{bio: "承接洗资业务，联系 @example_agent"}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, &fakeTelegram{}, nil)
	svc.SetBuiltinFilter(checker)
	svc.SetUserProfileReader(profile.New(reader, nil))
	message := testMessage()
	message.Text = "看我主页"
	for _, enabled := range []bool{true, false, true} {
		if _, err := checker.Update(ctx, nil, map[string]bool{builtin.HitMoneyLaundering: enabled}); err != nil {
			t.Fatal(err)
		}
		deleted, err := svc.Process(ctx, message)
		if err != nil || deleted != enabled {
			t.Fatalf("enabled %v: deleted %v, err %v", enabled, deleted, err)
		}
	}
	if len(reader.calls) != 1 {
		t.Fatalf("cached bio fetched %d times", len(reader.calls))
	}
}

func TestBioStrikesAndDeletionFailure(t *testing.T) {
	for _, tc := range []struct {
		name              string
		ids               []int
		admin, failDelete bool
		bans              int
	}{
		{"edits_and_redelivery", []int{9, 9, 9}, false, false, 0},
		{"three_messages", []int{9, 10, 11}, false, false, 1},
		{"admin", []int{9, 10, 11}, true, false, 0},
		{"failed_delete", []int{9}, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg, audit := &fakeTelegram{admin: tc.admin}, &fakeAudit{}
			if tc.failDelete {
				tg.deleteErr = errors.New("forbidden")
			}
			svc := NewService(&fakeRules{}, &fakeCache{}, audit, tg, nil)
			svc.SetBuiltinFilter(builtin.New(true))
			svc.SetUserProfileReader(&fakeProfiles{bio: "承接洗资业务，联系 @example_agent"})
			svc.SetSpamPolicy(3, 24*time.Hour)
			message := testMessage()
			message.Text = "看我主页"
			for _, id := range tc.ids {
				message.MessageID = id
				deleted, err := svc.HandleUpdate(context.Background(), message)
				if err != nil || deleted == tc.failDelete {
					t.Fatalf("delete %v, error %v", deleted, err)
				}
			}
			if len(tg.banCalls) != tc.bans || len(audit.entries) != len(tc.ids) {
				t.Fatalf("bans %v, audit %v", tg.banCalls, audit.entries)
			}
			if tc.failDelete && (audit.entries[0].DeleteSucceeded || len(tg.sends) != 0) {
				t.Fatal("failed delete produced success notice")
			}
		})
	}
}

func TestBioLookupParentCancellationStopsEnforcement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &fakeProfiles{bio: "承接洗资业务，联系 @example_agent", onRead: cancel}
	tg := &fakeTelegram{}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))
	svc.SetUserProfileReader(reader)
	message := testMessage()
	message.Text = "看我主页"
	if deleted, err := svc.Process(ctx, message); deleted || !errors.Is(err, context.Canceled) || len(tg.deleteCalls) != 0 {
		t.Fatalf("parent cancellation ignored: %v, %v", deleted, err)
	}
}
