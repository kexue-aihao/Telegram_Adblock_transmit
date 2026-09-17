package webui

import (
	"net/http"
	"strings"
	"time"
)

// statusWriter captures the response status so the access log can report it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

// logRequests emits one structured log line per request. Bodies are never
// read here, so credentials never reach the logs.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		s.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start).Round(time.Microsecond),
		)
	})
}

// securityHeaders applies hardening headers to every response. Cache-Control
// is set to no-store for everything (including the SPA shell and static
// assets) so a panel upgrade can never serve a stale index/app.js from a
// browser or proxy cache while the backend has moved on.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// authenticated requires a valid session cookie for /api routes other than
// the public ones (login/logout/session are exempted at the route layer).
func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authenticate(r); !ok {
			writeError(w, http.StatusUnauthorized, "未登录或会话已过期。", "unauthorized")
			return
		}
		if s.auth.needsRotation(mustCookieValue(r), time.Now()) {
			// Slide the session forward with a fresh expiry.
			if value, _ := s.auth.issueCookie(time.Now()); value != "" {
				http.SetCookie(w, s.sessionCookie(r, value, int(sessionTTL.Seconds())))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func mustCookieValue(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// withCSRF rejects state-changing requests that lack the custom header the
// SPA sends on every mutation. SameSite=Lax already stops cross-site cookies
// from being attached to POSTs; this header check is defense in depth.
func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("X-Requested-With"), "fetch") {
			writeError(w, http.StatusForbidden, "请求被拒绝（缺少跨站请求防护头）。", "csrf_rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}
