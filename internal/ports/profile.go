package ports

import (
	"context"
	"time"
)

// UserProfileReader returns an accessible user's bio. An empty bio is a
// successful lookup; an inaccessible profile is an error, never ad evidence.
type UserProfileReader interface {
	GetUserBio(ctx context.Context, userID int64) (string, error)
}

// ProfileRateLimitError carries the server's cooldown without exposing API
// response text or depending on the Telegram SDK in the cache layer.
type ProfileRateLimitError struct {
	RetryAfter time.Duration
}

func (*ProfileRateLimitError) Error() string { return "user profile lookup rate limited" }
