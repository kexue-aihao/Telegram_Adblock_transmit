// Package profile bounds the cost of optional user bio lookups. It caches only
// source text, so changes to moderation rules take effect on the next message.
package profile

import (
	"container/list"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

const (
	cacheCapacity = 10000
	successTTL    = 10 * time.Minute
	failureTTL    = time.Minute
	lookupTimeout = 2 * time.Second
)

var ErrLookupSkipped = errors.New("user profile lookup skipped by rate limit")

type result struct {
	bio string
	err error
}

type entry struct {
	userID int64
	result
	expires time.Time
}

type flight struct {
	done chan struct{}
	result
}

// Cache coalesces concurrent lookups and enforces one new request per second
// across all users and groups. Call New once per bot, before polling starts.
type Cache struct {
	reader      ports.UserProfileReader
	logger      *slog.Logger
	mu          sync.Mutex
	entries     map[int64]*list.Element
	lru         *list.List
	flights     map[int64]*flight
	nextRequest time.Time
	capacity    int
	now         func() time.Time
}

var _ ports.UserProfileReader = (*Cache)(nil)

func New(reader ports.UserProfileReader, logger *slog.Logger) *Cache {
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		reader: reader, logger: logger, entries: make(map[int64]*list.Element),
		lru: list.New(), flights: make(map[int64]*flight), capacity: cacheCapacity, now: time.Now,
	}
}

func (c *Cache) GetUserBio(ctx context.Context, userID int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if userID <= 0 || c.reader == nil {
		return "", errors.New("invalid user profile lookup")
	}
	c.mu.Lock()
	now := c.now()
	if element := c.entries[userID]; element != nil {
		cached := element.Value.(entry)
		if now.Before(cached.expires) {
			c.lru.MoveToFront(element)
			c.mu.Unlock()
			return cached.bio, cached.err
		}
		c.remove(element)
	}
	if pending := c.flights[userID]; pending != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-pending.done:
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return pending.bio, pending.err
		}
	}
	if now.Before(c.nextRequest) {
		c.mu.Unlock()
		c.logger.Debug("user profile lookup", "user_id", userID, "status", "rate_limited")
		return "", ErrLookupSkipped
	}
	c.nextRequest = now.Add(time.Second)
	pending := &flight{done: make(chan struct{})}
	c.flights[userID] = pending
	c.mu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, lookupTimeout)
	bio, err := c.reader.GetUserBio(queryCtx, userID)
	if queryCtx.Err() != nil {
		bio, err = "", queryCtx.Err()
	}
	cancel()
	if err != nil {
		bio = ""
	}

	c.mu.Lock()
	now = c.now()
	ttl, status := successTTL, "available"
	if bio == "" {
		status = "empty"
	}
	if err != nil {
		ttl, status = failureTTL, "unavailable"
	}
	var limited *ports.ProfileRateLimitError
	if errors.As(err, &limited) {
		status = "server_rate_limited"
		resume := now.Add(max(time.Second, limited.RetryAfter))
		if resume.After(c.nextRequest) {
			c.nextRequest = resume
		}
	}
	// A shutdown or canceled caller must not poison later lookups. Our own
	// short query timeout is cached like other transient lookup failures.
	if ctx.Err() != nil {
		bio, err, status = "", ctx.Err(), "canceled"
	} else {
		c.entries[userID] = c.lru.PushFront(entry{userID: userID, result: result{bio, err}, expires: now.Add(ttl)})
		if c.lru.Len() > c.capacity {
			c.remove(c.lru.Back())
		}
	}
	pending.result = result{bio, err}
	delete(c.flights, userID)
	close(pending.done)
	c.mu.Unlock()
	// Status labels only: SDK errors may contain remote text or request URLs.
	c.logger.Debug("user profile lookup", "user_id", userID, "status", status)
	return bio, err
}

// remove is called with mu held.
func (c *Cache) remove(element *list.Element) {
	delete(c.entries, element.Value.(entry).userID)
	c.lru.Remove(element)
}
