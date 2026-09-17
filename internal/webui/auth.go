package webui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "webui_session"
	sessionTTL        = 24 * time.Hour
	sessionRotateAge  = 12 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// authService issues and verifies stateless signed session cookies and guards
// the login endpoint against brute force. The username, password hash and
// session key are mutable so the settings page can change credentials at
// runtime; the mutex keeps concurrent logins and token checks consistent.
type authService struct {
	mu           sync.RWMutex
	username     string
	passwordHash [32]byte
	sessionKey   [32]byte
	limiter      *loginLimiter
}

// newAuthServiceFromHash builds an auth service around an already-computed
// password digest, used both for env-provided and database-stored
// credentials.
func newAuthServiceFromHash(username string, passwordHash [32]byte, secret []byte) (*authService, error) {
	if !usernamePattern.MatchString(username) {
		return nil, fmt.Errorf("webui: username may only contain A-Z a-z 0-9 _ . - (1-64 characters)")
	}
	var key [32]byte
	if len(secret) == 0 {
		if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
			return nil, fmt.Errorf("webui: generate session key: %w", err)
		}
	} else {
		key = sha256.Sum256(secret)
	}
	return &authService{
		username:     username,
		passwordHash: passwordHash,
		sessionKey:   key,
		limiter:      newLoginLimiter(),
	}, nil
}

func newAuthService(username, password string, secret []byte) (*authService, error) {
	if !usernamePattern.MatchString(username) {
		return nil, fmt.Errorf("webui: username may only contain A-Z a-z 0-9 _ . - (1-64 characters)")
	}
	if password == "" {
		return nil, fmt.Errorf("webui: password must not be empty")
	}
	return newAuthServiceFromHash(username, sha256.Sum256([]byte(password)), secret)
}

// issueCookie returns the signed token value and its expiry.
func (a *authService) issueCookie(now time.Time) (string, time.Time) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	expiry := now.Add(sessionTTL)
	payload := a.username + "|" + strconv.FormatInt(expiry.Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, a.sessionKey[:])
	_, _ = mac.Write([]byte(payload))
	return encoded + "." + hex.EncodeToString(mac.Sum(nil)), expiry
}

// verifyToken returns the authenticated username, or empty when the value is
// malformed, tampered, expired, or for a different account.
func (a *authService) verifyToken(value string, now time.Time) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	dot := strings.IndexByte(value, '.')
	if dot <= 0 || dot == len(value)-1 {
		return ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(value[:dot])
	if err != nil {
		return ""
	}
	payload := string(payloadBytes)
	mac := hmac.New(sha256.New, a.sessionKey[:])
	_, _ = mac.Write([]byte(payload))
	expect := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(value[dot+1:]), []byte(expect)) {
		return ""
	}
	fields := strings.SplitN(payload, "|", 2)
	if len(fields) != 2 || fields[0] != a.username {
		return ""
	}
	expiry, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || now.Unix() >= expiry {
		return ""
	}
	return a.username
}

// needsRotation reports whether a verified token is old enough to be re-issued
// with a fresh expiry, sliding the session forward.
func (a *authService) needsRotation(value string, now time.Time) bool {
	payloadBytes, err := base64.RawURLEncoding.DecodeString(value[:strings.IndexByte(value, '.')])
	if err != nil {
		return false
	}
	fields := strings.SplitN(string(payloadBytes), "|", 2)
	if len(fields) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return false
	}
	return expiry-now.Unix() <= int64(sessionRotateAge/time.Second)
}

// loginLimiter throttles failed logins per client IP within a sliding window.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]*attempt
}

type attempt struct {
	count     int
	windowEnd time.Time
}

const (
	loginWindow     = 15 * time.Minute
	loginMaxFailure = 5
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string]*attempt)}
}

// blocked reports whether the IP has exceeded the failure budget.
func (l *loginLimiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.failures[ip]
	if !ok {
		return false
	}
	if now.After(a.windowEnd) {
		delete(l.failures, ip)
		return false
	}
	return a.count >= loginMaxFailure
}

// recordFailure notes one failed attempt for ip.
func (l *loginLimiter) recordFailure(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.failures[ip]
	if !ok || now.After(a.windowEnd) {
		l.failures[ip] = &attempt{count: 1, windowEnd: now.Add(loginWindow)}
		return
	}
	a.count++
}

// reset clear failures for ip after a successful login.
func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

// clientIP resolves the origin IP from the first X-Forwarded-For value (set by
// the 1Panel reverse proxy) and falls back to the direct connection address.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, ok := strings.Cut(forwarded, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authenticate extracts and verifies the session cookie, returning the
// username on success.
func (s *Server) authenticate(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	username := s.auth.verifyToken(cookie.Value, time.Now())
	if username == "" {
		return "", false
	}
	return username, true
}

// sessionCookie builds the session cookie for a value. Secure is set when the
// request arrives over TLS directly or through the reverse proxy.
func (s *Server) sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   maxAge,
	}
}

// passwordMatches compares a provided password against the stored hash in
// constant time.
func (a *authService) passwordMatches(provided string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(providedHash[:], a.passwordHash[:]) == 1
}

// setUsername validates and atomically swaps the login username. The password
// hash is untouched. Tokens issued for the old username are rejected by
// verifyToken, so the current session's next request is redirected to login.
func (a *authService) setUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("webui: username may only contain A-Z a-z 0-9 _ . - (1-64 characters)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.username = username
	return nil
}

// setPassword validates and swaps the stored password hash. The session key is
// not rotated here; callers should call rotateSessionKey after a password
// change so every existing session is signed out.
func (a *authService) setPassword(password string) error {
	if password == "" {
		return fmt.Errorf("webui: password must not be empty")
	}
	if len(password) > 256 {
		return fmt.Errorf("webui: password is too long (max 256 characters)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passwordHash = sha256.Sum256([]byte(password))
	return nil
}

// rotateSessionKey replaces the cookie signing key, invalidating every issued
// session.
func (a *authService) rotateSessionKey() error {
	var key [32]byte
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		return fmt.Errorf("webui: generate session key: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionKey = key
	return nil
}

// currentUsername returns the active login username for responses.
func (a *authService) currentUsername() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.username
}

// passwordHashHex returns the active credential digest for persistence.
func (a *authService) passwordHashHex() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return hex.EncodeToString(a.passwordHash[:])
}
