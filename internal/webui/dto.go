package webui

import (
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// loginRequest is the body of POST /api/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// sessionResponse reports the current authentication state.
type sessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

type ruleDTO struct {
	ID        int64     `json:"id"`
	ChatID    int64     `json:"chat_id"`
	Pattern   string    `json:"pattern"`
	Enabled   bool      `json:"enabled"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toRuleDTO(rule domain.Rule) ruleDTO {
	return ruleDTO{
		ID: rule.ID, ChatID: rule.ChatID, Pattern: rule.Pattern, Enabled: rule.Enabled,
		CreatedBy: rule.CreatedBy, CreatedAt: rule.CreatedAt.UTC(), UpdatedAt: rule.UpdatedAt.UTC(),
	}
}

func toRuleDTOs(rules []domain.Rule) []ruleDTO {
	result := make([]ruleDTO, 0, len(rules))
	for _, rule := range rules {
		result = append(result, toRuleDTO(rule))
	}
	return result
}

type chatDTO struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	RuleCount    int    `json:"rule_count"`
	EnabledCount int    `json:"enabled_count"`
}

func toChatDTOs(chats []domain.ChatSummary) []chatDTO {
	result := make([]chatDTO, 0, len(chats))
	for _, chat := range chats {
		result = append(result, chatDTO{
			ID: chat.ID, Title: chat.Title,
			RuleCount: chat.RuleCount, EnabledCount: chat.EnabledCount,
		})
	}
	return result
}

// addRuleRequest is the body of POST /api/chats/{chatID}/rules.
type addRuleRequest struct {
	Pattern   string `json:"pattern"`
	ChatTitle string `json:"chat_title,omitempty"`
}

// updatePatternRequest is the body of PUT /api/chats/{chatID}/rules/{ruleID}.
type updatePatternRequest struct {
	Pattern string `json:"pattern"`
}

// setEnabledRequest is the body of PATCH /api/chats/{chatID}/rules/{ruleID}.
type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// ruleTestRequest exercises one pattern against a sample before saving.
type ruleTestRequest struct {
	Pattern string `json:"pattern"`
	Text    string `json:"text"`
}

type ruleTestResponse struct {
	Matched bool `json:"matched"`
}

type auditEntryDTO struct {
	ID              int64     `json:"id"`
	ChatID          int64     `json:"chat_id"`
	MessageThreadID *int      `json:"message_thread_id,omitempty"`
	UserID          *int64    `json:"user_id,omitempty"`
	MessageID       int       `json:"message_id"`
	MatchedRuleIDs  []int64   `json:"matched_rule_ids"`
	ContentSummary  string    `json:"content_summary"`
	DeleteSucceeded bool      `json:"delete_succeeded"`
	DeletionError   string    `json:"deletion_error,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

func toAuditEntryDTO(entry domain.AuditEntry) auditEntryDTO {
	return auditEntryDTO{
		ID: entry.ID, ChatID: entry.ChatID, MessageThreadID: entry.MessageThreadID,
		UserID: entry.UserID, MessageID: entry.MessageID, MatchedRuleIDs: entry.MatchedRuleIDs,
		ContentSummary: entry.ContentSummary, DeleteSucceeded: entry.DeleteSucceeded,
		DeletionError: entry.DeletionError, OccurredAt: entry.OccurredAt.UTC(),
	}
}

type auditListResponse struct {
	Items      []auditEntryDTO `json:"items"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}

type statsOverviewDTO struct {
	TotalChats   int64 `json:"total_chats"`
	TotalRules   int64 `json:"total_rules"`
	EnabledRules int64 `json:"enabled_rules"`
	HitsToday    int64 `json:"hits_today"`
	DeletedToday int64 `json:"deleted_today"`
	FailedToday  int64 `json:"failed_today"`
	Hits7Day     int64 `json:"hits_7day"`
	TotalHits    int64 `json:"total_hits"`
}

type dayStatDTO struct {
	Date    string `json:"date"`
	Hits    int64  `json:"hits"`
	Deleted int64  `json:"deleted"`
	Failed  int64  `json:"failed"`
}
