package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestPollerProcessesPendingUpdatesAndAcknowledgesAfterSuccess(t *testing.T) {
	var (
		mu      sync.Mutex
		offsets []string
		cancel  context.CancelFunc
	)
	bot := newPollingTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		form := readPollingForm(t, r)
		mu.Lock()
		offsets = append(offsets, form.Get("offset"))
		requestNumber := len(offsets)
		mu.Unlock()

		switch requestNumber {
		case 1:
			writeTelegramResult(t, w, []any{testUpdate(5), testUpdate(6)})
		default:
			cancel()
			writeTelegramResult(t, w, []any{})
		}
	})

	ctx, cancelFunc := context.WithCancel(context.Background())
	cancel = cancelFunc
	defer cancelFunc()
	var handled []int
	poller := &Poller{Bot: bot, Timeout: 1, RetryDelay: time.Millisecond}
	err := poller.Run(ctx, func(_ context.Context, message domain.ModerationMessage) error {
		handled = append(handled, message.MessageID)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got, want := handled, []int{5, 6}; !sameInts(got, want) {
		t.Fatalf("handled message IDs = %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) < 2 || offsets[0] != "0" || offsets[1] != "7" {
		t.Fatalf("getUpdates offsets = %v, want first two [0 7]", offsets)
	}
}

func TestPollerRetriesFailedHandlerWithoutAcknowledgingUpdate(t *testing.T) {
	var (
		mu      sync.Mutex
		offsets []string
		cancel  context.CancelFunc
	)
	bot := newPollingTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		form := readPollingForm(t, r)
		mu.Lock()
		offsets = append(offsets, form.Get("offset"))
		requestNumber := len(offsets)
		mu.Unlock()

		if requestNumber < 3 {
			writeTelegramResult(t, w, []any{testUpdate(5)})
			return
		}
		cancel()
		writeTelegramResult(t, w, []any{})
	})

	ctx, cancelFunc := context.WithCancel(context.Background())
	cancel = cancelFunc
	defer cancelFunc()
	attempts := 0
	poller := &Poller{Bot: bot, Timeout: 1, RetryDelay: time.Millisecond}
	err := poller.Run(ctx, func(_ context.Context, _ domain.ModerationMessage) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary moderation failure")
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if attempts != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) < 3 || !sameStrings(offsets[:3], []string{"0", "0", "6"}) {
		t.Fatalf("getUpdates offsets = %v, want first three [0 0 6]", offsets)
	}
}

func TestPollerSkipsUpdateAfterExhaustingHandlerRetries(t *testing.T) {
	var (
		mu      sync.Mutex
		offsets []string
		cancel  context.CancelFunc
	)
	bot := newPollingTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		form := readPollingForm(t, r)
		mu.Lock()
		offsets = append(offsets, form.Get("offset"))
		requestNumber := len(offsets)
		mu.Unlock()

		switch requestNumber {
		case 1, 2:
			writeTelegramResult(t, w, []any{testUpdate(5), testUpdate(6)})
		default:
			cancel()
			writeTelegramResult(t, w, []any{})
		}
	})

	ctx, cancelFunc := context.WithCancel(context.Background())
	cancel = cancelFunc
	defer cancelFunc()
	var handled []int
	poller := &Poller{Bot: bot, Timeout: 1, RetryDelay: time.Millisecond, MaxHandlerRetries: 2}
	err := poller.Run(ctx, func(_ context.Context, message domain.ModerationMessage) error {
		handled = append(handled, message.MessageID)
		if message.MessageID == 5 {
			return errors.New("permanent moderation failure")
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if got, want := handled, []int{5, 5, 6}; !sameInts(got, want) {
		t.Fatalf("handled message IDs = %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(offsets) < 3 || !sameStrings(offsets[:3], []string{"0", "0", "7"}) {
		t.Fatalf("getUpdates offsets = %v, want first three [0 0 7]", offsets)
	}
}

func TestPollerCancellationInterruptsRetryWait(t *testing.T) {
	firstRequest := make(chan struct{})
	bot := newPollingTestBot(t, func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-firstRequest:
		default:
			close(firstRequest)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"temporary failure"}`))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poller := &Poller{Bot: bot, Timeout: 1, RetryDelay: time.Second}
	done := make(chan error, 1)
	go func() { done <- poller.Run(ctx, func(context.Context, domain.ModerationMessage) error { return nil }) }()
	select {
	case <-firstRequest:
	case <-time.After(time.Second):
		t.Fatal("getUpdates was not requested")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Fatalf("Run() took %s after cancellation, retry wait was not interrupted", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestPollerCancellationInterruptsLongPollRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	bot := &tgbotapi.BotAPI{Token: "token", Client: httpClientFunc(func(r *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poller := &Poller{Bot: bot, APIEndpoint: "https://api.telegram.org/bot%s/%s", Timeout: 30, RetryDelay: time.Second}
	done := make(chan error, 1)
	go func() { done <- poller.Run(ctx, func(context.Context, domain.ModerationMessage) error { return nil }) }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("getUpdates was not requested")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after long-poll cancellation")
	}
}

func newPollingTestBot(t *testing.T, getUpdates http.HandlerFunc) *tgbotapi.BotAPI {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottoken/getMe" {
			writeTelegramResult(t, w, map[string]any{"id": 1, "is_bot": true, "first_name": "test"})
			return
		}
		if r.URL.Path != "/bottoken/getUpdates" {
			t.Errorf("unexpected Telegram API path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		getUpdates(w, r)
	}))
	t.Cleanup(server.Close)
	bot, err := tgbotapi.NewBotAPIWithAPIEndpoint("token", server.URL+"/bot%s/%s")
	if err != nil {
		t.Fatalf("NewBotAPIWithAPIEndpoint() error = %v", err)
	}
	return bot
}

func readPollingForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm() error = %v", err)
	}
	return r.Form
}

func writeTelegramResult(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result}); err != nil {
		t.Fatalf("encode Telegram response: %v", err)
	}
}

func testUpdate(id int) map[string]any {
	return map[string]any{
		"update_id": id,
		"message": map[string]any{
			"message_id": id,
			"chat":       map[string]any{"id": -100, "type": "supergroup"},
		},
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
