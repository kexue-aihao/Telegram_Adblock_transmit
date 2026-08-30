package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// Poller uses raw getUpdates responses because the selected Telegram client
// version does not expose message_thread_id on its typed Message struct.
// Keeping the raw JSON also avoids silently losing forum-topic routing.
type Poller struct {
	Bot     *tgbotapi.BotAPI
	Timeout int
	Logger  *slog.Logger
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
	// Telegram delivers queued updates from the previous process lifetime by
	// default. A negative offset asks Telegram to discard that backlog; capture
	// the returned update ID so it cannot be delivered again on the next poll.
	var offset int
	for {
		var err error
		offset, err = p.discardPending(ctx, logger)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !sleepContext(ctx, 3*time.Second) {
			return ctx.Err()
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		params := tgbotapi.Params{
			"offset":          strconv.Itoa(offset),
			"timeout":         strconv.Itoa(timeout),
			"allowed_updates": `["message","edited_message"]`,
		}
		response, err := p.Bot.MakeRequest("getUpdates", params)
		if err != nil {
			logger.Warn("telegram polling request failed", "error", err)
			if !sleepContext(ctx, 3*time.Second) {
				return ctx.Err()
			}
			continue
		}
		if response == nil {
			logger.Warn("telegram polling returned an empty response")
			if !sleepContext(ctx, 3*time.Second) {
				return ctx.Err()
			}
			continue
		}
		var updates []json.RawMessage
		if err := json.Unmarshal(response.Result, &updates); err != nil {
			return fmt.Errorf("decode telegram updates: %w", err)
		}
		for _, raw := range updates {
			var envelope struct {
				UpdateID int `json:"update_id"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				logger.Warn("decode telegram update envelope failed", "error", err)
				continue
			}
			if envelope.UpdateID >= offset {
				offset = envelope.UpdateID + 1
			}
			message, ok, err := ParseUpdate(raw)
			if err != nil {
				logger.Warn("decode telegram message failed", "error", err)
				continue
			}
			if !ok {
				continue
			}
			if err := handle(ctx, message); err != nil {
				logger.Error("message moderation failed", "chat_id", message.ChatID, "message_id", message.MessageID, "error", err)
			}
		}
	}
}

func (p *Poller) discardPending(ctx context.Context, logger *slog.Logger) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	response, err := p.Bot.MakeRequest("getUpdates", tgbotapi.Params{
		"offset":          "-1",
		"timeout":         "0",
		"allowed_updates": `["message","edited_message"]`,
	})
	if err != nil {
		logger.Warn("unable to discard pending telegram updates", "error", err)
		return 0, err
	}
	var updates []struct {
		UpdateID int `json:"update_id"`
	}
	if response == nil {
		return 0, fmt.Errorf("telegram discard response is empty")
	}
	if err := json.Unmarshal(response.Result, &updates); err != nil {
		return 0, fmt.Errorf("decode pending telegram updates: %w", err)
	}
	offset := 0
	for _, update := range updates {
		if update.UpdateID >= offset {
			offset = update.UpdateID + 1
		}
	}
	return offset, nil
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
