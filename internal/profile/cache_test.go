package profile

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type readerFunc func(context.Context, int64) (string, error)

func (f readerFunc) GetUserBio(ctx context.Context, id int64) (string, error) { return f(ctx, id) }

func TestCacheExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, bio string
		err       error
		ttl       time.Duration
	}{
		{"available", "source bio", nil, 10 * time.Minute},
		{"empty", "", nil, 10 * time.Minute},
		{"failure", "must discard", errors.New("unavailable"), time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			now := time.Now()
			cache := New(readerFunc(func(context.Context, int64) (string, error) {
				calls++
				return tc.bio, tc.err
			}), nil)
			cache.now = func() time.Time { return now }
			wantBio := tc.bio
			if tc.err != nil {
				wantBio = ""
			}
			for i := 0; i < 2; i++ {
				bio, err := cache.GetUserBio(context.Background(), 42)
				if bio != wantBio || !errors.Is(err, tc.err) {
					t.Fatalf("result = %q, %v", bio, err)
				}
				now = now.Add(tc.ttl - time.Nanosecond)
			}
			if calls != 1 {
				t.Fatalf("cached lookup refetched %d times", calls)
			}
			if _, err := cache.GetUserBio(context.Background(), 42); !errors.Is(err, tc.err) || calls != 2 {
				t.Fatalf("expired lookup = calls %d, err %v", calls, err)
			}
		})
	}
}

func TestCacheGlobalRateLimitAndServerCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	calls := 0
	cache := New(readerFunc(func(_ context.Context, id int64) (string, error) {
		calls++
		if id == 2 {
			return "", &ports.ProfileRateLimitError{RetryAfter: 17 * time.Second}
		}
		return "bio", nil
	}), nil)
	cache.now = func() time.Time { return now }
	if _, err := cache.GetUserBio(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetUserBio(ctx, 2); !errors.Is(err, ErrLookupSkipped) {
		t.Fatalf("not limited: %v", err)
	}
	now = now.Add(time.Second)
	if _, err := cache.GetUserBio(ctx, 2); err == nil {
		t.Fatal("server limit lost")
	}
	now = now.Add(16 * time.Second)
	if _, err := cache.GetUserBio(ctx, 3); !errors.Is(err, ErrLookupSkipped) {
		t.Fatalf("cooldown ignored: %v", err)
	}
	if bio, err := cache.GetUserBio(ctx, 1); bio != "bio" || err != nil {
		t.Fatal("cooldown blocked cached data")
	}
	now = now.Add(time.Second)
	if _, err := cache.GetUserBio(ctx, 3); err != nil || calls != 3 {
		t.Fatalf("did not resume: %d %v", calls, err)
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Now()
	calls := map[int64]int{}
	cache := New(readerFunc(func(_ context.Context, id int64) (string, error) {
		calls[id]++
		return "bio", nil
	}), nil)
	cache.now = func() time.Time { return now }
	cache.capacity = 2
	for _, id := range []int64{1, 2, 1, 3, 1, 2} {
		if _, err := cache.GetUserBio(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
		if len(cache.entries) > 2 {
			t.Fatal("capacity exceeded")
		}
	}
	if calls[1] != 1 || calls[2] != 2 || calls[3] != 1 {
		t.Fatalf("wrong eviction: %v", calls)
	}
}

func TestCacheCoalescesConcurrentLookups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		release := make(chan struct{})
		cache := New(readerFunc(func(ctx context.Context, _ int64) (string, error) {
			calls.Add(1)
			select {
			case <-release:
				return "shared bio", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}), nil)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				bio, err := cache.GetUserBio(context.Background(), 42)
				if bio != "shared bio" || err != nil {
					t.Errorf("coalesced result: %q %v", bio, err)
				}
			}()
		}
		synctest.Wait()
		// Canceling a waiting caller does not cancel the shared request.
		waitCtx, cancel := context.WithCancel(context.Background())
		waited := make(chan error, 1)
		go func() { _, err := cache.GetUserBio(waitCtx, 42); waited <- err }()
		synctest.Wait()
		cancel()
		if err := <-waited; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		close(release)
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("got %d upstream calls", calls.Load())
		}
	})
}

func TestCacheTimeoutAndParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		cache := New(readerFunc(func(ctx context.Context, _ int64) (string, error) {
			calls++
			<-ctx.Done()
			return "", ctx.Err()
		}), nil)
		start := time.Now()
		if _, err := cache.GetUserBio(context.Background(), 42); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 2*time.Second {
			t.Fatalf("timeout did not bound lookup: %v, %s", err, time.Since(start))
		}
		_, _ = cache.GetUserBio(context.Background(), 42)
		if calls != 1 {
			t.Fatal("timeout not cached")
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := cache.GetUserBio(ctx, 43); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if cache.entries[43] != nil {
			t.Fatal("caller cancellation cached")
		}
		_, _ = cache.GetUserBio(ctx, 42)
		if calls != 2 {
			t.Fatal("canceled context made new request")
		}
	})
}

func TestCacheLogsOnlyStatus(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := New(readerFunc(func(context.Context, int64) (string, error) {
		return "private bio", errors.New("secret-token: private bio")
	}), logger)
	_, _ = cache.GetUserBio(context.Background(), 42)
	if strings.Contains(output.String(), "secret-token") || strings.Contains(output.String(), "private bio") || !strings.Contains(output.String(), "unavailable") {
		t.Fatalf("unsafe or missing status: %s", output.String())
	}
}
