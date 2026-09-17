package webui

import (
	"net/http"
	"strconv"
)

// handleDashboardOverview returns the headline counters for the dashboard.
func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	stats, err := s.options.AuditStore.StatsOverview(r.Context())
	if err != nil {
		s.internalError(w, "查询统计失败", err)
		return
	}
	writeJSON(w, http.StatusOK, statsOverviewDTO{
		TotalChats: stats.TotalChats, TotalRules: stats.TotalRules, EnabledRules: stats.EnabledRules,
		HitsToday: stats.HitsToday, DeletedToday: stats.DeletedToday, FailedToday: stats.FailedToday,
		Hits7Day: stats.Hits7Day, TotalHits: stats.TotalHits,
	})
}

// handleDashboardTrend returns daily aggregates for the trend chart.
// Query params: days (1..90, default 30), chat_id (optional filter).
func (s *Server) handleDashboardTrend(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	days := 30
	if raw := query.Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 90 {
			writeError(w, http.StatusBadRequest, "days 无效（应为 1-90）。", "invalid_days")
			return
		}
		days = value
	}
	var chatID *int64
	if raw := query.Get("chat_id"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "chat_id 无效。", "invalid_chat_id")
			return
		}
		chatID = &value
	}
	stats, err := s.options.AuditStore.StatsByDay(r.Context(), days, chatID)
	if err != nil {
		s.internalError(w, "查询趋势统计失败", err)
		return
	}
	result := make([]dayStatDTO, 0, len(stats))
	for _, stat := range stats {
		result = append(result, dayStatDTO{
			Date: stat.Date.Format("2006-01-02"),
			Hits: stat.Hits, Deleted: stat.Deleted, Failed: stat.Failed,
		})
	}
	writeJSON(w, http.StatusOK, result)
}
