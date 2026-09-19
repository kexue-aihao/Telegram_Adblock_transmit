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
	BuiltinHits     []string
	BuiltinDetails  *BuiltinDetails
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
	BuiltinHits     []string
	BuiltinDetails  *BuiltinDetails
	Content         string
	DeleteSucceeded bool
	DeletionError   string
}

// MessageEntityInfo is the subset of Telegram message entity data the built-in
// ad filter needs. Text offsets are resolved to the actual @username, and
// text_mention entities carry the mentioned user's bot flag.
type MessageEntityInfo struct {
	Type     string // "mention", "text_mention", "url", "text_link", …
	Username string // mentioned @username (without the leading @), if any
	IsBot    bool   // only known for text_mention entities
	HasURL   bool   // entity points at a link (url / text_link)
	URL      string // actual target, resolved before text normalization
	Offset   int    // original Telegram UTF-16 code-unit offset
	Length   int    // original Telegram UTF-16 code-unit length
}

// InlineButtonInfo retains visible button text and its optional link target.
// Callback data is deliberately not used as advertising content.
type InlineButtonInfo struct {
	Text string
	URL  string
}

// ForwardInfo describes where a forwarded message originated, used to detect
// forwarded advertisements. Type mirrors Telegram's forward_origin type:
// "user", "hidden_user", "channel", "chat", or "" when unknown.
type ForwardInfo struct {
	Type        string
	SourceID    int64
	SourceTitle string
}

type ModerationMessage struct {
	ChatID          int64
	ChatTitle       string
	ChatType        string
	MessageID       int
	MessageThreadID *int
	UserID          *int64
	UserIsBot       bool
	SenderChatID    *int64 // Non-nil for channel identities and anonymous administrators.
	Text            string
	Caption         string
	Entities        []MessageEntityInfo
	InlineButtons   []InlineButtonInfo
	Forward         *ForwardInfo
	Reply           *ReplyInfo
}

func (m ModerationMessage) Content() string {
	if m.Text != "" {
		return m.Text
	}
	return m.Caption
}

// ReplyInfo is the quoted message an administrator replied to. Only the text
// or caption is retained: rule authoring converts that content into a pattern
// and no other field of the quoted message participates.
type ReplyInfo struct {
	MessageID   int
	Text        string
	Caption     string
	UserID      *int64
	SenderIsBot bool
}

func (r ReplyInfo) Content() string {
	if r.Text != "" {
		return r.Text
	}
	return r.Caption
}
