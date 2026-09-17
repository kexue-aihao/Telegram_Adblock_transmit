package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// minPanelPasswordLength is the minimum length enforced for passwords chosen
// from the settings page.
const minPanelPasswordLength = 8

type settingsAccountRequest struct {
	Username string `json:"username"`
}

type settingsPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleGetAccount reports the active panel username for the settings page.
func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, Username: s.auth.currentUsername()})
}

// handleUpdateAccount renames the panel login and keeps the current password
// hash unchanged, so the same password keeps working under the new username.
// The write is persisted to the database before the in-memory credentials are
// swapped, so a failed write cannot desynchronize the two.
func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req settingsAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(username) {
		writeError(w, http.StatusBadRequest, "用户名仅限字母、数字、_ . -（1-64 字符）。", "invalid_username")
		return
	}
	creds := domain.PanelCredentials{Username: username, PasswordHash: s.auth.passwordHashHex()}
	if err := s.options.SettingsStore.SavePanelSettings(r.Context(), creds); err != nil {
		s.internalError(w, "persist panel username failed", err)
		return
	}
	if err := s.auth.setUsername(username); err != nil {
		s.internalError(w, "apply panel username failed", err)
		return
	}
	s.logger.Info("panel username changed", "username", username)
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdatePassword verifies the current password, persists the new digest,
// updates the in-memory credentials and rotates the session key so every
// existing session (including the current one) must log in again.
func (s *Server) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	var req settingsPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.auth.passwordMatches(req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "当前密码不正确。", "invalid_current_password")
		return
	}
	newPassword := req.NewPassword
	if len(newPassword) < minPanelPasswordLength {
		writeError(w, http.StatusBadRequest, "新密码至少需要 8 个字符。", "weak_password")
		return
	}
	if len(newPassword) > 256 {
		writeError(w, http.StatusBadRequest, "新密码最长 256 个字符。", "invalid_new_password")
		return
	}
	hash := sha256.Sum256([]byte(newPassword))
	creds := domain.PanelCredentials{
		Username:     s.auth.currentUsername(),
		PasswordHash: hex.EncodeToString(hash[:]),
	}
	if err := s.options.SettingsStore.SavePanelSettings(r.Context(), creds); err != nil {
		s.internalError(w, "persist panel password failed", err)
		return
	}
	if err := s.auth.setPassword(newPassword); err != nil {
		s.internalError(w, "apply panel password failed", err)
		return
	}
	if err := s.auth.rotateSessionKey(); err != nil {
		s.internalError(w, "rotate session key failed", err)
		return
	}
	s.logger.Info("panel password changed")
	w.WriteHeader(http.StatusNoContent)
}
