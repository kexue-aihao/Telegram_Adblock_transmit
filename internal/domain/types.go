package domain

import "time"

const (
	MaxPatternLength      = 512
	MaxRulesPerChat       = 100
	MaxPatternTotalLength = 32768
	AuditSummaryLimit     = 120
	AuditRetentionDays    = 30
)

type Rule struct {
	ID        int64
	ChatID    int64
	Pattern   string
	Enabled   bool
	CreatedBy int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NewRule struct {
	ChatID    int64
	ChatTitle string
	Pattern   string
	CreatedBy int64
}

type CompiledRule struct {
	Rule
	PatternMatcher interface{ MatchString(string) bool }
}

type AuditEntry struct {
	ID              int64
	ChatID          int64
	ChatTitle       string
	MessageThreadID *int
	UserID          *int64
	MessageID       int
	MatchedRuleIDs  []int64
	ContentSHA256   string
	ContentSummary  string
	DeleteSucceeded bool
	DeletionError   string
	OccurredAt      time.Time
}

type NewAuditEntry struct {
	ChatID          int64
	ChatTitle       string
	MessageThreadID *int
	UserID          *int64
	MessageID       int
	MatchedRuleIDs  []int64
	Content         string
	DeleteSucceeded bool
	DeletionError   string
}

type ModerationMessage struct {
	ChatID          int64
	ChatTitle       string
	ChatType        string
	MessageID       int
	MessageThreadID *int
	UserID          *int64
	UserIsBot       bool
	Text            string
	Caption         string
}

func (m ModerationMessage) Content() string {
	if m.Text != "" {
		return m.Text
	}
	return m.Caption
}
