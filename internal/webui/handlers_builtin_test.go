package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type fakeBuiltinSettings struct {
	settings *domain.BuiltinSettings
	err      error
	saves    int
}

func (f *fakeBuiltinSettings) GetBuiltinSettings(context.Context) (domain.BuiltinSettings, error) {
	if f.settings == nil {
		return domain.BuiltinSettings{}, ports.ErrBuiltinSettingsNotFound
	}
	return *f.settings, nil
}
func (f *fakeBuiltinSettings) SaveBuiltinSettings(_ context.Context, settings domain.BuiltinSettings) error {
	f.saves++
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
	if res.Code != 200 || !status.Enabled || status.LibraryVersion == "" || len(status.Rules) != len(builtin.Catalog()) {
		t.Fatalf("catalog: %s", res.Body)
	}
	for _, rule := range status.Rules {
		if !rule.Enabled || !rule.Effective || rule.Description == "" || rule.Category == "" || len(rule.Conditions) == 0 {
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

func TestBuiltinTextTestUsesCurrentSettingsWithoutWrites(t *testing.T) {
	s, rules, audit, refresher, _, handler := newTestPanel(t, nil)
	store := &fakeBuiltinSettings{}
	s.options.BuiltinFilter = newTestBuiltin(t, store)
	request := func(text string) builtin.Analysis {
		t.Helper()
		body, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			t.Fatal(err)
		}
		res := authedRequest(t, handler, http.MethodPost, "/api/builtin-rules/test", string(body), true)
		var result builtin.Analysis
		if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &result) != nil {
			t.Fatalf("test response: %d %s", res.Code, res.Body)
		}
		if result.LibraryVersion == "" {
			t.Fatal("test response omitted library version")
		}
		return result
	}
	initialRules, initialAudit := len(rules.rules), len(audit.entries)
	text := "承接洗资业务，联系 @example_agent"
	result := request(text)
	if !result.Enabled || !result.Matched || !slices.ContainsFunc(result.Hits, func(hit domain.BuiltinHit) bool {
		return hit.ID == "ad_money_laundering" && hit.Name != "" && hit.Category != "" && len(hit.Evidence) > 0
	}) {
		t.Fatalf("missing categorized detection: %+v", result)
	}
	if clean := request("警方提醒防范洗钱风险，医学文章介绍药物安全。"); clean.Matched || len(clean.Hits) != 0 {
		t.Fatalf("benign discussion matched: %+v", clean)
	}
	if store.saves != 0 || len(rules.rules) != initialRules || len(audit.entries) != initialAudit || len(refresher.refreshed) != 0 {
		t.Fatal("dry run changed settings, rules, audit or cache")
	}
	// Disable every item that participated in this sample; unrelated detectors
	// remain enabled and may still analyze subsequent messages.
	updates := make(map[string]bool)
	for _, hit := range result.Hits {
		updates[hit.ID] = false
	}
	if _, err := s.options.BuiltinFilter.Update(context.Background(), nil, updates); err != nil {
		t.Fatal(err)
	}
	if off := request(text); !off.Enabled || off.Matched || len(off.Hits) != 0 {
		t.Fatalf("disabled detectors participated in dry run: %+v", off)
	}
	off := false
	if _, err := s.options.BuiltinFilter.Update(context.Background(), &off, nil); err != nil {
		t.Fatal(err)
	}
	if result = request(text); result.Enabled || result.Matched || len(result.Hits) != 0 {
		t.Fatalf("master off was not represented: %+v", result)
	}
	if store.saves != 2 || len(audit.entries) != initialAudit {
		t.Fatal("testing saved configuration or created moderation audit entries")
	}
}

func TestBuiltinTextTestValidationAndAuth(t *testing.T) {
	s, _, _, _, _, handler := newTestPanel(t, nil)
	for _, body := range []string{"", `{}`, `null`, `{"text":null}`, `{"text":""}`, `{"text":" \t\n　"}`,
		`{"text":42}`, `{"text":"hello","entities":[]}`, `{"pattern":"x","text":"x"}`} {
		t.Run(body, func(t *testing.T) {
			res := authedRequest(t, handler, http.MethodPost, "/api/builtin-rules/test", body, true)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("invalid test body accepted: %d %s", res.Code, res.Body)
			}
		})
	}
	for _, length := range []int{maxBuiltinTestRunes, maxBuiltinTestRunes + 1} {
		body, err := json.Marshal(map[string]string{"text": strings.Repeat("😀", length)})
		if err != nil {
			t.Fatal(err)
		}
		res := authedRequest(t, handler, http.MethodPost, "/api/builtin-rules/test", string(body), true)
		want := http.StatusOK
		if length > maxBuiltinTestRunes {
			want = http.StatusBadRequest
		}
		if res.Code != want {
			t.Fatalf("Unicode length %d: %d %s", length, res.Code, res.Body)
		}
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/builtin-rules/test", strings.NewReader(`{"text":"hello"}`)))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated test: %d", res.Code)
	}
	res = authedRequest(t, handler, http.MethodPost, "/api/builtin-rules/test", `{"text":"hello"}`, false)
	if res.Code != http.StatusForbidden {
		t.Fatalf("test without CSRF header: %d", res.Code)
	}
	s.options.BuiltinFilter = nil
	res = authedRequest(t, handler, http.MethodPost, "/api/builtin-rules/test", `{"text":"hello"}`, true)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "builtin_unavailable") {
		t.Fatalf("unavailable filter: %d %s", res.Code, res.Body)
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
