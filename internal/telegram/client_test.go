package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestClientDeleteMessageCancelsInFlightRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	bot := &tgbotapi.BotAPI{Token: "test-token", Client: httpClientFunc(func(r *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
		return nil, r.Context().Err()
	})}
	client := NewClientWithAPIEndpoint(bot, "https://api.telegram.org/bot%s/%s")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- client.DeleteMessage(ctx, -100, 7) }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Telegram request did not reach server")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DeleteMessage() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeleteMessage() did not return after context cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("underlying HTTP request context was not canceled")
	}
}

func TestClientRedactsTokenFromHTTPError(t *testing.T) {
	const token = "secret-token"
	bot := &tgbotapi.BotAPI{Token: token, Client: httpClientFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Post %s: %w", r.URL.String(), context.DeadlineExceeded)
	})}
	client := NewClientWithAPIEndpoint(bot, "https://api.telegram.org/bot%s/%s")
	err := client.DeleteMessage(context.Background(), -100, 7)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("DeleteMessage() error leaked token: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DeleteMessage() error = %v, want deadline exceeded", err)
	}
}

func TestClientSendMessageUsesEndpointAndTopic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("chat_id") != "-100" || r.Form.Get("message_thread_id") != "12" || r.Form.Get("text") != "notice" {
			t.Fatalf("unexpected form: %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	bot := &tgbotapi.BotAPI{Token: "test-token", Client: server.Client()}
	client := NewClientWithAPIEndpoint(bot, server.URL+"/bot%s/%s")
	topicID := 12
	if err := client.SendMessage(context.Background(), -100, &topicID, "notice"); err != nil {
		t.Fatal(err)
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFromMessageFiltersUnsupportedChatsAndCopiesFields(t *testing.T) {
	threadID := 12
	message := &tgbotapi.Message{
		MessageID: 9,
		From:      &tgbotapi.User{ID: 42, IsBot: true},
		Chat:      &tgbotapi.Chat{ID: -100, Type: "supergroup", Title: "Forum"},
		Caption:   "caption",
	}
	converted, ok := FromMessage(message, &threadID)
	if !ok || converted.ChatID != -100 || converted.MessageID != 9 || converted.Caption != "caption" || !converted.UserIsBot {
		t.Fatalf("unexpected conversion: %+v, %v", converted, ok)
	}
	if converted.MessageThreadID == nil || *converted.MessageThreadID != 12 {
		t.Fatalf("thread id not retained: %+v", converted)
	}
	message.Chat.Type = "private"
	if _, ok := FromMessage(message, &threadID); ok {
		t.Fatal("private chat should not be supported")
	}
}

func TestParseUpdateRetainsTopicAndEditedMessage(t *testing.T) {
	threadID := 88
	payload, err := json.Marshal(map[string]any{
		"edited_message": map[string]any{
			"message_id":        3,
			"message_thread_id": threadID,
			"from":              map[string]any{"id": 7, "is_bot": false},
			"chat":              map[string]any{"id": -100, "type": "supergroup", "title": "x"},
			"text":              "edited",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	message, ok, err := ParseUpdate(payload)
	if err != nil || !ok || message.MessageThreadID == nil || *message.MessageThreadID != threadID || message.Text != "edited" {
		t.Fatalf("ParseUpdate() = %+v, %v, %v", message, ok, err)
	}
}
