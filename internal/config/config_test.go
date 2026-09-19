package config

import (
	"testing"
	"time"
)

func TestBioCheckOptIn(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	for _, tc := range []struct {
		value            string
		enabled, invalid bool
	}{
		{"", false, false}, {"false", false, false}, {"true", true, false}, {"typo", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("BIO_CHECK_ENABLED", tc.value)
			cfg, err := Load()
			if (err != nil) != tc.invalid || cfg.BioCheckEnabled != tc.enabled {
				t.Fatalf("BIO_CHECK_ENABLED=%q: enabled %v, error %v", tc.value, cfg.BioCheckEnabled, err)
			}
		})
	}
}

func TestNoticeTTLDefaultAndOverride(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("NOTICE_TTL", "")
	cfg, err := Load()
	if err != nil || cfg.NoticeTTL != 10*time.Second {
		t.Fatalf("default NOTICE_TTL = %v, error %v", cfg.NoticeTTL, err)
	}
	for _, tc := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"30s", 30 * time.Second, false},
		{"0", 0, false},
		{"2m", 2 * time.Minute, false},
		{"-5s", 0, true},
		{"soon", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("NOTICE_TTL", tc.value)
			cfg, err := Load()
			if (err != nil) != tc.invalid {
				t.Fatalf("NOTICE_TTL=%q error = %v", tc.value, err)
			}
			if !tc.invalid && cfg.NoticeTTL != tc.want {
				t.Fatalf("NOTICE_TTL=%q = %v, want %v", tc.value, cfg.NoticeTTL, tc.want)
			}
		})
	}
}

func TestBotOwnerIDsParsing(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	for _, tc := range []struct {
		value   string
		want    []int64
		invalid bool
	}{
		{"", nil, false},
		{"123456789", []int64{123456789}, false},
		{"123456789, 987654321", []int64{123456789, 987654321}, false},
		{"123456789;987654321 555", []int64{123456789, 987654321, 555}, false},
		{"abc", nil, true},
		{"0", nil, true},
		{"-5", nil, true},
		{"123,abc", nil, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("BOT_OWNER_IDS", tc.value)
			cfg, err := Load()
			if (err != nil) != tc.invalid {
				t.Fatalf("BOT_OWNER_IDS=%q: error = %v", tc.value, err)
			}
			if len(cfg.BotOwnerIDs) != len(tc.want) {
				t.Fatalf("BOT_OWNER_IDS=%q parsed %v, want %v", tc.value, cfg.BotOwnerIDs, tc.want)
			}
			for i, id := range tc.want {
				if cfg.BotOwnerIDs[i] != id {
					t.Fatalf("BOT_OWNER_IDS=%q parsed %v, want %v", tc.value, cfg.BotOwnerIDs, tc.want)
				}
			}
		})
	}
}

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

func TestLoadWebUIIsDisabledByDefault(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("WEBUI_ADDR", "")
	t.Setenv("WEBUI_USERNAME", "")
	t.Setenv("WEBUI_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebUIEnabled() {
		t.Fatal("WebUI should be disabled by default (empty WEBUI_ADDR)")
	}
}

func TestLoadWebUIEnabledRequiresCredentials(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("WEBUI_ADDR", "0.0.0.0:8080")
	t.Setenv("WEBUI_USERNAME", "")
	t.Setenv("WEBUI_PASSWORD", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted WEBUI_ADDR without username/password")
	}
}

func TestLoadWebUIEnabledAcceptsValidCredentials(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("WEBUI_ADDR", "127.0.0.1:8080")
	t.Setenv("WEBUI_USERNAME", "admin")
	t.Setenv("WEBUI_PASSWORD", "s3cret")
	t.Setenv("WEBUI_SESSION_SECRET", "0123456789abcdef")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WebUIEnabled() {
		t.Fatal("WebUI should be enabled with WEBUI_ADDR set")
	}
	if cfg.WebUIUsername != "admin" {
		t.Fatalf("username = %q", cfg.WebUIUsername)
	}
}

func TestLoadWebUIRejectsInvalidAddress(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("WEBUI_USERNAME", "admin")
	t.Setenv("WEBUI_PASSWORD", "s3cret")
	for _, addr := range []string{"8080", "0.0.0.0", "0.0.0.0:99999", "0.0.0.0:0"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("WEBUI_ADDR", addr)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted invalid WEBUI_ADDR %q", addr)
			}
		})
	}
}

func TestLoadWebUIRejectsInvalidUsername(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("WEBUI_ADDR", "0.0.0.0:8080")
	t.Setenv("WEBUI_USERNAME", "bad user!")
	t.Setenv("WEBUI_PASSWORD", "s3cret")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted WEBUI_USERNAME with invalid characters")
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

func TestParseAdFilterEnabledDefaultsToOn(t *testing.T) {
	t.Setenv("ADFILTER_ENABLED", "")
	if !parseAdFilterEnabled() {
		t.Fatal("unset ADFILTER_ENABLED should default to enabled")
	}
	t.Setenv("ADFILTER_ENABLED", "false")
	if parseAdFilterEnabled() {
		t.Fatal("ADFILTER_ENABLED=false should disable")
	}
	t.Setenv("ADFILTER_ENABLED", "maybe")
	if !parseAdFilterEnabled() {
		t.Fatal("a typo must never silently disable the ad filter")
	}
}

func TestLoadSpamStrikeDefaultsAndOverrides(t *testing.T) {
	t.Setenv("BOT_TOKEN", "token")
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("SPAM_STRIKE_LIMIT", "")
	t.Setenv("SPAM_STRIKE_WINDOW", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpamStrikeLimit != 3 || cfg.SpamStrikeWindow != 24*time.Hour {
		t.Fatalf("defaults = limit %d window %s", cfg.SpamStrikeLimit, cfg.SpamStrikeWindow)
	}
	t.Setenv("SPAM_STRIKE_LIMIT", "5")
	t.Setenv("SPAM_STRIKE_WINDOW", "6h")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpamStrikeLimit != 5 || cfg.SpamStrikeWindow != 6*time.Hour {
		t.Fatalf("overrides = limit %d window %s", cfg.SpamStrikeLimit, cfg.SpamStrikeWindow)
	}
	t.Setenv("SPAM_STRIKE_LIMIT", "bogus")
	t.Setenv("SPAM_STRIKE_WINDOW", "bogus")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpamStrikeLimit != 3 || cfg.SpamStrikeWindow != 24*time.Hour {
		t.Fatalf("invalid values should fall back: limit %d window %s", cfg.SpamStrikeLimit, cfg.SpamStrikeWindow)
	}
}
