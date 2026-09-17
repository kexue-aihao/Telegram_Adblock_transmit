package webui

import (
	"errors"
	"net/http"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
)

// Panel-created rules use -1 as created_by. Telegram user IDs are positive and
// group IDs negative, so -1 is unambiguous and is documented in the README.
const createdByPanel int64 = -1

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats, err := s.options.ChatStore.ListChats(r.Context())
	if err != nil {
		s.internalError(w, "查询群组列表失败", err)
		return
	}
	writeJSON(w, http.StatusOK, toChatDTOs(chats))
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatID")
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的群组 ID。", "invalid_chat_id")
		return
	}
	rules, err := s.options.RuleStore.List(r.Context(), chatID)
	if err != nil {
		s.internalError(w, "查询规则失败", err)
		return
	}
	writeJSON(w, http.StatusOK, toRuleDTOs(rules))
}

func (s *Server) handleAddRule(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathInt64(r, "chatID")
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的群组 ID。", "invalid_chat_id")
		return
	}
	var req addRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rule, err := s.options.RuleStore.Add(r.Context(), domain.NewRule{
		ChatID: chatID, ChatTitle: req.ChatTitle, Pattern: req.Pattern, CreatedBy: createdByPanel,
	})
	if err != nil {
		s.mapRuleError(w, err)
		return
	}
	warning := s.refreshChat(r.Context(), chatID)
	writeWarning(w, http.StatusCreated, toRuleDTO(rule), warning)
}

func (s *Server) handleUpdatePattern(w http.ResponseWriter, r *http.Request) {
	chatID, ruleID, ok := rulePathIDs(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的群组或规则 ID。", "invalid_id")
		return
	}
	var req updatePatternRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rule, err := s.options.RuleStore.UpdatePattern(r.Context(), chatID, ruleID, req.Pattern)
	if err != nil {
		s.mapRuleError(w, err)
		return
	}
	warning := s.refreshChat(r.Context(), chatID)
	writeWarning(w, http.StatusOK, toRuleDTO(rule), warning)
}

func (s *Server) handleSetEnabled(w http.ResponseWriter, r *http.Request) {
	chatID, ruleID, ok := rulePathIDs(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的群组或规则 ID。", "invalid_id")
		return
	}
	var req setEnabledRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.options.RuleStore.SetEnabled(r.Context(), chatID, ruleID, req.Enabled); err != nil {
		s.mapRuleError(w, err)
		return
	}
	refreshed, err := s.options.RuleStore.List(r.Context(), chatID)
	if err != nil {
		s.internalError(w, "查询规则失败", err)
		return
	}
	warning := s.refreshChat(r.Context(), chatID)
	var updated domain.Rule
	for _, rule := range refreshed {
		if rule.ID == ruleID {
			updated = rule
			break
		}
	}
	writeWarning(w, http.StatusOK, toRuleDTO(updated), warning)
}

func (s *Server) handleRemoveRule(w http.ResponseWriter, r *http.Request) {
	chatID, ruleID, ok := rulePathIDs(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的群组或规则 ID。", "invalid_id")
		return
	}
	if err := s.options.RuleStore.Remove(r.Context(), chatID, ruleID); err != nil {
		s.mapRuleError(w, err)
		return
	}
	s.refreshChat(r.Context(), chatID) // cleanup best-effort; deletion is committed
	w.WriteHeader(http.StatusNoContent)
}

// handleRuleTest dry-runs a pattern against sample text without saving it.
func (s *Server) handleRuleTest(w http.ResponseWriter, r *http.Request) {
	var req ruleTestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	compiled, err := rules.CompilePattern(req.Pattern)
	if err != nil {
		var invalid *rules.InvalidPatternError
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, invalid.Reason, "invalid_pattern")
			return
		}
		s.internalError(w, "正则表达式测试失败", err)
		return
	}
	writeJSON(w, http.StatusOK, ruleTestResponse{Matched: compiled.MatchString(req.Text)})
}

// handleCacheReload forces a full reload of the in-process rule cache from the
// database. Useful after manual repairs performed directly on PostgreSQL.
func (s *Server) handleCacheReload(w http.ResponseWriter, r *http.Request) {
	if err := s.options.Refresher.LoadCache(r.Context()); err != nil {
		s.logger.Error("full rule cache reload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "规则缓存刷新失败。", "cache_reload_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func rulePathIDs(r *http.Request) (chatID, ruleID int64, ok bool) {
	chatID, ok = pathInt64(r, "chatID")
	if !ok {
		return 0, 0, false
	}
	ruleID, ok = pathInt64(r, "ruleID")
	if !ok {
		return 0, 0, false
	}
	return chatID, ruleID, true
}

// mapRuleError translates store and validation sentinels into HTTP responses.
func (s *Server) mapRuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrRuleNotFound):
		writeError(w, http.StatusNotFound, "规则不存在。", "rule_not_found")
	case errors.Is(err, store.ErrRuleLimitExceeded):
		writeError(w, http.StatusConflict, "该群组的规则数量或正则总长度已达上限。", "rule_limit_exceeded")
	default:
		var invalid *rules.InvalidPatternError
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, invalid.Reason, "invalid_pattern")
			return
		}
		s.internalError(w, "规则操作失败", err)
	}
}
