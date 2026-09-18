package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type fakeBuiltinSettings struct {
	settings *domain.BuiltinSettings
	err      error
}

func (f *fakeBuiltinSettings) GetBuiltinSettings(context.Context) (domain.BuiltinSettings, error) {
	if f.settings == nil {
		return domain.BuiltinSettings{}, ports.ErrBuiltinSettingsNotFound
	}
	return *f.settings, nil
}
func (f *fakeBuiltinSettings) SaveBuiltinSettings(_ context.Context, settings domain.BuiltinSettings) error {
	if f.err != nil {
		return f.err
	}
	settings.DisabledRules = slices.Clone(settings.DisabledRules)
	f.settings = &settings
	return nil
}
func newTestBuiltin(t *testing.T, store *fakeBuiltinSettings) *builtin.Checker {
	t.Helper()
	checker, err := builtin.NewManaged(context.Background(), true, store)
	if err != nil {
		t.Fatal(err)
	}
	return checker
}

func TestBuiltinManagementAPI(t *testing.T) {
	s, _, _, _, _, handler := newTestPanel(t, nil)
	store := &fakeBuiltinSettings{}
	s.options.BuiltinFilter = newTestBuiltin(t, store)
	res := authedRequest(t, handler, "GET", "/api/builtin-rules", "", true)
	var status builtin.Status
	if err := json.Unmarshal(res.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || !status.Enabled || len(status.Rules) != 6 {
		t.Fatalf("catalog: %s", res.Body)
	}
	for _, rule := range status.Rules {
		if !rule.Enabled || !rule.Effective || rule.Description == "" {
			t.Fatalf("incomplete catalog: %+v", rule)
		}
	}
	res = authedRequest(t, handler, "PATCH", "/api/builtin-rules", `{"rules":{"ad_bot_mention":false}}`, true)
	if res.Code != 200 || !slices.Contains(store.settings.DisabledRules, builtin.HitBotMention) {
		t.Fatalf("patch: %d %s", res.Code, res.Body)
	}
	res = authedRequest(t, handler, "PATCH", "/api/builtin-rules", `{"enabled":false}`, true)
	if err := json.Unmarshal(res.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Enabled || s.options.BuiltinFilter.Enabled() {
		t.Fatal("master switch did not affect shared checker")
	}
	for _, rule := range status.Rules {
		if rule.Effective {
			t.Fatalf("master off but rule effective: %+v", rule)
		}
	}
	store.err = errors.New("database unavailable")
	res = authedRequest(t, handler, "PATCH", "/api/builtin-rules", `{"enabled":true}`, true)
	if res.Code != 500 || s.options.BuiltinFilter.Enabled() {
		t.Fatalf("failed save changed runtime: %d", res.Code)
	}
}

func TestBuiltinManagementValidationAndAuth(t *testing.T) {
	for _, body := range []string{`{}`, `{"rules":{"unknown":false}}`, `{"rules":{"ad_bot_mention":null}}`, `{"enabled":"false"}`, `{"pattern":"anything"}`} {
		t.Run(body, func(t *testing.T) {
			_, _, _, _, _, handler := newTestPanel(t, nil)
			res := authedRequest(t, handler, "PATCH", "/api/builtin-rules", body, true)
			if res.Code != 400 {
				t.Fatalf("invalid patch: %d %s", res.Code, res.Body)
			}
		})
	}
	_, _, _, _, _, handler := newTestPanel(t, nil)
	for _, method := range []string{"GET", "PATCH"} {
		req := httptest.NewRequest(method, "/api/builtin-rules", nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d", method, res.Code)
		}
	}
	res := authedRequest(t, handler, "PATCH", "/api/builtin-rules", `{"enabled":false}`, false)
	if res.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF accepted: %d", res.Code)
	}
}
