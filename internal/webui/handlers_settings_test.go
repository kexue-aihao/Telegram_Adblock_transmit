package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// rawAuthed performs a request with an explicitly supplied session cookie,
// unlike authedRequest which logs in first. Needed to verify that previously
// issued cookies stop working after a credential change.
func rawAuthed(handler http.Handler, cookie *http.Cookie, method, path, body string, csrf bool) *httptest.ResponseRecorder {
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

// tryLogin attempts a login with the given credentials and returns the session
// cookie (nil on failure) along with the HTTP status.
func tryLogin(handler http.Handler, username, password string) (*http.Cookie, int) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		return nil, rec.Code
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie, rec.Code
		}
	}
	return nil, rec.Code
}

func TestGetAccountEndpoint(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodGet, "/api/settings/account", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Username != "admin" {
		t.Fatalf("username = %q, want admin", body.Username)
	}
}

func TestUpdateAccountChangesUsernameAndPersistsHash(t *testing.T) {
	_, _, _, _, settings, handler := newTestPanel(t, nil)
	cookie := login(t, handler)
	rec := rawAuthed(handler, cookie, http.MethodPost, "/api/settings/account", `{"username":"owner"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if settings.stored.Username != "owner" {
		t.Fatalf("stored username = %q, want owner", settings.stored.Username)
	}
	origHash := sha256.Sum256([]byte("hunter2"))
	wantHash := hex.EncodeToString(origHash[:])
	if settings.stored.PasswordHash != wantHash {
		t.Fatalf("stored hash = %q, want original password hash preserved", settings.stored.PasswordHash)
	}
	// The old cookie carried the previous username, so it must be rejected now.
	after := rawAuthed(handler, cookie, http.MethodGet, "/api/chats", "", false)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("old cookie accepted after username change: status = %d", after.Code)
	}
	// The old password must still log the account in under the new username.
	cookie2, status := tryLogin(handler, "owner", "hunter2")
	if status != http.StatusNoContent || cookie2 == nil {
		t.Fatalf("login with renamed account failed: status = %d", status)
	}
}

func TestUpdateAccountRejectsInvalidUsername(t *testing.T) {
	_, _, _, _, settings, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodPost, "/api/settings/account", `{"username":"bad user"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if settings.stored.Username != "" {
		t.Fatal("invalid username was persisted")
	}
}

func TestUpdatePasswordRejectsWrongCurrent(t *testing.T) {
	_, _, _, _, settings, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodPost, "/api/settings/password",
		`{"current_password":"nope","new_password":"newpass12"}`, true)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if settings.stored.PasswordHash != "" {
		t.Fatal("password hash persisted despite wrong current password")
	}
}

func TestUpdatePasswordRejectsWeakNewPassword(t *testing.T) {
	_, _, _, _, _, handler := newTestPanel(t, nil)
	rec := authedRequest(t, handler, http.MethodPost, "/api/settings/password",
		`{"current_password":"hunter2","new_password":"short"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdatePasswordSignsOutAllSessions(t *testing.T) {
	_, _, _, _, settings, handler := newTestPanel(t, nil)
	cookie := login(t, handler)
	rec := rawAuthed(handler, cookie, http.MethodPost, "/api/settings/password",
		`{"current_password":"hunter2","new_password":"newpass12"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	newHash := sha256.Sum256([]byte("newpass12"))
	wantHash := hex.EncodeToString(newHash[:])
	if settings.stored.PasswordHash != wantHash {
		t.Fatalf("stored hash = %q, want digest of new password", settings.stored.PasswordHash)
	}
	// The pre-change cookie must be dead (session key rotated).
	after := rawAuthed(handler, cookie, http.MethodGet, "/api/chats", "", false)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("pre-change cookie accepted: status = %d", after.Code)
	}
	// Old password must fail, new password must succeed.
	if _, status := tryLogin(handler, "admin", "hunter2"); status != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", status)
	}
	if _, status := tryLogin(handler, "admin", "newpass12"); status != http.StatusNoContent {
		t.Fatalf("new password login = %d, want 204", status)
	}
}

func TestPanelPrefersStoredCredentialsOverEnv(t *testing.T) {
	hash := sha256.Sum256([]byte("ownerpass"))
	settings := &fakePanelSettings{stored: domain.PanelCredentials{
		Username:     "owner",
		PasswordHash: hex.EncodeToString(hash[:]),
	}}
	s, err := New(Options{
		Addr: "127.0.0.1:0", RuleStore: newFakeRuleStore(), ChatStore: &fakeChatStore{},
		AuditStore: &fakePanelAudit{}, Refresher: &fakeRefresher{}, SettingsStore: settings,
		Username: "admin", Password: "hunter2", SessionSecret: []byte("test-secret"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := s.securityHeaders(s.router)
	if _, status := tryLogin(handler, "admin", "hunter2"); status != http.StatusUnauthorized {
		t.Fatalf("environment credentials still accepted: %d", status)
	}
	if _, status := tryLogin(handler, "owner", "ownerpass"); status != http.StatusNoContent {
		t.Fatalf("database credentials rejected: %d", status)
	}
}
