package webui

import (
	"net/http"
	"time"
)

// handleLogin validates credentials, enforces the per-IP failure budget, and
// issues the session cookie on success.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.auth.limiter.blocked(ip, time.Now()) {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试。", "rate_limited")
		return
	}
	if req.Username != s.auth.username || !s.auth.passwordMatches(req.Password) {
		time.Sleep(200 * time.Millisecond) // blunt the timing signal for username probing
		s.auth.limiter.recordFailure(ip, time.Now())
		writeError(w, http.StatusUnauthorized, "用户名或密码错误。", "invalid_credentials")
		return
	}
	s.auth.limiter.reset(ip)
	value, _ := s.auth.issueCookie(time.Now())
	http.SetCookie(w, s.sessionCookie(r, value, int(sessionTTL.Seconds())))
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, s.sessionCookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// handleSession reports the current authentication state without requiring a
// valid session, so the SPA can decide whether to show the login screen.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	username, ok := s.authenticate(r)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: ok, Username: username})
}

// handleIndex serves the embedded single-page application shell. Static
// assets and the shell are public so the login screen can load; they contain
// no data.
func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.index)
}

// handleHealthz is an unauthenticated probe for 1Panel and external health
// checks; the distroless image has no shell for in-container probes.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
