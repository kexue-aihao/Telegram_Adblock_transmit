package domain

import "time"

// ChatSummary is the per-chat inventory row shown in the WebUI panel's chat
// pickers and dashboard. A chat appears once a chat_groups row exists, which
// happens lazily on the first rule add or first audited hit.
type ChatSummary struct {
	ID           int64
	Title        string
	RuleCount    int
	EnabledCount int
}

// AuditQuery carries the filter and pagination options for the panel's audit
// log listing. All pointers are optional filters; nil means "no constraint".
type AuditQuery struct {
	ChatID         *int64
	From           *time.Time
	To             *time.Time // exclusive
	Success        *bool
	RuleID         *int64
	Page, PageSize int
}

// PanelCredentials is the persisted WebUI login pair. Only the password hash
// is stored; the plaintext password never touches the database.
type PanelCredentials struct {
	Username     string
	PasswordHash string // SHA-256 digest in lowercase hex, 64 characters
}

// AuditPage is the paginated result of an AuditQuery.
type AuditPage struct {
	Items    []AuditEntry
	Total    int64
	Page     int
	PageSize int
}

// AuditStatsOverview holds the dashboard's headline counters.
type AuditStatsOverview struct {
	TotalChats   int64
	TotalRules   int64
	EnabledRules int64
	HitsToday    int64
	DeletedToday int64
	FailedToday  int64
	Hits7Day     int64
	TotalHits    int64
}

// AuditDayStat is one day of aggregated moderation actions, used by the
// dashboard trend chart. Date is a UTC midnight.
type AuditDayStat struct {
	Date    time.Time
	Hits    int64
	Deleted int64
	Failed  int64
}
