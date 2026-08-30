package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// Poller uses raw getUpdates responses because the selected Telegram client
// version does not expose message_thread_id on its typed Message struct.
// Keeping the raw JSON also avoids silently losing forum-topic routing.
type Poller struct {
	Bot         *tgbotapi.BotAPI
	APIEndpoint string
	Timeout     int
	RetryDelay  time.Duration
	// MaxHandlerRetries is the number of times a failed update is retried before
	// it is skipped. A non-positive value uses the default.
	MaxHandlerRetries int
	Logger            *slog.Logger
}

func (p *Poller) Run(ctx context.Context, handle func(context.Context, domain.ModerationMessage) error) error {
	if p == nil || p.Bot == nil {
		return fmt.Errorf("telegram poller bot is nil")
	}
	if handle == nil {
		return fmt.Errorf("telegram poller handler is nil")
	}
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	retryDelay := p.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 3 * time.Second
	}
	maxHandlerRetries := p.MaxHandlerRetries
	if maxHandlerRetries <= 0 {
		maxHandlerRetries = 3
	}
	// Begin at Telegram's default offset so updates queued while the process was
	// down are handled instead of being discarded.
	var offset int
	var failedUpdateID *int
	failedAttempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		params := tgbotapi.Params{
			"offset":          strconv.Itoa(offset),
			"timeout":         strconv.Itoa(timeout),
			"allowed_updates": `["message","edited_message"]`,
		}
		response, err := p.getUpdates(ctx, params)
		if err != nil {
			logger.Warn("telegram polling request failed", "error", err)
			if !sleepContext(ctx, retryDelay) {
				return ctx.Err()
			}
			continue
		}
		if response == nil {
			logger.Warn("telegram polling returned an empty response")
			if !sleepContext(ctx, retryDelay) {
				return ctx.Err()
			}
			continue
		}
		var updates []json.RawMessage
		if err := json.Unmarshal(response.Result, &updates); err != nil {
			return fmt.Errorf("decode telegram updates: %w", err)
		}
		shouldRetry := false
		for _, raw := range updates {
			if err := ctx.Err(); err != nil {
				return err
			}
			var envelope struct {
				UpdateID int `json:"update_id"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				return fmt.Errorf("decode telegram update envelope: %w", err)
			}
			message, ok, err := ParseUpdate(raw)
			if err != nil {
				logger.Warn("decode telegram message failed", "error", err)
				// The update is not processable by this version of the bot. Advance
				// it so one malformed payload cannot block the whole update stream.
				offset = envelope.UpdateID + 1
				continue
			}
			if !ok {
				offset = envelope.UpdateID + 1
				continue
			}
			if err := handle(ctx, message); err != nil {
				logger.Error("message moderation failed", "chat_id", message.ChatID, "message_id", message.MessageID, "error", err)
				// Do not acknowledge this update. The next getUpdates call uses the
				// last successfully processed offset, so Telegram redelivers it.
				if failedUpdateID == nil || *failedUpdateID != envelope.UpdateID {
					updateID := envelope.UpdateID
					failedUpdateID = &updateID
					failedAttempts = 0
				}
				failedAttempts++
				if failedAttempts >= maxHandlerRetries {
					logger.Error("skipping update after exhausted moderation retries", "update_id", envelope.UpdateID, "attempts", failedAttempts)
					offset = envelope.UpdateID + 1
					failedUpdateID = nil
					failedAttempts = 0
					continue
				}
				shouldRetry = true
				break
			}
			offset = envelope.UpdateID + 1
			failedUpdateID = nil
			failedAttempts = 0
		}
		if shouldRetry {
			if !sleepContext(ctx, retryDelay) {
				return ctx.Err()
			}
		}
	}
}

// getUpdates uses a context-bound request when APIEndpoint is available. The
// upstream library's MakeRequest cannot attach a context, so the fallback is
// retained for callers that construct Poller directly without an endpoint.
func (p *Poller) getUpdates(ctx context.Context, params tgbotapi.Params) (*tgbotapi.APIResponse, error) {
	if p.APIEndpoint == "" {
		return p.Bot.MakeRequest("getUpdates", params)
	}
	if p.Bot.Client == nil {
		return nil, fmt.Errorf("telegram poller HTTP client is nil")
	}
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	requestURL := fmt.Sprintf(p.APIEndpoint, p.Bot.Token, "getUpdates")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, redactTelegramError(fmt.Errorf("create Telegram getUpdates request: %w", err), p.Bot.Token)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.Bot.Client.Do(req)
	if err != nil {
		return nil, redactTelegramError(err, p.Bot.Token)
	}
	defer resp.Body.Close()

	var response tgbotapi.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Telegram getUpdates response: %w", err)
	}
	if !response.Ok {
		var parameters tgbotapi.ResponseParameters
		if response.Parameters != nil {
			parameters = *response.Parameters
		}
		return &response, &tgbotapi.Error{Code: response.ErrorCode, Message: response.Description, ResponseParameters: parameters}
	}
	return &response, nil
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
