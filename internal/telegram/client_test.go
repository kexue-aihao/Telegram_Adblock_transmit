package telegram

import (
	"encoding/json"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
