package webui

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

//go:embed assets
var assetsFS embed.FS

const maxJSONBodyBytes = 1 << 20 // 1 MiB

// Options wires the panel to its shared dependencies. The concrete stores and
// the moderation service are provided by the caller; the panel sees them only
// through small ports so handlers stay unit-testable with fakes.
type Options struct {
	Addr string // listen address, e.g. "0.0.0.0:8080" or "127.0.0.1:8080"

	RuleStore  ports.RuleStore
	ChatStore  ports.ChatStore
	AuditStore ports.PanelAuditStore
	Refresher  ports.RuleCacheRefresher // *moderation.Service; invalidates the in-process cache
	// SettingsStore persists username/password changes made from the settings
	// page. Credentials written there take precedence over the environment
	// values passed via Username/Password.
	SettingsStore ports.PanelSettingsStore

	Username      string
	Password      string
	SessionSecret []byte // optional; random per-process when nil

	Logger *slog.Logger
}

// Server owns the panel's HTTP router and shared services.
type Server struct {
	options Options
	auth    *authService
	logger  *slog.Logger
	index   []byte
	router  *http.ServeMux
}

// New validates dependencies and builds the router.
func New(o Options) (*Server, error) {
	if o.Addr == "" {
		return nil, errors.New("webui: addr must not be empty")
	}
	if o.RuleStore == nil || o.ChatStore == nil || o.AuditStore == nil || o.Refresher == nil {
		return nil, errors.New("webui: rule store, chat store, audit store and refresher are required")
	}
	if o.SettingsStore == nil {
		return nil, errors.New("webui: settings store is required")
	}
	if o.Username == "" || o.Password == "" {
		return nil, errors.New("webui: username and password are required")
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	auth, err := newAuthService(o.Username, o.Password, o.SessionSecret)
	if err != nil {
		return nil, err
	}
	if creds, loadErr := o.SettingsStore.GetPanelSettings(context.Background()); loadErr == nil {
		hashBytes, decodeErr := hex.DecodeString(creds.PasswordHash)
		if decodeErr == nil && len(hashBytes) == 32 && usernamePattern.MatchString(creds.Username) {
			var storedHash [32]byte
			copy(storedHash[:], hashBytes)
			auth, err = newAuthServiceFromHash(creds.Username, storedHash, o.SessionSecret)
			if err != nil {
				logger.Warn("stored panel credentials are invalid, using environment credentials", "error", err)
			} else {
				logger.Info("panel credentials loaded from database", "username", creds.Username)
			}
		} else {
			logger.Warn("stored panel credentials are malformed, using environment credentials")
		}
	} else if !errors.Is(loadErr, ports.ErrPanelSettingsNotFound) {
		logger.Warn("unable to read panel settings, using environment credentials", "error", loadErr)
	}
	index, err := fs.ReadFile(assetsFS, "assets/index.html")
	if err != nil {
		return nil, fmt.Errorf("webui: read embedded index.html: %w", err)
	}
	s := &Server{options: o, auth: auth, logger: logger, index: index}
	s.routes()
	return s, nil
}

// Run serves HTTP until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.options.Addr,
		Handler:           s.logRequests(s.securityHeaders(s.router)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func (s *Server) routes() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleIndex)
	assets, err := fs.Sub(assetsFS, "assets")
	if err == nil {
		mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	}
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)

	authed := func(h http.HandlerFunc) http.Handler { return s.authenticated(h) }
	authedCSRF := func(h http.HandlerFunc) http.Handler { return s.authenticated(s.withCSRF(h)) }

	mux.Handle("GET /api/dashboard/overview", authed(s.handleDashboardOverview))
	mux.Handle("GET /api/dashboard/trend", authed(s.handleDashboardTrend))

	mux.Handle("GET /api/chats", authed(s.handleListChats))
	// Rules are global: they live at /api/rules, shared by every group.
	// The old /api/chats/{chatID}/rules namespace was removed.
	mux.Handle("GET /api/rules", authed(s.handleListRules))
	mux.Handle("POST /api/rules", authedCSRF(s.handleAddRule))
	mux.Handle("PUT /api/rules/{ruleID}", authedCSRF(s.handleUpdatePattern))
	mux.Handle("PATCH /api/rules/{ruleID}", authedCSRF(s.handleSetEnabled))
	mux.Handle("DELETE /api/rules/{ruleID}", authedCSRF(s.handleRemoveRule))
	mux.Handle("GET /api/rules/export", authed(s.handleExportRules))
	mux.Handle("POST /api/rules/test", authedCSRF(s.handleRuleTest))

	mux.Handle("GET /api/audit", authed(s.handleListAudit))
	mux.Handle("GET /api/audit/{id}", authed(s.handleGetAudit))
	mux.Handle("POST /api/cache/reload", authedCSRF(s.handleCacheReload))

	mux.Handle("GET /api/settings/account", authed(s.handleGetAccount))
	mux.Handle("POST /api/settings/account", authedCSRF(s.handleUpdateAccount))
	mux.Handle("POST /api/settings/password", authedCSRF(s.handleUpdatePassword))

	// Fallback for unknown /api paths so API clients get a JSON 404 instead of
	// the SPA index. One pattern per method keeps them strictly more specific
	// than the "GET /" shell route.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		mux.HandleFunc(method+" /api/", s.handleAPINotFound)
	}

	s.router = mux
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "接口不存在。", "not_found")
}

// --- shared HTTP helpers ----------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	body := map[string]string{"error": message}
	if code != "" {
		body["code"] = code
	}
	writeJSON(w, status, body)
}

func writeWarning(w http.ResponseWriter, status int, value any, warning string) {
	wrapped := map[string]any{}
	if encoded, err := json.Marshal(value); err == nil {
		_ = json.Unmarshal(encoded, &wrapped)
	}
	if warning != "" {
		wrapped["warning"] = warning
	}
	writeJSON(w, status, wrapped)
}

// decodeJSON parses a JSON request body, capping its size. On failure it
// writes the error response and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "请求体不能为空。", "")
		} else {
			writeError(w, http.StatusBadRequest, "请求体格式无效。", "")
		}
		return false
	}
	return true
}

// pathInt64 reads an integer path parameter via r.PathValue.
func pathInt64(r *http.Request, name string) (int64, bool) {
	var value int64
	if _, err := fmt.Sscanf(r.PathValue(name), "%d", &value); err != nil {
		return 0, false
	}
	return value, true
}

// internalError logs a server-side failure once and emits a generic response.
func (s *Server) internalError(w http.ResponseWriter, message string, err error) {
	s.logger.Error(message, "error", err)
	writeError(w, http.StatusInternalServerError, "服务器内部错误。", "internal_error")
}

// refreshChat syncs the in-process rule cache after a write and returns a
// warning string (empty when fine). The DB write is already committed, so a
// cache failure must not turn into a hard error response.
func (s *Server) refreshChat(ctx context.Context, chatID int64) string {
	if err := s.options.Refresher.RefreshChatCache(ctx, chatID); err != nil {
		s.logger.Warn("rule cache refresh failed", "chat_id", chatID, "error", err)
		return "cache_refresh_failed"
	}
	return ""
}

// totalPages computes the pagination page count for a total and page size.
func totalPages(total int64, pageSize int) int {
	if total == 0 {
		return 0
	}
	if pageSize < 1 {
		return 1
	}
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if pages < 1 {
		pages = 1
	}
	return pages
}
