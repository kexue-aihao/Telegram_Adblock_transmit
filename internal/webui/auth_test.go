package webui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	svc, err := newAuthService("admin", "hunter2", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	value, _ := svc.issueCookie(now)
	if got := svc.verifyToken(value, now.Add(time.Minute)); got != "admin" {
		t.Fatalf("verifyToken = %q, want admin", got)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	svc, _ := newAuthService("admin", "hunter2", []byte("secret"))
	now := time.Now()
	value, _ := svc.issueCookie(now)
	for i := range value {
		flipped := value[:i] + string(value[i]^0x01)
		if i+1 < len(value) {
			flipped += value[i+1:]
		}
		if got := svc.verifyToken(flipped, now.Add(time.Minute)); got != "" {
			t.Fatalf("tampered token at position %d accepted as %q", i, got)
		}
	}
}

func TestTokenRejectsDifferentKey(t *testing.T) {
	a, _ := newAuthService("admin", "hunter2", []byte("secret-one"))
	b, _ := newAuthService("admin", "hunter2", []byte("secret-two"))
	now := time.Now()
	value, _ := a.issueCookie(now)
	if got := b.verifyToken(value, now.Add(time.Minute)); got != "" {
		t.Fatalf("token from another key accepted as %q", got)
	}
}

func TestTokenRejectsExpired(t *testing.T) {
	svc, _ := newAuthService("admin", "hunter2", []byte("secret"))
	now := time.Now()
	value, _ := svc.issueCookie(now.Add(-25 * time.Hour))
	if got := svc.verifyToken(value, now); got != "" {
		t.Fatalf("expired token accepted as %q", got)
	}
	if got := svc.verifyToken(value, now.Add(30*time.Minute)); got != "" {
		t.Fatalf("token from a revoked window accepted as %q", got)
	}
}

func TestTokenRejectsGarbage(t *testing.T) {
	svc, _ := newAuthService("admin", "hunter2", []byte("secret"))
	now := time.Now()
	for _, value := range []string{"", "abc", "a.b.c", "!", base64.RawURLEncoding.EncodeToString([]byte("x|1"))} {
		if got := svc.verifyToken(value, now); got != "" {
			t.Fatalf("garbage token %q accepted as %q", value, got)
		}
	}
}

func TestTokenBindsUsername(t *testing.T) {
	// A token issued for one account must not verify as another, even if the
	// attacker re-signs it: the payload carries the username.
	issuer, _ := newAuthService("admin", "pw", []byte("secret"))
	forged := forgeToken(t, "other", "secret")
	if got := issuer.verifyToken(forged, time.Now().Add(time.Hour)); got != "" {
		t.Fatalf("token for a different username accepted as %q", got)
	}
}

func TestNeedsRotation(t *testing.T) {
	svc, _ := newAuthService("admin", "hunter2", []byte("secret"))
	now := time.Now()
	fresh, _ := svc.issueCookie(now)
	if svc.needsRotation(fresh, now) {
		t.Fatal("fresh token should not need rotation")
	}
	old, _ := svc.issueCookie(now.Add(-sessionTTL + 2*time.Hour))
	if !svc.needsRotation(old, now) {
		t.Fatal("nearly-expired token should need rotation")
	}
}

func TestPasswordMatches(t *testing.T) {
	svc, _ := newAuthService("admin", "hunter2", []byte("secret"))
	if !svc.passwordMatches("hunter2") {
		t.Fatal("correct password rejected")
	}
	if svc.passwordMatches("wrong") {
		t.Fatal("wrong password accepted")
	}
	empty, _ := newAuthService("admin", "pw", []byte("secret"))
	if empty.passwordMatches("") {
		t.Fatal("empty password accepted")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	ip := "10.0.0.1"
	for i := 0; i < loginMaxFailure; i++ {
		if l.blocked(ip, now) {
			t.Fatalf("blocked before reaching limit (attempt %d)", i+1)
		}
		l.recordFailure(ip, now)
	}
	if !l.blocked(ip, now) {
		t.Fatal("limiter did not block after exceeding the budget")
	}
	if l.blocked("other-ip", now) {
		t.Fatal("limiter blocked an unrelated IP")
	}
	if l.blocked(ip, now.Add(loginWindow+time.Minute)) {
		t.Fatal("limiter still blocked after the window expired")
	}
	l.recordFailure(ip, now)
	l.reset(ip)
	if l.blocked(ip, now) {
		t.Fatal("limiter still blocked after reset")
	}
}

func TestUsernameValidation(t *testing.T) {
	cases := []struct {
		username string
		ok       bool
	}{
		{"admin", true},
		{"admin_1.abc-def", true},
		{"", false},
		{"bad user", false},
		{strings.Repeat("a", 65), false},
	}
	for _, tc := range cases {
		_, err := newAuthService(tc.username, "pw", nil)
		if (err == nil) != tc.ok {
			t.Errorf("newAuthService(%q) error = %v, want ok=%v", tc.username, err, tc.ok)
		}
	}
}

func TestRuntimeCredentialUpdateAndKeyRotation(t *testing.T) {
	svc, err := newAuthService("admin", "hunter2", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	oldToken, _ := svc.issueCookie(now)

	// Rotating the session key invalidates every previously issued token.
	if err := svc.rotateSessionKey(); err != nil {
		t.Fatal(err)
	}
	if got := svc.verifyToken(oldToken, now.Add(time.Minute)); got != "" {
		t.Fatal("token survived session-key rotation")
	}

	// setPassword swaps the accepted password without touching the username.
	if err := svc.setPassword("newpass12"); err != nil {
		t.Fatal(err)
	}
	if svc.passwordMatches("hunter2") {
		t.Fatal("old password still accepted after change")
	}
	if !svc.passwordMatches("newpass12") {
		t.Fatal("new password rejected after change")
	}

	// setUsername swaps the account; the hash is preserved for persistence.
	if err := svc.setUsername("owner"); err != nil {
		t.Fatal(err)
	}
	if svc.currentUsername() != "owner" {
		t.Fatalf("currentUsername = %q, want owner", svc.currentUsername())
	}
	if got := svc.passwordHashHex(); len(got) != 64 {
		t.Fatalf("passwordHashHex = %q, want 64 hex chars", got)
	}
	fresh, _ := svc.issueCookie(now)
	if got := svc.verifyToken(fresh, now.Add(time.Minute)); got != "owner" {
		t.Fatalf("token for updated account not verifiable: %q", got)
	}
}

func TestRuntimeCredentialValidation(t *testing.T) {
	svc, err := newAuthService("admin", "pw", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.setUsername("bad user"); err == nil {
		t.Fatal("setUsername accepted an invalid username")
	}
	if err := svc.setPassword(""); err == nil {
		t.Fatal("setPassword accepted an empty password")
	}
	if err := svc.setPassword(strings.Repeat("x", 300)); err == nil {
		t.Fatal("setPassword accepted an over-long password")
	}
	if got := svc.currentUsername(); got != "admin" {
		t.Fatalf("invalid setUsername mutated state: %q", got)
	}
	if !svc.passwordMatches("pw") {
		t.Fatal("rejected credentials altered the valid password")
	}
}

// forgeToken builds a signed token for an arbitrary username with the given
// session secret, mirroring the production payload layout.
func forgeToken(t *testing.T, username, secret string) string {
	t.Helper()
	now := time.Now().Add(2 * time.Hour)
	if now.IsZero() {
		t.Fatal("unreachable")
	}
	payload := username + "|" + strconv.FormatInt(now.Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, sha256Sum([]byte(secret)))
	_, _ = mac.Write([]byte(payload))
	return encoded + "." + hex.EncodeToString(mac.Sum(nil))
}

func sha256Sum(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}
