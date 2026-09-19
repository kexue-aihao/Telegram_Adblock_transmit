package moderation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	botsettings "github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
)

var errSaveFailed = errors.New("database is down")

type fakeBotSettings struct {
	stored  *domain.BotSettings
	saveErr error
}

func (f *fakeBotSettings) GetBotSettings(context.Context) (domain.BotSettings, error) {
	if f.stored == nil {
		return domain.BotSettings{}, ports.ErrBotSettingsNotFound
	}
	return f.stored.Clone(), nil
}

func (f *fakeBotSettings) SaveBotSettings(_ context.Context, next domain.BotSettings) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	clone := next.Clone()
	f.stored = &clone
	return nil
}

func managedSettings(t *testing.T, defaults domain.BotSettings) *botsettings.Manager {
	t.Helper()
	manager, err := botsettings.NewManager(context.Background(), defaults, &fakeBotSettings{})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

// The owner fallback is what lets the bot creator manage a group they do not
// administer, which is the documented behaviour for the bot owner.
func TestOwnerManagesGroupsWhereTheyAreNotAdmin(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBotSettings(managedSettings(t, domain.BotSettings{OwnerUserIDs: []int64{42}}))

	message := testMessage()
	message.Text = "/rule_add 限时优惠"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("owner command failed: %v, %v", deleted, err)
	}
	if store.addCalls != 1 {
		t.Fatalf("owner could not add a rule: %+v", store.added)
	}
	if len(tg.deleteCalls) != 0 {
		t.Fatal("the owner's command text must stay exempt from moderation")
	}
}

func TestNonAdminIsStillDeniedWithoutTheCrossGroupPermission(t *testing.T) {
	tg := &fakeTelegram{admin: false}
	store := &fakeRules{}
	svc := NewService(store, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBotSettings(managedSettings(t, domain.BotSettings{}))

	message := testMessage()
	message.Text = "/rule_list"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if store.addCalls != 0 || len(tg.sends) != 1 {
		t.Fatalf("non-admin executed a command: %+v", tg.sends)
	}
	if !strings.HasPrefix(tg.sends[0].text, PermissionNotice) || !strings.Contains(tg.sends[0].text, "42") {
		t.Fatalf("denial should point at the setup path with the sender's ID: %q", tg.sends[0].text)
	}
}

func TestCrossGroupPermissionAllowsCommandsButNotAnAdBypass(t *testing.T) {
	// A clean command from a non-admin runs when the permission is on.
	tg := &fakeTelegram{admin: false}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBotSettings(managedSettings(t, domain.BotSettings{CrossGroupManagement: true}))
	message := testMessage()
	message.Text = "/rule_list"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("cross-group command failed: %v, %v", deleted, err)
	}
	if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, "本群尚未设置广告规则") {
		t.Fatalf("cross-group command did not run: %+v", tg.sends)
	}

	// The same permission must never exempt an advertisement that hides behind
	// a command prefix.
	tg = &fakeTelegram{admin: false}
	audit := &fakeAudit{}
	svc = NewService(&fakeRules{}, &fakeCache{matched: []int64{5}}, audit, tg, nil)
	svc.SetBotSettings(managedSettings(t, domain.BotSettings{CrossGroupManagement: true}))
	message = testMessage()
	message.Text = "/rule_list 限时优惠 https://t.me/+spam"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
		t.Fatalf("advertisement behind a command was not deleted: %v, %v", deleted, err)
	}
	if len(audit.entries) != 1 || len(tg.deleteCalls) != 1 {
		t.Fatalf("missing audit or deletion: %+v", audit.entries)
	}
	if len(tg.sends) != 1 || tg.sends[0].text != ModerationNotice {
		t.Fatalf("the command must not run once it was moderated: %+v", tg.sends)
	}
}

func TestSettingsCommandReportsStateAndRequiresOwner(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))
	svc.SetBotSettings(managedSettings(t, domain.BotSettings{BioCheckEnabled: true, OwnerUserIDs: []int64{7}}))

	message := testMessage()
	message.Text = "/settings"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(tg.sends) != 1 {
		t.Fatalf("expected one report, got %+v", tg.sends)
	}
	report := tg.sends[0].text
	for _, want := range []string{"当前运行设置", "内置广告库：开启", "简介辅助检测：开启", "仅群管理员与机器人所有者", "7"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report %q is missing %q", report, want)
		}
	}

	// A group administrator who is not an owner may read but not change.
	tg.sends = nil
	message.Text = "/settings bio_check off"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, "只有机器人所有者") {
		t.Fatalf("non-owner changed a global switch: %+v", tg.sends)
	}
}

func TestSettingsCommandTogglesBioCheckAndBuiltin(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	checker, err := builtin.NewManaged(context.Background(), true, &fakeBuiltinSettings{})
	if err != nil {
		t.Fatal(err)
	}
	manager := managedSettings(t, domain.BotSettings{OwnerUserIDs: []int64{42}})
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBuiltinFilter(checker)
	svc.SetBotSettings(manager)

	message := testMessage() // user 42, the owner
	for _, tc := range []struct {
		command string
		want    string
		check   func() bool
	}{
		{"/settings bio_check on", "简介辅助检测已开启", func() bool { return manager.Settings().BioCheckEnabled }},
		{"/settings bio_check off", "简介辅助检测已关闭", func() bool { return !manager.Settings().BioCheckEnabled }},
		{"/settings builtin off", "内置广告库总开关已关闭", func() bool { return !checker.Enabled() }},
		{"/settings builtin on", "内置广告库总开关已开启", func() bool { return checker.Enabled() }},
	} {
		tg.sends = nil
		message.Text = tc.command
		if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
		if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, tc.want) {
			t.Fatalf("%s replied %+v", tc.command, tg.sends)
		}
		if !tc.check() {
			t.Fatalf("%s did not change the runtime value", tc.command)
		}
	}

	// Usage problems are reported instead of silently ignored.
	for _, command := range []string{"/settings bio_check", "/settings nope on", "/settings bio_check maybe"} {
		tg.sends = nil
		message.Text = command
		if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, "用法") && !strings.Contains(tg.sends[0].text, "on 或 off") {
			t.Fatalf("%s replied %+v", command, tg.sends)
		}
	}
}

// The profile check is switched at runtime: no restart and no profile request
// while it is off.
func TestBioCheckRuntimeToggleTakesEffectImmediately(t *testing.T) {
	const ad = "承接洗资业务，联系 @example_agent"
	manager := managedSettings(t, domain.BotSettings{OwnerUserIDs: []int64{42}})
	profiles := &fakeProfiles{bio: ad}
	tg := &fakeTelegram{}
	audit := &fakeAudit{}
	svc := NewService(&fakeRules{}, &fakeCache{}, audit, tg, nil)
	svc.SetBuiltinFilter(builtin.New(true))
	svc.SetUserProfileReader(profiles)
	svc.SetBotSettings(manager)

	message := testMessage()
	message.Text = "看我主页"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || deleted {
		t.Fatalf("disabled bio check still enforced: %v, %v", deleted, err)
	}
	if len(profiles.calls) != 0 {
		t.Fatalf("disabled bio check queried profiles: %v", profiles.calls)
	}

	message.Text = "/settings bio_check on"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	message.Text = "看我主页"
	if deleted, err := svc.HandleUpdate(context.Background(), message); err != nil || !deleted {
		t.Fatalf("enabled bio check did not enforce: %v, %v", deleted, err)
	}
	if len(profiles.calls) != 1 {
		t.Fatalf("profile queries = %v", profiles.calls)
	}
	if len(audit.entries) != 1 || len(audit.entries[0].BuiltinHits) == 0 {
		t.Fatalf("bio hit was not audited: %+v", audit.entries)
	}
}

// A settings write that fails must not change what the bot enforces.
func TestSettingsCommandKeepsRunningValueWhenSaveFails(t *testing.T) {
	tg := &fakeTelegram{admin: true}
	store := &fakeBotSettings{saveErr: errSaveFailed}
	manager, err := botsettings.NewManager(context.Background(), domain.BotSettings{OwnerUserIDs: []int64{42}}, store)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(&fakeRules{}, &fakeCache{}, &fakeAudit{}, tg, nil)
	svc.SetBotSettings(manager)

	message := testMessage()
	message.Text = "/settings bio_check on"
	if _, err := svc.HandleUpdate(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(tg.sends) != 1 || !strings.Contains(tg.sends[0].text, "保存设置失败") {
		t.Fatalf("failed save was reported as success: %+v", tg.sends)
	}
	if manager.Settings().BioCheckEnabled {
		t.Fatal("a failed save changed the runtime value")
	}
	if !slices.Equal(manager.Settings().OwnerUserIDs, []int64{42}) {
		t.Fatal("a failed save changed the owner list")
	}
}
