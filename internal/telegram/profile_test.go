package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

func TestGetUserBio(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		wantErr              bool
		retry                time.Duration
	}{
		{name: "bio", response: `{"ok":true,"result":{"id":42,"type":"private","bio":"个人简介"}}`, want: "个人简介"},
		{name: "empty", response: `{"ok":true,"result":{"id":42,"type":"private","bio":""}}`},
		{name: "missing", response: `{"ok":true,"result":{"id":42,"type":"private"}}`},
		{name: "wrong_user", response: `{"ok":true,"result":{"id":43,"type":"private","bio":"广告"}}`, wantErr: true},
		{name: "wrong_chat", response: `{"ok":true,"result":{"id":42,"type":"channel","bio":"广告"}}`, wantErr: true},
		{name: "null", response: `{"ok":true,"result":null}`, wantErr: true},
		{name: "invalid_bio", response: `{"ok":true,"result":{"id":42,"type":"private","bio":[]}}`, wantErr: true},
		{name: "invalid_json", response: `<html>bad gateway</html>`, wantErr: true},
		{name: "not_found", response: `{"ok":false,"error_code":400,"description":"chat not found"}`, wantErr: true},
		{name: "forbidden", response: `{"ok":false,"error_code":403,"description":"forbidden test-token"}`, wantErr: true},
		{name: "limited", response: `{"ok":false,"error_code":429,"parameters":{"retry_after":17}}`, wantErr: true, retry: 17 * time.Second},
		{name: "limited_without_delay", response: `{"ok":false,"error_code":429}`, wantErr: true, retry: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &tgbotapi.BotAPI{Token: "test-token", Client: httpClientFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://custom.example/bottest-token/getChat" || r.Method != http.MethodPost {
					t.Fatalf("wrong endpoint: %v", r)
				}
				if err := r.ParseForm(); err != nil || r.Form.Get("chat_id") != "42" {
					t.Fatalf("wrong user: %v, %v", r.Form, err)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})}
			client := NewClientWithAPIEndpoint(bot, "https://custom.example/bot%s/%s")
			bio, err := client.GetUserBio(context.Background(), 42)
			if bio != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("GetUserBio = %q, %v", bio, err)
			}
			if err != nil && strings.Contains(err.Error(), bot.Token) {
				t.Fatal("error leaked bot token")
			}
			if tc.retry > 0 {
				var limited *ports.ProfileRateLimitError
				if !errors.As(err, &limited) || limited.RetryAfter != tc.retry {
					t.Fatalf("cooldown not preserved: %v", err)
				}
			}
		})
	}
}

func TestGetUserBioTimeoutAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		bot := &tgbotapi.BotAPI{Token: "secret-token", Client: httpClientFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			<-r.Context().Done()
			return nil, fmt.Errorf("Post %s: %w", r.URL, r.Context().Err())
		})}
		client := NewClientWithAPIEndpoint(bot, "https://custom.example/bot%s/%s")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		bio, err := client.GetUserBio(ctx, 42)
		if bio != "" || !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), bot.Token) || time.Since(start) != 2*time.Second {
			t.Fatalf("timeout = %q, %v after %s", bio, err, time.Since(start))
		}
		ctx, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		if _, err := client.GetUserBio(ctx, 42); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("canceled lookup made request: %d, %v", calls, err)
		}
	})
}

func TestSenderChatIdentityBothConversionPaths(t *testing.T) {
	message := &tgbotapi.Message{
		MessageID: 9, From: &tgbotapi.User{ID: 42}, SenderChat: &tgbotapi.Chat{ID: -123, Type: "channel"},
		Chat: &tgbotapi.Chat{ID: -100, Type: "supergroup"}, Text: "看我主页",
	}
	typed, ok := FromMessage(message, nil)
	if !ok || typed.SenderChatID == nil || *typed.SenderChatID != -123 {
		t.Fatalf("typed identity lost: %+v", typed)
	}
	for _, field := range []string{"message", "edited_message"} {
		raw := fmt.Sprintf(`{"%s":{"message_id":9,"chat":{"id":-100,"type":"supergroup"},"from":{"id":42},"sender_chat":{"id":-123},"text":"看我主页"}}`, field)
		converted, ok, err := ParseUpdate([]byte(raw))
		if err != nil || !ok || converted.SenderChatID == nil || *converted.SenderChatID != -123 {
			t.Fatalf("raw identity lost: %+v, %v", converted, err)
		}
	}
}
