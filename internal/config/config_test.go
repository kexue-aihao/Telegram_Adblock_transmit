package config

import "testing"

func TestLoadUsesOfficialTelegramEndpointByDefault(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_API_ENDPOINT", "")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramAPIEndpoint != "https://api.telegram.org/bot%s/%s" {
		t.Fatalf("default endpoint = %q", cfg.TelegramAPIEndpoint)
	}
}

func TestLoadAcceptsSelfHostedTelegramEndpoint(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_API_ENDPOINT", "http://telegram-bot-api:8081/bot%s/%s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramAPIEndpoint != "http://telegram-bot-api:8081/bot%s/%s" {
		t.Fatalf("custom endpoint = %q", cfg.TelegramAPIEndpoint)
	}
}

func TestLoadRejectsInvalidTelegramEndpoint(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	for _, endpoint := range []string{
		"http://telegram-bot-api:8081/bot%s",
		"ftp://telegram-bot-api:8081/bot%s/%s",
		"telegram-bot-api:8081/bot%s/%s",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Setenv("TELEGRAM_API_ENDPOINT", endpoint)
			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted invalid Telegram endpoint")
			}
		})
	}
}
