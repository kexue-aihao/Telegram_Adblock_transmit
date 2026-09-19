package webui

import (
	"errors"
	"net/http"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
)

// botSettingsResponse is the JSON shape shared by GET and PATCH. Owner IDs are
// Telegram user IDs, never credentials.
type botSettingsResponse struct {
	BioCheckEnabled      bool    `json:"bio_check_enabled"`
	CrossGroupManagement bool    `json:"cross_group_management"`
	OwnerUserIDs         []int64 `json:"owner_user_ids"`
}

func newBotSettingsResponse(current domain.BotSettings) botSettingsResponse {
	owners := current.OwnerUserIDs
	if owners == nil {
		owners = []int64{}
	}
	return botSettingsResponse{
		BioCheckEnabled:      current.BioCheckEnabled,
		CrossGroupManagement: current.CrossGroupManagement,
		OwnerUserIDs:         owners,
	}
}

func (s *Server) handleGetBotSettings(w http.ResponseWriter, _ *http.Request) {
	if s.options.BotSettings == nil {
		writeError(w, http.StatusServiceUnavailable, "运行设置尚未配置。", "bot_settings_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, newBotSettingsResponse(s.options.BotSettings.Settings()))
}

// handleUpdateBotSettings applies a partial update. Settings are global, so a
// failed write leaves the running configuration untouched.
func (s *Server) handleUpdateBotSettings(w http.ResponseWriter, r *http.Request) {
	if s.options.BotSettings == nil {
		writeError(w, http.StatusServiceUnavailable, "运行设置尚未配置。", "bot_settings_unavailable")
		return
	}
	var req struct {
		BioCheckEnabled      *bool    `json:"bio_check_enabled"`
		CrossGroupManagement *bool    `json:"cross_group_management"`
		OwnerUserIDs         *[]int64 `json:"owner_user_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BioCheckEnabled == nil && req.CrossGroupManagement == nil && req.OwnerUserIDs == nil {
		writeError(w, http.StatusBadRequest, "请提供需要修改的设置项。", "empty_update")
		return
	}
	updated, err := s.options.BotSettings.Update(r.Context(), domain.BotSettingsPatch{
		BioCheckEnabled:      req.BioCheckEnabled,
		CrossGroupManagement: req.CrossGroupManagement,
		OwnerUserIDs:         req.OwnerUserIDs,
	})
	if errors.Is(err, settings.ErrInvalidSettings) {
		writeError(w, http.StatusBadRequest, "所有者必须是正整数 Telegram 用户 ID，且不超过 50 个。", "invalid_bot_settings")
		return
	}
	if err != nil {
		s.internalError(w, "save bot settings failed", err)
		return
	}
	s.logger.Info("bot settings changed", "bio_check", updated.BioCheckEnabled,
		"cross_group_management", updated.CrossGroupManagement, "owners", len(updated.OwnerUserIDs))
	writeJSON(w, http.StatusOK, newBotSettingsResponse(updated))
}
