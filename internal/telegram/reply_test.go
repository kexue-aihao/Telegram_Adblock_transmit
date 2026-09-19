package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// /rule_regex works from the quoted message, so the polling path must keep it.
func TestParseUpdateKeepsRepliedMessage(t *testing.T) {
	raw := []byte(`{
	  "message": {
	    "message_id": 10,
	    "chat": {"id": -100, "type": "supergroup", "title": "Test"},
	    "from": {"id": 42, "is_bot": false, "first_name": "Admin"},
	    "text": "/rule_regex",
	    "reply_to_message": {
	      "message_id": 9,
	      "chat": {"id": -100, "type": "supergroup", "title": "Test"},
	      "from": {"id": 7, "is_bot": true, "first_name": "Spam"},
	      "text": "专业广告代发，广告投放支付"
	    }
	  }
	}`)
	message, ok, err := ParseUpdate(raw)
	if err != nil || !ok {
		t.Fatalf("ParseUpdate() = %v, %v", ok, err)
	}
	if message.Reply == nil {
		t.Fatal("quoted message was dropped")
	}
	if message.Reply.MessageID != 9 || !strings.Contains(message.Reply.Content(), "广告代发") {
		t.Fatalf("unexpected quoted content: %+v", message.Reply)
	}
	if message.Reply.UserID == nil || *message.Reply.UserID != 7 || !message.Reply.SenderIsBot {
		t.Fatalf("quoted sender was lost: %+v", message.Reply)
	}
}

func TestParseUpdateWithoutReply(t *testing.T) {
	raw := []byte(`{
	  "message": {
	    "message_id": 10,
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/rule_regex"
	  }
	}`)
	message, ok, err := ParseUpdate(raw)
	if err != nil || !ok {
		t.Fatalf("ParseUpdate() = %v, %v", ok, err)
	}
	if message.Reply != nil {
		t.Fatalf("unexpected quoted message: %+v", message.Reply)
	}
}

func TestFromMessageKeepsRepliedCaption(t *testing.T) {
	quoted := &tgbotapi.Message{
		MessageID: 9,
		Caption:   "全网招代理，日入过万",
		From:      &tgbotapi.User{ID: 7, FirstName: "Spam"},
	}
	message := &tgbotapi.Message{
		MessageID:      10,
		Chat:           &tgbotapi.Chat{ID: -100, Type: "supergroup"},
		Text:           "/rule_regex",
		ReplyToMessage: quoted,
	}
	got, ok := FromMessage(message, nil)
	if !ok {
		t.Fatal("FromMessage rejected a supergroup message")
	}
	if got.Reply == nil || got.Reply.Content() != quoted.Caption {
		t.Fatalf("quoted caption was lost: %+v", got.Reply)
	}
	if got.Reply.SenderIsBot || got.Reply.UserID == nil || *got.Reply.UserID != 7 {
		t.Fatalf("quoted sender metadata is wrong: %+v", got.Reply)
	}
}
