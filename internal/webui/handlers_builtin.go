package webui

import (
	"errors"
	"net/http"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
)

func (s *Server) handleGetBuiltinRules(w http.ResponseWriter, r *http.Request) {
	if s.options.BuiltinFilter == nil {
		writeError(w, http.StatusServiceUnavailable, "内置广告库尚未配置。", "builtin_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.options.BuiltinFilter.Status())
}

func (s *Server) handleUpdateBuiltinRules(w http.ResponseWriter, r *http.Request) {
	if s.options.BuiltinFilter == nil {
		writeError(w, http.StatusServiceUnavailable, "内置广告库尚未配置。", "builtin_unavailable")
		return
	}
	var req struct {
		Enabled *bool            `json:"enabled"`
		Rules   map[string]*bool `json:"rules"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil && len(req.Rules) == 0 {
		writeError(w, http.StatusBadRequest, "请提供总开关或检测项状态。", "empty_update")
		return
	}
	rules := make(map[string]bool, len(req.Rules))
	for id, enabled := range req.Rules {
		if enabled == nil {
			writeError(w, http.StatusBadRequest, "检测项状态必须为 true 或 false。", "invalid_enabled")
			return
		}
		rules[id] = *enabled
	}
	result, err := s.options.BuiltinFilter.Update(r.Context(), req.Enabled, rules)
	if errors.Is(err, builtin.ErrUnknownRule) {
		writeError(w, http.StatusBadRequest, "检测项 ID 不存在。请刷新列表后重试。", "unknown_builtin_rule")
		return
	}
	if err != nil {
		s.internalError(w, "save builtin settings failed", err)
		return
	}
	s.logger.Info("builtin settings changed", "enabled", result.Enabled, "rule_updates", rules)
	writeJSON(w, http.StatusOK, result)
}
