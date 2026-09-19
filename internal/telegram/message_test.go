package telegram

import (
	"encoding/json"
	"reflect"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestMessageConversionRetainsLinkTargetsAndButtons(t *testing.T) {
	const visibleURL = "https://example.org/sale"
	const hiddenURL = "tg://resolve?domain=example_contact"
	const content = "😀 立即下单 " + visibleURL
	buttonURL := "https://t.me/example_shop"
	callbackData := "not visible advertising content"
	entities := []tgbotapi.MessageEntity{
		{Type: "text_link", Offset: 3, Length: 4, URL: hiddenURL},
		{Type: "url", Offset: 8, Length: len(visibleURL)},
		{Type: "blockquote", Offset: 3, Length: 4},
	}
	wantEntities := []domain.MessageEntityInfo{
		{Type: "text_link", HasURL: true, URL: hiddenURL, Offset: 3, Length: 4},
		{Type: "url", HasURL: true, URL: visibleURL, Offset: 8, Length: len(visibleURL)},
		{Type: "blockquote", Offset: 3, Length: 4},
	}
	wantButtons := []domain.InlineButtonInfo{
		{Text: "联系客服", URL: buttonURL},
		{Text: "登录", URL: "https://example.org/login"},
		{Text: "帮助"},
	}
	for _, source := range []string{"text", "caption"} {
		for _, path := range []string{"sdk", "raw", "raw_edit"} {
			t.Run(source+"/"+path, func(t *testing.T) {
				message := &tgbotapi.Message{
					MessageID: 10,
					From:      &tgbotapi.User{ID: 42},
					Chat:      &tgbotapi.Chat{ID: -100, Type: "supergroup"},
					ReplyMarkup: &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
						{{Text: "联系客服", URL: &buttonURL}, {Text: "登录", LoginURL: &tgbotapi.LoginURL{URL: "https://example.org/login"}}},
						{{Text: "帮助", CallbackData: &callbackData}},
					}},
				}
				if source == "text" {
					message.Text, message.Entities = content, entities
				} else {
					message.Caption, message.CaptionEntities = content, entities
				}
				converted := convertTestMessage(t, path, message)
				if converted.Content() != content || !reflect.DeepEqual(converted.Entities, wantEntities) {
					t.Fatalf("content or entities changed: %+v", converted)
				}
				if !reflect.DeepEqual(converted.InlineButtons, wantButtons) {
					t.Fatalf("buttons = %+v, want %+v", converted.InlineButtons, wantButtons)
				}
			})
		}
	}
}

func TestMessageConversionIgnoresInvalidEntityRanges(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	message := &tgbotapi.Message{
		MessageID: 10,
		Chat:      &tgbotapi.Chat{ID: -100, Type: "supergroup"},
		Text:      "😀 @example_bot",
		Entities: []tgbotapi.MessageEntity{
			{Type: "mention", Offset: 3, Length: len("@example_bot")},
			{Type: "text_link", Offset: -1, Length: 4, URL: "https://example.org"},
			{Type: "url", Offset: 3, Length: -1},
			{Type: "url", Offset: maxInt, Length: maxInt},
			{Type: "mention", Offset: 3, Length: 100},
			{Type: "text_mention", Offset: 0, Length: 1, User: &tgbotapi.User{ID: 7, IsBot: true}},
			{Type: "text_link", Offset: 1, Length: 1, URL: "https://example.org"},
			{Type: "url", Offset: 3, Length: 0},
		},
	}
	for _, path := range []string{"sdk", "raw"} {
		t.Run(path, func(t *testing.T) {
			converted := convertTestMessage(t, path, message)
			if len(converted.Entities) != 1 || converted.Entities[0].Username != "example_bot" {
				t.Fatalf("invalid entities were retained: %+v", converted.Entities)
			}
		})
	}
}

func TestMessageConversionKeepsButtonsWithoutText(t *testing.T) {
	buttonURL := "https://t.me/example_contact"
	message := &tgbotapi.Message{
		MessageID: 11,
		Chat:      &tgbotapi.Chat{ID: -100, Type: "supergroup"},
		ReplyMarkup: &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			{{Text: "联系客服", URL: &buttonURL}},
		}},
	}
	for _, path := range []string{"sdk", "raw_edit"} {
		t.Run(path, func(t *testing.T) {
			converted := convertTestMessage(t, path, message)
			if converted.Content() != "" || len(converted.InlineButtons) != 1 || converted.InlineButtons[0].URL != buttonURL {
				t.Fatalf("button-only message was lost: %+v", converted)
			}
		})
	}
}

func TestParseUpdateRetainsHiddenForwardNameAndOriginPriority(t *testing.T) {
	payload := []byte(`{"message":{
		"message_id":3,"chat":{"id":-100,"type":"supergroup"},"text":"新闻转发",
		"forward_origin":{"type":"hidden_user","date":1700000000,"sender_user_name":"原发言者"},
		"forward_from":{"id":42,"first_name":"Legacy"}
	}}`)
	message, ok, err := ParseUpdate(payload)
	if err != nil || !ok || message.Forward == nil {
		t.Fatalf("ParseUpdate = %+v, %v, %v", message, ok, err)
	}
	if *message.Forward != (domain.ForwardInfo{Type: "hidden_user", SourceTitle: "原发言者"}) {
		t.Fatalf("unexpected origin: %+v", message.Forward)
	}
}

func convertTestMessage(t *testing.T, path string, message *tgbotapi.Message) domain.ModerationMessage {
	t.Helper()
	if path == "sdk" {
		converted, ok := FromMessage(message, nil)
		if !ok {
			t.Fatal("FromMessage rejected message")
		}
		return converted
	}
	field := "message"
	if path == "raw_edit" {
		field = "edited_message"
	}
	encoded, err := json.Marshal(map[string]*tgbotapi.Message{field: message})
	if err != nil {
		t.Fatal(err)
	}
	converted, ok, err := ParseUpdate(encoded)
	if err != nil || !ok {
		t.Fatalf("ParseUpdate = %+v, %v, %v", converted, ok, err)
	}
	return converted
}
