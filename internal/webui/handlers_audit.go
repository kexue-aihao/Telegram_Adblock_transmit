package webui

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
)

// handleListAudit returns a filtered, paginated page of audit entries.
// Query params: chat_id, from, to (YYYY-MM-DD, from inclusive / to exclusive,
// UTC), success (true|false), rule_id, page, page_size.
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := domain.AuditQuery{}

	if raw := query.Get("chat_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "chat_id 无效。", "invalid_chat_id")
			return
		}
		q.ChatID = &value
	}
	if raw := query.Get("from"); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from 格式无效（应为 YYYY-MM-DD）。", "invalid_from")
			return
		}
		q.From = &value
	}
	if raw := query.Get("to"); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to 格式无效（应为 YYYY-MM-DD）。", "invalid_to")
			return
		}
		value = value.AddDate(0, 0, 1) // exclusive end
		q.To = &value
	}
	if raw := query.Get("success"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "success 无效（应为 true 或 false）。", "invalid_success")
			return
		}
		q.Success = &value
	}
	if raw := query.Get("rule_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "rule_id 无效。", "invalid_rule_id")
			return
		}
		q.RuleID = &value
	}
	if raw := query.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeError(w, http.StatusBadRequest, "page 无效（应为不小于 1 的整数）。", "invalid_page")
			return
		}
		q.Page = value
	}
	if raw := query.Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "page_size 无效（应为 1-100）。", "invalid_page_size")
			return
		}
		q.PageSize = value
	}

	page, err := s.options.AuditStore.ListAudit(r.Context(), q)
	if err != nil {
		s.internalError(w, "查询审计日志失败", err)
		return
	}
	items := make([]auditEntryDTO, 0, len(page.Items))
	for _, entry := range page.Items {
		items = append(items, toAuditEntryDTO(entry))
	}
	writeJSON(w, http.StatusOK, auditListResponse{
		Items:      items,
		Total:      page.Total,
		Page:       page.Page,
		PageSize:   page.PageSize,
		TotalPages: totalPages(page.Total, page.PageSize),
	})
}

func (s *Server) handleGetAudit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "无效的审计记录 ID。", "invalid_id")
		return
	}
	entry, err := s.options.AuditStore.GetAudit(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAuditNotFound) {
			writeError(w, http.StatusNotFound, "审计记录不存在。", "audit_not_found")
			return
		}
		s.internalError(w, "查询审计记录失败", err)
		return
	}
	writeJSON(w, http.StatusOK, toAuditEntryDTO(entry))
}
