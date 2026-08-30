package config

import (
	"testing"
	"time"
)

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
	if cfg.TelegramHTTPTimeout != 30*time.Second {
		t.Fatalf("default timeout = %s", cfg.TelegramHTTPTimeout)
	}
}

func TestLoadAcceptsSelfHostedTelegramEndpoint(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_API_ENDPOINT", "http://telegram-bot-api:8081/bot%s/%s")
	t.Setenv("TELEGRAM_ALLOW_INSECURE_HTTP", "true")
	t.Setenv("TELEGRAM_HTTP_TIMEOUT", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramAPIEndpoint != "http://telegram-bot-api:8081/bot%s/%s" {
		t.Fatalf("custom endpoint = %q", cfg.TelegramAPIEndpoint)
	}
	if cfg.TelegramHTTPTimeout != 45*time.Second {
		t.Fatalf("custom timeout = %s", cfg.TelegramHTTPTimeout)
	}
}

func TestLoadRejectsInsecureTelegramEndpointUnlessExplicitlyAllowed(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_API_ENDPOINT", "http://localhost:8081/bot%s/%s")
	t.Setenv("TELEGRAM_ALLOW_INSECURE_HTTP", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted HTTP endpoint without explicit opt-in")
	}
}

func TestLoadRejectsPublicInsecureTelegramEndpoint(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_API_ENDPOINT", "http://telegram-api.example.com/bot%s/%s")
	t.Setenv("TELEGRAM_ALLOW_INSECURE_HTTP", "true")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted public HTTP endpoint")
	}
}

func TestLoadRejectsInvalidTelegramHTTPTimeout(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("TELEGRAM_HTTP_TIMEOUT", "0s")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted zero HTTP timeout")
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
