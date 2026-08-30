// Package telegram contains the concrete Telegram Bot API adapter and update
// conversion helpers used by the moderation service.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// Client implements ports.TelegramClient using go-telegram-bot-api. The
// upstream library predates forum topics, so SendMessage uses MakeRequest to
// include message_thread_id when one is provided.
type Client struct {
	bot *tgbotapi.BotAPI
}

func NewClient(bot *tgbotapi.BotAPI) *Client { return &Client{bot: bot} }

// NewTelegramClient is an explicit alias for integrations that prefer the
// interface name in constructor calls.
func NewTelegramClient(bot *tgbotapi.BotAPI) *Client { return NewClient(bot) }

// Bot exposes the underlying API client for polling setup and graceful
// shutdown. Callers should use Client methods for moderation side effects.
func (c *Client) Bot() *tgbotapi.BotAPI {
	if c == nil {
		return nil
	}
	return c.bot
}

var _ ports.TelegramClient = (*Client)(nil)

func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.bot == nil {
		return errors.New("telegram client is nil")
	}
	_, err := c.bot.Request(tgbotapi.DeleteMessageConfig{ChatID: chatID, MessageID: messageID})
	return err
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, threadID *int, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.bot == nil {
		return errors.New("telegram client is nil")
	}
	if text == "" {
		return errors.New("telegram message text is empty")
	}
	params := tgbotapi.Params{
		"chat_id": strconv.FormatInt(chatID, 10),
		"text":    text,
	}
	if threadID != nil && *threadID > 0 {
		params["message_thread_id"] = strconv.Itoa(*threadID)
	}
	response, err := c.bot.MakeRequest("sendMessage", params)
	if err != nil {
		return err
	}
	if response == nil || !response.Ok {
		if response == nil {
			return errors.New("telegram sendMessage returned an empty response")
		}
		return fmt.Errorf("telegram sendMessage failed: %s", response.Description)
	}
	return nil
}

func (c *Client) IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c == nil || c.bot == nil {
		return false, errors.New("telegram client is nil")
	}
	member, err := c.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: userID},
	})
	if err != nil {
		return false, err
	}
	return member.Status == "creator" || member.Status == "owner" || member.Status == "administrator", nil
}

// FromUpdate converts a library update into the domain message. The boolean
// is false when the update is unrelated to a group message.
func FromUpdate(update tgbotapi.Update) (domain.ModerationMessage, bool) {
	if update.Message != nil {
		return FromMessage(update.Message, nil)
	}
	if update.EditedMessage != nil {
		return FromMessage(update.EditedMessage, nil)
	}
	return domain.ModerationMessage{}, false
}

// FromUpdatePtr is the pointer-friendly counterpart to FromUpdate.
func FromUpdatePtr(update *tgbotapi.Update) (domain.ModerationMessage, bool) {
	if update == nil {
		return domain.ModerationMessage{}, false
	}
	return FromUpdate(*update)
}

// ConvertUpdate is an alias kept for callers that use conversion-oriented
// naming in their polling loop.
func ConvertUpdate(update tgbotapi.Update) (domain.ModerationMessage, bool) {
	return FromUpdate(update)
}

// FromMessage converts a Telegram message. threadID is optional because the
// current upstream client does not expose message_thread_id on Message.
func FromMessage(message *tgbotapi.Message, threadID *int) (domain.ModerationMessage, bool) {
	if message == nil || message.Chat == nil || (message.Chat.Type != "group" && message.Chat.Type != "supergroup") {
		return domain.ModerationMessage{}, false
	}
	var userID *int64
	userIsBot := false
	if message.From != nil {
		id := message.From.ID
		userID = &id
		userIsBot = message.From.IsBot
	}
	return domain.ModerationMessage{
		ChatID:          message.Chat.ID,
		ChatTitle:       message.Chat.Title,
		ChatType:        message.Chat.Type,
		MessageID:       message.MessageID,
		MessageThreadID: threadID,
		UserID:          userID,
		UserIsBot:       userIsBot,
		Text:            message.Text,
		Caption:         message.Caption,
	}, true
}

// ConvertMessage is an alias for FromMessage.
func ConvertMessage(message *tgbotapi.Message, threadID *int) (domain.ModerationMessage, bool) {
	return FromMessage(message, threadID)
}

// RawUpdate is a minimal update representation that preserves forum topic
// IDs omitted by older versions of go-telegram-bot-api.
type RawUpdate struct {
	Message       *RawMessage `json:"message,omitempty"`
	EditedMessage *RawMessage `json:"edited_message,omitempty"`
}

type RawMessage struct {
	MessageID       int            `json:"message_id"`
	MessageThreadID *int           `json:"message_thread_id,omitempty"`
	From            *tgbotapi.User `json:"from,omitempty"`
	Chat            *tgbotapi.Chat `json:"chat"`
	Text            string         `json:"text,omitempty"`
	Caption         string         `json:"caption,omitempty"`
}

// ParseUpdate converts raw Telegram JSON while retaining message_thread_id.
// It is useful when polling via a transport that exposes raw update payloads.
func ParseUpdate(data []byte) (domain.ModerationMessage, bool, error) {
	var raw RawUpdate
	if err := json.Unmarshal(data, &raw); err != nil {
		return domain.ModerationMessage{}, false, err
	}
	message := raw.Message
	if message == nil {
		message = raw.EditedMessage
	}
	if message == nil || message.Chat == nil || (message.Chat.Type != "group" && message.Chat.Type != "supergroup") {
		return domain.ModerationMessage{}, false, nil
	}
	var userID *int64
	userIsBot := false
	if message.From != nil {
		id := message.From.ID
		userID = &id
		userIsBot = message.From.IsBot
	}
	return domain.ModerationMessage{
		ChatID: message.Chat.ID, ChatTitle: message.Chat.Title, ChatType: message.Chat.Type,
		MessageID: message.MessageID, MessageThreadID: message.MessageThreadID,
		UserID: userID, UserIsBot: userIsBot, Text: message.Text, Caption: message.Caption,
	}, true, nil
}
