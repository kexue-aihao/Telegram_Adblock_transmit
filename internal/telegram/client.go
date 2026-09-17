// Package telegram contains the concrete Telegram Bot API adapter and update
// conversion helpers used by the moderation service.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

// Client implements ports.TelegramClient using go-telegram-bot-api. The
// upstream library predates forum topics, so SendMessage uses a raw API
// request to include message_thread_id when one is provided.
type Client struct {
	bot         *tgbotapi.BotAPI
	apiEndpoint string
}

func NewClient(bot *tgbotapi.BotAPI) *Client { return &Client{bot: bot} }

// NewClientWithAPIEndpoint creates a client whose moderation requests use
// context-aware HTTP requests. The BotAPI instance must have been constructed
// with the same endpoint and an HTTP client with a timeout.
func NewClientWithAPIEndpoint(bot *tgbotapi.BotAPI, apiEndpoint string) *Client {
	return &Client{bot: bot, apiEndpoint: apiEndpoint}
}

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
	if c.bot.Client == nil {
		return errors.New("telegram HTTP client is nil")
	}
	if c.apiEndpoint == "" {
		_, err := c.bot.Request(tgbotapi.DeleteMessageConfig{ChatID: chatID, MessageID: messageID})
		return redactTelegramError(err, c.bot.Token)
	}
	_, err := c.makeRequest(ctx, "deleteMessage", url.Values{
		"chat_id":    []string{strconv.FormatInt(chatID, 10)},
		"message_id": []string{strconv.Itoa(messageID)},
	})
	return err
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, threadID *int, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.bot == nil {
		return errors.New("telegram client is nil")
	}
	if c.bot.Client == nil {
		return errors.New("telegram HTTP client is nil")
	}
	if text == "" {
		return errors.New("telegram message text is empty")
	}
	if c.apiEndpoint == "" {
		params := tgbotapi.Params{
			"chat_id": strconv.FormatInt(chatID, 10),
			"text":    text,
		}
		if threadID != nil && *threadID > 0 {
			params["message_thread_id"] = strconv.Itoa(*threadID)
		}
		response, err := c.bot.MakeRequest("sendMessage", params)
		if err != nil {
			return redactTelegramError(err, c.bot.Token)
		}
		if response == nil || !response.Ok {
			if response == nil {
				return errors.New("telegram sendMessage returned an empty response")
			}
			return fmt.Errorf("telegram sendMessage failed: %s", response.Description)
		}
		return nil
	}
	params := url.Values{"chat_id": []string{strconv.FormatInt(chatID, 10)}, "text": []string{text}}
	if threadID != nil && *threadID > 0 {
		params.Set("message_thread_id", strconv.Itoa(*threadID))
	}
	_, err := c.makeRequest(ctx, "sendMessage", params)
	return err
}

// BanChatMember permanently bans (and kicks) a user, revoking the messages
// they posted, mirroring the two dispatch paths used by the other moderation
// calls (library request vs context-aware raw request).
func (c *Client) BanChatMember(ctx context.Context, chatID, userID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.bot == nil {
		return errors.New("telegram client is nil")
	}
	if c.bot.Client == nil {
		return errors.New("telegram HTTP client is nil")
	}
	if c.apiEndpoint == "" {
		_, err := c.bot.Request(tgbotapi.BanChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
			RevokeMessages:   true,
		})
		return redactTelegramError(err, c.bot.Token)
	}
	_, err := c.makeRequest(ctx, "banChatMember", url.Values{
		"chat_id":         []string{strconv.FormatInt(chatID, 10)},
		"user_id":         []string{strconv.FormatInt(userID, 10)},
		"revoke_messages": []string{"true"},
	})
	return err
}

func (c *Client) IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c == nil || c.bot == nil {
		return false, errors.New("telegram client is nil")
	}
	if c.bot.Client == nil {
		return false, errors.New("telegram HTTP client is nil")
	}
	if c.apiEndpoint == "" {
		member, err := c.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: userID},
		})
		if err != nil {
			return false, redactTelegramError(err, c.bot.Token)
		}
		return member.Status == "creator" || member.Status == "owner" || member.Status == "administrator", nil
	}
	response, err := c.makeRequest(ctx, "getChatMember", url.Values{
		"chat_id": []string{strconv.FormatInt(chatID, 10)},
		"user_id": []string{strconv.FormatInt(userID, 10)},
	})
	if err != nil {
		return false, err
	}
	var member tgbotapi.ChatMember
	if err := json.Unmarshal(response.Result, &member); err != nil {
		return false, fmt.Errorf("decode Telegram chat member: %w", err)
	}
	return member.Status == "creator" || member.Status == "owner" || member.Status == "administrator", nil
}

func (c *Client) makeRequest(ctx context.Context, method string, params url.Values) (*tgbotapi.APIResponse, error) {
	if c.apiEndpoint == "" {
		return nil, errors.New("telegram API endpoint is required for context-aware requests")
	}
	requestURL := fmt.Sprintf(c.apiEndpoint, c.bot.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, redactTelegramError(fmt.Errorf("create Telegram %s request: %w", method, err), c.bot.Token)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.bot.Client.Do(req)
	if err != nil {
		return nil, redactTelegramError(err, c.bot.Token)
	}
	defer resp.Body.Close()

	var response tgbotapi.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Telegram %s response: %w", method, err)
	}
	if !response.Ok {
		var parameters tgbotapi.ResponseParameters
		if response.Parameters != nil {
			parameters = *response.Parameters
		}
		return nil, tgbotapi.Error{Code: response.ErrorCode, Message: response.Description, ResponseParameters: parameters}
	}
	return &response, nil
}

// redactTelegramError keeps cancellation and timeout matching intact while
// removing the bot token that net/http includes in URL-related errors.
func redactTelegramError(err error, token string) error {
	if err == nil || token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return &redactedError{message: strings.ReplaceAll(err.Error(), token, "<redacted>"), cause: err}
}

// RedactTokenError is provided for startup and integration errors that occur
// outside Client methods but may still include the Bot API URL.
func RedactTokenError(err error, token string) error {
	return redactTelegramError(err, token)
}

type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.cause }

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

// isHandledChatType reports whether a message from this chat type should reach
// the moderation service. Groups are moderated; private chats are additionally
// accepted so /start and /help work in direct messages. Channel posts and other
// chat kinds are ignored.
func isHandledChatType(chatType string) bool {
	return chatType == "group" || chatType == "supergroup" || chatType == "private"
}

// FromMessage converts a Telegram message. threadID is optional because the
// current upstream client does not expose message_thread_id on Message.
func FromMessage(message *tgbotapi.Message, threadID *int) (domain.ModerationMessage, bool) {
	if message == nil || message.Chat == nil || !isHandledChatType(message.Chat.Type) {
		return domain.ModerationMessage{}, false
	}
	var userID *int64
	userIsBot := false
	if message.From != nil {
		id := message.From.ID
		userID = &id
		userIsBot = message.From.IsBot
	}
	text, entities := activeTextAndEntities(message.Text, message.Caption, message.Entities, message.CaptionEntities)
	msg := domain.ModerationMessage{
		ChatID:          message.Chat.ID,
		ChatTitle:       message.Chat.Title,
		ChatType:        message.Chat.Type,
		MessageID:       message.MessageID,
		MessageThreadID: threadID,
		UserID:          userID,
		UserIsBot:       userIsBot,
		Text:            message.Text,
		Caption:         message.Caption,
		Entities:        messageEntities(text, entities),
	}
	// The typed message keeps forward_from/forward_from_chat but not
	// forward_origin; the raw polling path carries the richer origin.
	msg.Forward = forwardInfoFrom(nil, message.ForwardFrom, message.ForwardFromChat)
	return msg, true
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
	MessageID       int                      `json:"message_id"`
	MessageThreadID *int                     `json:"message_thread_id,omitempty"`
	From            *tgbotapi.User           `json:"from,omitempty"`
	Chat            *tgbotapi.Chat           `json:"chat"`
	Text            string                   `json:"text,omitempty"`
	Caption         string                   `json:"caption,omitempty"`
	Entities        []tgbotapi.MessageEntity `json:"entities,omitempty"`
	CaptionEntities []tgbotapi.MessageEntity `json:"caption_entities,omitempty"`
	// forward_origin only exists in the raw payload: the upstream library
	// version does not model it, so the typed path falls back to
	// forward_from / forward_from_chat.
	ForwardOrigin   *rawForwardOrigin `json:"forward_origin,omitempty"`
	ForwardFrom     *tgbotapi.User    `json:"forward_from,omitempty"`
	ForwardFromChat *tgbotapi.Chat    `json:"forward_from_chat,omitempty"`
}

// rawForwardOrigin mirrors Telegram's forward_origin object, whose type field
// discriminates "user", "hidden_user", "channel" and "chat" origins.
type rawForwardOrigin struct {
	Type       string         `json:"type"`
	Date       int64          `json:"date"`
	SenderUser *tgbotapi.User `json:"sender_user,omitempty"`
	SenderName string         `json:"sender_user_name,omitempty"`
	SenderChat *tgbotapi.Chat `json:"sender_chat,omitempty"`
	Chat       *tgbotapi.Chat `json:"chat,omitempty"`
	MessageID  int            `json:"message_id,omitempty"`
}

// activeTextAndEntities picks the text the format entities describe: Telegram
// attaches entities to the message text and caption_entities to the caption.
func activeTextAndEntities(text, caption string, entities, captionEntities []tgbotapi.MessageEntity) (string, []tgbotapi.MessageEntity) {
	if text != "" {
		return text, entities
	}
	return caption, captionEntities
}

// messageEntities reduces library entities to the domain subset the built-in
// ad filter needs. Mention usernames are resolved from the message text via
// their UTF-16 offsets; bot usernames are ASCII so the alignment holds, and
// text_mention entities carry the full user (including the bot flag).
func messageEntities(text string, entities []tgbotapi.MessageEntity) []domain.MessageEntityInfo {
	if len(entities) == 0 {
		return nil
	}
	out := make([]domain.MessageEntityInfo, 0, len(entities))
	for _, e := range entities {
		info := domain.MessageEntityInfo{Type: e.Type}
		switch e.Type {
		case "mention":
			info.Username = strings.TrimPrefix(utf16Slice(text, e.Offset, e.Length), "@")
		case "text_mention":
			if e.User != nil {
				info.Username = e.User.UserName
				info.IsBot = e.User.IsBot
			}
		case "url", "text_link":
			info.HasURL = true
		}
		out = append(out, info)
	}
	return out
}

// utf16Slice decodes the UTF-16 code-unit range [offset, offset+length) that
// Telegram uses for entity offsets into a Go substring.
func utf16Slice(value string, offset, length int) string {
	runes := []rune(value)
	start := utf16OffsetToRune(runes, offset)
	end := utf16OffsetToRune(runes, offset+length)
	if start > len(runes) {
		return ""
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}

func utf16OffsetToRune(runes []rune, target int) int {
	units := 0
	for i, r := range runes {
		if units >= target {
			return i
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return len(runes)
}

// forwardInfoFrom prefers Telegram's forward_origin (raw path) and falls back
// to the legacy forward_from/forward_from_chat fields.
func forwardInfoFrom(origin *rawForwardOrigin, from *tgbotapi.User, fromChat *tgbotapi.Chat) *domain.ForwardInfo {
	if origin != nil {
		info := &domain.ForwardInfo{Type: origin.Type}
		switch {
		case origin.Chat != nil:
			info.SourceID, info.SourceTitle = origin.Chat.ID, origin.Chat.Title
		case origin.SenderChat != nil:
			info.SourceID, info.SourceTitle = origin.SenderChat.ID, origin.SenderChat.Title
		case origin.SenderUser != nil:
			info.SourceID, info.SourceTitle = origin.SenderUser.ID, origin.SenderUser.FirstName+" "+origin.SenderUser.LastName
		}
		return info
	}
	if fromChat != nil {
		return &domain.ForwardInfo{Type: fromChat.Type, SourceID: fromChat.ID, SourceTitle: fromChat.Title}
	}
	if from != nil {
		return &domain.ForwardInfo{Type: "user", SourceID: from.ID, SourceTitle: strings.TrimSpace(from.FirstName + " " + from.LastName)}
	}
	return nil
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
	if message == nil || message.Chat == nil || !isHandledChatType(message.Chat.Type) {
		return domain.ModerationMessage{}, false, nil
	}
	var userID *int64
	userIsBot := false
	if message.From != nil {
		id := message.From.ID
		userID = &id
		userIsBot = message.From.IsBot
	}
	text, entities := activeTextAndEntities(message.Text, message.Caption, message.Entities, message.CaptionEntities)
	msg := domain.ModerationMessage{
		ChatID: message.Chat.ID, ChatTitle: message.Chat.Title, ChatType: message.Chat.Type,
		MessageID: message.MessageID, MessageThreadID: message.MessageThreadID,
		UserID: userID, UserIsBot: userIsBot, Text: message.Text, Caption: message.Caption,
		Entities: messageEntities(text, entities),
	}
	msg.Forward = forwardInfoFrom(message.ForwardOrigin, message.ForwardFrom, message.ForwardFromChat)
	return msg, true, nil
}
