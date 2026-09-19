package webui

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	botsettings "github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
)

// fakeBotSettingsStore mirrors the production repository for panel tests.
type fakeBotSettingsStore struct {
	stored  *domain.BotSettings
	saveErr error
}

func (f *fakeBotSettingsStore) GetBotSettings(context.Context) (domain.BotSettings, error) {
	if f.stored == nil {
		return domain.BotSettings{}, ports.ErrBotSettingsNotFound
	}
	return f.stored.Clone(), nil
}

func (f *fakeBotSettingsStore) SaveBotSettings(_ context.Context, next domain.BotSettings) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	clone := next.Clone()
	f.stored = &clone
	return nil
}

// panelWithBotSettings builds a panel whose runtime settings are writable, and
// returns the manager so tests can assert on what was stored.
func panelWithBotSettings(t *testing.T) (http.Handler, *botsettings.Manager) {
	t.Helper()
	manager, err := botsettings.NewManager(context.Background(), domain.BotSettings{}, &fakeBotSettingsStore{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		Addr: "127.0.0.1:0", RuleStore: newFakeRuleStore(), ChatStore: &fakeChatStore{},
		AuditStore: &fakePanelAudit{}, Refresher: &fakeRefresher{}, SettingsStore: &fakePanelSettings{},
		BuiltinFilter: newTestBuiltin(t, &fakeBuiltinSettings{}),
		BotSettings:   manager,
		Username:      "admin", Password: "hunter2", SessionSecret: []byte("test-secret"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s.securityHeaders(s.router), manager
}

func TestBotSettingsPanelRoundTrip(t *testing.T) {
	handler, manager := panelWithBotSettings(t)

	res := authedRequest(t, handler, http.MethodGet, "/api/bot-settings", "", false)
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d body = %s", res.Code, res.Body.String())
	}
	var initial botSettingsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.BioCheckEnabled || initial.CrossGroupManagement || len(initial.OwnerUserIDs) != 0 {
		t.Fatalf("unexpected defaults: %+v", initial)
	}
	if initial.OwnerUserIDs == nil {
		t.Fatal("owner list must be encoded as an array, never null")
	}

	res = authedRequest(t, handler, http.MethodPatch, "/api/bot-settings",
		`{"bio_check_enabled":true,"cross_group_management":true,"owner_user_ids":[42,7]}`, true)
	if res.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body = %s", res.Code, res.Body.String())
	}
	var updated botSettingsResponse
	if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if !updated.BioCheckEnabled || !updated.CrossGroupManagement {
		t.Fatalf("switches were not stored: %+v", updated)
	}
	if len(updated.OwnerUserIDs) != 2 || updated.OwnerUserIDs[0] != 7 {
		t.Fatalf("owners were not normalized: %v", updated.OwnerUserIDs)
	}
	if current := manager.Settings(); !current.IsOwner(42) || !current.BioCheckEnabled {
		t.Fatalf("manager did not publish the change: %+v", current)
	}

	// A partial update leaves the other switches alone.
	res = authedRequest(t, handler, http.MethodPatch, "/api/bot-settings", `{"bio_check_enabled":false}`, true)
	if res.Code != http.StatusOK {
		t.Fatalf("partial PATCH status = %d", res.Code)
	}
	current := manager.Settings()
	if current.BioCheckEnabled || !current.CrossGroupManagement || !current.IsOwner(42) {
		t.Fatalf("partial update changed unrelated fields: %+v", current)
	}
}

func TestBotSettingsPanelRejectsBadInput(t *testing.T) {
	handler, manager := panelWithBotSettings(t)
	cases := []struct {
		name, body string
		status     int
	}{
		{"empty update", `{}`, http.StatusBadRequest},
		{"zero owner", `{"owner_user_ids":[0]}`, http.StatusBadRequest},
		{"negative owner", `{"owner_user_ids":[-5]}`, http.StatusBadRequest},
		{"unknown field", `{"bio_check":true}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := authedRequest(t, handler, http.MethodPatch, "/api/bot-settings", tc.body, true)
			if res.Code != tc.status {
				t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
			}
		})
	}
	if len(manager.Settings().OwnerUserIDs) != 0 || manager.Settings().BioCheckEnabled {
		t.Fatal("rejected input changed the settings")
	}
}

func TestBotSettingsRequireSessionAndCSRF(t *testing.T) {
	handler, _ := panelWithBotSettings(t)

	req := httptest.NewRequest(http.MethodGet, "/api/bot-settings", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d", res.Code)
	}

	res = authedRequest(t, handler, http.MethodPatch, "/api/bot-settings", `{"bio_check_enabled":true}`, false)
	if res.Code != http.StatusForbidden {
		t.Fatalf("PATCH without CSRF header status = %d", res.Code)
	}
}

func TestBotSettingsUnavailableWithoutManager(t *testing.T) {
	s, err := New(Options{
		Addr: "127.0.0.1:0", RuleStore: newFakeRuleStore(), ChatStore: &fakeChatStore{},
		AuditStore: &fakePanelAudit{}, Refresher: &fakeRefresher{}, SettingsStore: &fakePanelSettings{},
		Username: "admin", Password: "hunter2", SessionSecret: []byte("test-secret"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.securityHeaders(s.router)
	if res := authedRequest(t, handler, http.MethodGet, "/api/bot-settings", "", false); res.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET status = %d, want 503", res.Code)
	}
	if res := authedRequest(t, handler, http.MethodPatch, "/api/bot-settings", `{"bio_check_enabled":true}`, true); res.Code != http.StatusServiceUnavailable {
		t.Fatalf("PATCH status = %d, want 503", res.Code)
	}
}
