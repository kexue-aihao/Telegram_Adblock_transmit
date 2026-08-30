package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultTelegramHTTPTimeout = 30 * time.Second

type Config struct {
	BotToken            string
	DatabaseURL         string
	TelegramAPIEndpoint string
	TelegramHTTPTimeout time.Duration
	LogLevel            string
}

func Load() (Config, error) {
	cfg := Config{
		BotToken:            os.Getenv("BOT_TOKEN"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		TelegramAPIEndpoint: os.Getenv("TELEGRAM_API_ENDPOINT"),
		TelegramHTTPTimeout: defaultTelegramHTTPTimeout,
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
	if rawTimeout := os.Getenv("TELEGRAM_HTTP_TIMEOUT"); rawTimeout != "" {
		timeout, err := time.ParseDuration(rawTimeout)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_HTTP_TIMEOUT must be a positive duration: %q", rawTimeout)
		}
		cfg.TelegramHTTPTimeout = timeout
	}
	allowInsecureHTTP, err := parseBoolEnv("TELEGRAM_ALLOW_INSECURE_HTTP")
	if err != nil {
		return Config{}, err
	}
	if err := validateTelegramAPIEndpoint(cfg.TelegramAPIEndpoint, allowInsecureHTTP); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "INFO"
	}
	return cfg, nil
}

func parseBoolEnv(name string) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return value, nil
}

func validateTelegramAPIEndpoint(endpoint string, allowInsecureHTTP bool) error {
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
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT must use https or explicitly allowed http")
	}
	if !allowInsecureHTTP {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT uses http; set TELEGRAM_ALLOW_INSECURE_HTTP=true only for a local or Docker-network endpoint")
	}
	if !isLocalOrDockerHost(parsed.Hostname()) {
		return fmt.Errorf("TELEGRAM_API_ENDPOINT http host must be loopback or a private Docker service")
	}
	return nil
}

func isLocalOrDockerHost(host string) bool {
	if host == "localhost" || host == "host.docker.internal" || host == "gateway.docker.internal" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	// Docker Compose service names are single DNS labels, unlike public hosts.
	return host != "" && !strings.Contains(host, ".") && !strings.Contains(host, ":")
}
