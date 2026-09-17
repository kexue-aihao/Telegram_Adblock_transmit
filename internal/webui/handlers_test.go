package webui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
)

/* ── Fakes ────────────────────────────────────────────────────── */

type fakeRuleStore struct {
	byChat   map[int64][]domain.Rule
	nextID   int64
	removeOK bool
}

func newFakeRuleStore() *fakeRuleStore {
	return &fakeRuleStore{byChat: make(map[int64][]domain.Rule), nextID: 1}
}

func (f *fakeRuleStore) LoadEnabled(context.Context) (map[int64][]domain.Rule, error) {
	return nil, nil
}
func (f *fakeRuleStore) Add(_ context.Context, input domain.NewRule) (domain.Rule, error) {
	if _, err := rules.ValidatePattern(input.Pattern); err != nil {
		return domain.Rule{}, err
	}
	rule := domain.Rule{ID: f.nextID, ChatID: input.ChatID, Pattern: input.Pattern, Enabled: true, CreatedBy: input.CreatedBy, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.nextID++
	f.byChat[input.ChatID] = append(f.byChat[input.ChatID], rule)
	return rule, nil
}
func (f *fakeRuleStore) List(_ context.Context, chatID int64) ([]domain.Rule, error) {
	return append([]domain.Rule(nil), f.byChat[chatID]...), nil
}
func (f *fakeRuleStore) Remove(_ context.Context, chatID, ruleID int64) error {
	rules := f.byChat[chatID]
	for i, rule := range rules {
		if rule.ID == ruleID {
			f.byChat[chatID] = append(rules[:i], rules[i+1:]...)
			return nil
		}
	}
	if !f.removeOK {
		return store.ErrRuleNotFound
	}
	return nil
}
func (f *fakeRuleStore) SetEnabled(_ context.Context, chatID, ruleID int64, enabled bool) error {
	rules := f.byChat[chatID]
	for i := range rules {
		if rules[i].ID == ruleID {
			rules[i].Enabled = enabled
			rules[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return store.ErrRuleNotFound
}
func (f *fakeRuleStore) UpdatePattern(_ context.Context, chatID, ruleID int64, pattern string) (domain.Rule, error) {
	if _, err := rules.ValidatePattern(pattern); err != nil {
		return domain.Rule{}, err
	}
	for i := range f.byChat[chatID] {
		if f.byChat[chatID][i].ID == ruleID {
			f.byChat[chatID][i].Pattern = pattern
			f.byChat[chatID][i].UpdatedAt = time.Now()
			return f.byChat[chatID][i], nil
		}
	}
	return domain.Rule{}, store.ErrRuleNotFound
}

func (f *fakeRuleStore) exceedQuota(chatID int64) {
	// Pad an existing rule so the total-pattern quota would be exceeded.
	for i := range f.byChat[chatID] {
		f.byChat[chatID][i].Pattern = strings.Repeat("x", domain.MaxPatternLength)
	}
}

type fakeChatStore struct{ chats []domain.ChatSummary }

func (f *fakeChatStore) ListChats(context.Context) ([]domain.ChatSummary, error) {
	return append([]domain.ChatSummary(nil), f.chats...), nil
}

type fakePanelAudit struct {
	entries []domain.AuditEntry
}

func (f *fakePanelAudit) ListAudit(_ context.Context, q domain.AuditQuery) (domain.AuditPage, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	from := (page - 1) * pageSize
	if from > len(f.entries) {
		from = len(f.entries)
	}
	to := from + pageSize
	if to > len(f.entries) {
		to = len(f.entries)
	}
	items := f.entries[from:to]
	return domain.AuditPage{Items: items, Total: int64(len(f.entries)), Page: page, PageSize: pageSize}, nil
}
func (f *fakePanelAudit) GetAudit(_ context.Context, id int64) (domain.AuditEntry, error) {
	for _, entry := range f.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return domain.AuditEntry{}, store.ErrAuditNotFound
}
func (f *fakePanelAudit) StatsOverview(context.Context) (domain.AuditStatsOverview, error) {
	return domain.AuditStatsOverview{TotalChats: 3, TotalRules: 12, EnabledRules: 10, HitsToday: 5}, nil
}
func (f *fakePanelAudit) StatsByDay(_ context.Context, days int, _ *int64) ([]domain.AuditDayStat, error) {
	out := make([]domain.AuditDayStat, 0, days)
	today := time.Now().UTC()
	for i := days - 1; i >= 0; i-- {
		out = append(out, domain.AuditDayStat{Date: today.AddDate(0, 0, -i), Hits: int64(days - i)})
	}
	return out, nil
}

type fakeRefresher struct {
	refreshed []int64
	err       error
}

func (f *fakeRefresher) RefreshChatCache(_ context.Context, chatID int64) error {
	f.refreshed = append(f.refreshed, chatID)
	return f.err
}
func (f *fakeRefresher) LoadCache(context.Context) error { return f.err }

type fakePanelSettings struct {
	stored domain.PanelCredentials // zero value => ErrPanelSettingsNotFound
	err    error
}

func (f *fakePanelSettings) GetPanelSettings(context.Context) (domain.PanelCredentials, error) {
	if f.err != nil {
		return domain.PanelCredentials{}, f.err
	}
	if f.stored.Username == "" && f.stored.PasswordHash == "" {
		return domain.PanelCredentials{}, ports.ErrPanelSettingsNotFound
	}
	return f.stored, nil
}
func (f *fakePanelSettings) SavePanelSettings(_ context.Context, creds domain.PanelCredentials) error {
	if f.err != nil {
		return f.err
	}
	f.stored = creds
	return nil
}

/* ── Test harness ─────────────────────────────────────────────── */

func newTestPanel(t *testing.T, friction error) (*Server, *fakeRuleStore, *fakePanelAudit, *fakeRefresher, *fakePanelSettings, http.Handler) {
	t.Helper()
	ruleStore := newFakeRuleStore()
	chatStore := &fakeChatStore{chats: []domain.ChatSummary{{ID: 100, Title: "测试群"}}}
	audit := &fakePanelAudit{entries: []domain.AuditEntry{
		{ID: 1, ChatID: 100, MessageID: 7, MatchedRuleIDs: []int64{1}, ContentSummary: "广告", DeleteSucceeded: true, OccurredAt: time.Now()},
	}}
	refresher := &fakeRefresher{err: friction}
	settings := &fakePanelSettings{}
	s, err := New(Options{
		Addr: "127.0.0.1:0", RuleStore: ruleStore, ChatStore: chatStore,
		AuditStore: audit, Refresher: refresher, SettingsStore: settings,
		Username: "admin", Password: "hunter2", SessionSecret: []byte("test-secret"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.securityHeaders(s.router)
	return s, ruleStore, audit, refresher, settings, handler
}

func login(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	body := `{"username":"admin","password":"hunter2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func authedRequest(t *testing.T, handler http.Handler, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	cookie := login(t, handler)
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(cookie)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		req.Header.Set("X-Requested-With", "fetch")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

/* ── Auth flow tests ──────────────────────────────────────────── */

func TestHealthzIsPublic(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", rec.Code, rec.Body.String())
	}
}

func TestIndexIsPublic(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<!DOCTYPE html>") {
		t.Fatalf("index status = %d", rec.Code)
	}
}

func TestAPIRequiresSession(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	for _, path := range []string{"/api/dashboard/overview", "/api/chats", "/api/audit"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session = %d, want 401", path, rec.Code)
		}
	}
}

func TestSessionEndpoint(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("session = %d", rec.Code)
	}
	var body sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Authenticated {
		t.Fatal("session should be unauthenticated by default")
	}
}

func TestLoginRejected(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", rec.Code)
	}
}

func TestLoginRateLimited(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	for i := 0; i < loginMaxFailure; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("rate-limited before reaching budget (attempt %d)", i+1)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login after budget = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After")
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(login(t, handler))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	setCookies := rec.Result().Cookies()
	if !hasCookie(setCookies, sessionCookieName, -1) {
		t.Fatal("logout did not clear the session cookie")
	}
}

func hasCookie(cookies []*http.Cookie, name string, maxAge int) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge == maxAge {
			return true
		}
	}
	return false
}

/* ── CSRF ─────────────────────────────────────────────────────── */

func TestMutationsRequireCSRFHeader(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodDelete, "/api/chats/100/rules/1", "", false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mutation without CSRF header = %d, want 403", rec.Code)
	}
}

/* ── Rules API ────────────────────────────────────────────────── */

func TestRulesCRUD(t *testing.T) {
	s, ruleStore, _, refresher, _, handler := newTestPanel(t, nil)
	_ = s

	// List empty chat.
	rec := authedRequest(t, handler, http.MethodGet, "/api/chats/100/rules", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}

	// Add.
	rec = authedRequest(t, handler, http.MethodPost, "/api/chats/100/rules", `{"pattern":"免费.*领取"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created ruleDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 1 || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if created.CreatedBy != createdByPanel {
		t.Fatalf("created_by = %d, want %d", created.CreatedBy, createdByPanel)
	}
	if len(refresher.refreshed) != 1 || refresher.refreshed[0] != 100 {
		t.Fatalf("cache refresh calls = %v", refresher.refreshed)
	}

	// Invalid pattern → 400.
	rec = authedRequest(t, handler, http.MethodPost, "/api/chats/100/rules", `{"pattern":"(?!x)"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid pattern status = %d, want 400", rec.Code)
	}

	// Update pattern.
	rec = authedRequest(t, handler, http.MethodPut, "/api/chats/100/rules/1", `{"pattern":"广告.*加群"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rules := ruleStore.byChat[100]; rules[0].Pattern != "广告.*加群" {
		t.Fatalf("pattern not updated: %+v", rules[0])
	}

	// Toggle off.
	rec = authedRequest(t, handler, http.MethodPatch, "/api/chats/100/rules/1", `{"enabled":false}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d", rec.Code)
	}
	if rules := ruleStore.byChat[100]; rules[0].Enabled {
		t.Fatal("rule still enabled after PATCH")
	}

	// Delete.
	rec = authedRequest(t, handler, http.MethodDelete, "/api/chats/100/rules/1", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if len(ruleStore.byChat[100]) != 0 {
		t.Fatal("rule not removed")
	}

	// 404 for a missing rule.
	rec = authedRequest(t, handler, http.MethodDelete, "/api/chats/100/rules/9", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

func TestRuleTestEndpoint(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodPost, "/api/rules/test", `{"pattern":"免费.*领取","text":"本群免费领取大礼包"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("rule test status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result ruleTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Matched {
		t.Fatal("expected match")
	}
	rec = authedRequest(t, handler, http.MethodPost, "/api/rules/test", `{"pattern":"免费.*领取","text":"无关消息"}`, true)
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched {
		t.Fatal("expected no match")
	}
	rec = authedRequest(t, handler, http.MethodPost, "/api/rules/test", `{"pattern":"(?=x)","text":"x"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid pattern for test = %d, want 400", rec.Code)
	}
}

func TestRuleWriteWarningOnRefreshFailure(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, errors.New("boom"))
	rec := authedRequest(t, handler, http.MethodPost, "/api/chats/100/rules", `{"pattern":"免费.*领取"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add with refresh failure = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["warning"] != "cache_refresh_failed" {
		t.Fatalf("warning = %v, want cache_refresh_failed", body["warning"])
	}
}

/* ── Audit + dashboard ────────────────────────────────────────── */

func TestListAuditDefaultsAndPagination(t *testing.T) {
	_, _, audit, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodGet, "/api/audit", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit list = %d", rec.Code)
	}
	var page auditListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.PageSize != 20 || page.Total != int64(len(audit.entries)) {
		t.Fatalf("audit page defaults wrong: %+v", page)
	}
	if page.TotalPages != 1 {
		t.Fatalf("total_pages = %d", page.TotalPages)
	}
}

func TestGetAuditDetailAnd404(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodGet, "/api/audit/1", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit detail = %d", rec.Code)
	}
	rec = authedRequest(t, handler, http.MethodGet, "/api/audit/999", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing audit = %d, want 404", rec.Code)
	}
}

func TestDashboardOverviewAndTrend(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodGet, "/api/dashboard/overview", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview = %d", rec.Code)
	}
	var stats statsOverviewDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.TotalChats != 3 || stats.EnabledRules != 10 {
		t.Fatalf("overview = %+v", stats)
	}

	rec = authedRequest(t, handler, http.MethodGet, "/api/dashboard/trend?days=7", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("trend = %d", rec.Code)
	}
	var days []dayStatDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &days); err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 {
		t.Fatalf("trend length = %d, want 7", len(days))
	}
	if days[0].Date == "" {
		t.Fatal("trend date format missing")
	}

	rec = authedRequest(t, handler, http.MethodGet, "/api/dashboard/trend?days=0", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid days = %d, want 400", rec.Code)
	}
}

func TestCacheReloadEndpoint(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodPost, "/api/cache/reload", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cache reload = %d", rec.Code)
	}
}

func TestRealServerServesAssetsAndAuthenticates(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	server := httptest.NewServer(handler) // real listener: exercises the full chain
	defer server.Close()

	// Static assets load without auth.
	client := server.Client()
	res, err := client.Get(server.URL + "/assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") == "" {
		t.Fatalf("app.css = %d, content-type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	res, err = client.Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("app.js = %d", res.StatusCode)
	}

	// Login then hit a protected endpoint with the session cookie.
	loginReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/login",
		strings.NewReader(`{"username":"admin","password":"hunter2"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie over a real server")
	}

	chatsReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/chats", nil)
	chatsReq.Header.Set("X-Requested-With", "fetch")
	for _, cookie := range cookies {
		chatsReq.AddCookie(cookie)
	}
	chatsResp, err := client.Do(chatsReq)
	if err != nil {
		t.Fatal(err)
	}
	defer chatsResp.Body.Close()
	if chatsResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated /api/chats = %d", chatsResp.StatusCode)
	}
	if chatsResp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("api response missing Cache-Control no-store: %q", chatsResp.Header.Get("Cache-Control"))
	}
}

func TestUnknownAPIReturnsJSON404(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	cookie := login(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-Requested-With", "fetch")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api path = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unknown api path content type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestInvalidChatIDParam(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodGet, "/api/chats/not-a-number/rules", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid chat id = %d, want 400", rec.Code)
	}
}

var _ ports.RuleStore = (*fakeRuleStore)(nil)
var _ ports.ChatStore = (*fakeChatStore)(nil)
var _ ports.PanelAuditStore = (*fakePanelAudit)(nil)
var _ ports.RuleCacheRefresher = (*fakeRefresher)(nil)
