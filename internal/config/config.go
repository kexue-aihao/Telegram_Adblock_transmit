package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	BotToken            string
	DatabaseURL         string
	TelegramAPIEndpoint string
	LogLevel            string
}

func Load() (Config, error) {
	cfg := Config{
		BotToken:            os.Getenv("BOT_TOKEN"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		TelegramAPIEndpoint: os.Getenv("TELEGRAM_API_ENDPOINT"),
		LogLevel:            os.Getenv("LOG_LEVEL"),
	}
	if cfg.BotToken == "" {
		return Config{}, fmt.Errorf("BOT_TOKEN must be set")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}
	if cfg.TelegramAPIEndpoint == "" {
		cfg.TelegramAPIEndpoint = "https://api.telegram.org/bot%s/%s"
	}
	if err := validateTelegramAPIEndpoint(cfg.TelegramAPIEndpoint); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}
	return cfg, nil
}

func validateTelegramAPIEndpoint(endpoint string) error {
	if strings.Count(endpoint, "%s") != 2 {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT must contain exactly two %%s placeholders (token and method)")
	}
	if strings.Contains(strings.ReplaceAll(endpoint, "%s", ""), "%") {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT contains an unsupported format placeholder")
	}
	probe := strings.Replace(endpoint, "%s", "placeholder", 2)
	parsed, err := url.Parse(probe)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT must use http or https")
	}
	return nil
}
