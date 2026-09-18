package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
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

	// WebUIAddr is the panel listen address, e.g. "0.0.0.0:8080" or "127.0.0.1:8080".
	// Empty disables the panel (the default), so existing deployments upgrade
	// without any new required variables.
	WebUIAddr string
	// WebUIUsername and WebUIPassword are required whenever WebUIAddr is set.
	WebUIUsername string
	WebUIPassword string
	// WebUISessionSecret signs session cookies. Optional: when unset, a fresh
	// random key is generated at startup, invalidating all sessions on restart.
	WebUISessionSecret string

	// AdFilterEnabled is the initial master switch for the shipped-in filter.
	// Persisted panel settings take precedence once an administrator saves them.
	// It defaults to true so protection is on out of the box.
	AdFilterEnabled bool

	// SpamStrikeLimit is the number of ad hits (per user, per chat, within
	// SpamStrikeWindow, built-in or user-rule) that permanently bans the user.
	SpamStrikeLimit int
	// SpamStrikeWindow bounds the strike counting window.
	SpamStrikeWindow time.Duration
}

// WebUIEnabled reports whether the panel HTTP server should be started.
func (c Config) WebUIEnabled() bool { return c.WebUIAddr != "" }

func Load() (Config, error) {
	cfg := Config{
		BotToken:            os.Getenv("BOT_TOKEN"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		TelegramAPIEndpoint: os.Getenv("TELEGRAM_API_ENDPOINT"),
		TelegramHTTPTimeout: defaultTelegramHTTPTimeout,
		LogLevel:            os.Getenv("LOG_LEVEL"),
		WebUIAddr:           os.Getenv("WEBUI_ADDR"),
		WebUIUsername:       os.Getenv("WEBUI_USERNAME"),
		WebUIPassword:       os.Getenv("WEBUI_PASSWORD"),
		WebUISessionSecret:  os.Getenv("WEBUI_SESSION_SECRET"),
		AdFilterEnabled:     parseAdFilterEnabled(),
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
	cfg.SpamStrikeLimit = intEnv("SPAM_STRIKE_LIMIT", 3)
	cfg.SpamStrikeWindow = 24 * time.Hour
	if raw := os.Getenv("SPAM_STRIKE_WINDOW"); raw != "" {
		if window, err := time.ParseDuration(raw); err == nil && window > 0 {
			cfg.SpamStrikeWindow = window
		}
	}
	if err := validateWebUI(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

var webUIUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func validateWebUI(cfg Config) error {
	if !cfg.WebUIEnabled() {
		return nil
	}
	host, portStr, err := net.SplitHostPort(cfg.WebUIAddr)
	if err != nil {
		return fmt.Errorf("WEBUI_ADDR must be a host:port address: %q", cfg.WebUIAddr)
	}
	if host == "" {
		return fmt.Errorf("WEBUI_ADDR must include a listen host (e.g. 127.0.0.1:8080), got %q", cfg.WebUIAddr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("WEBUI_ADDR port must be between 1 and 65535: %q", cfg.WebUIAddr)
	}
	if cfg.WebUIUsername == "" || cfg.WebUIPassword == "" {
		return fmt.Errorf("WEBUI_ADDR is set so WEBUI_USERNAME and WEBUI_PASSWORD must both be set (leave WEBUI_ADDR empty to disable the panel)")
	}
	if !webUIUsernamePattern.MatchString(cfg.WebUIUsername) {
		return fmt.Errorf("WEBUI_USERNAME may only contain A-Z a-z 0-9 _ . - (1-64 characters)")
	}
	return nil
}

// parseAdFilterEnabled reads ADFILTER_ENABLED. It defaults to true; an
// unparsable value also stays enabled so a config typo never silently
// disables spam protection.
func parseAdFilterEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("ADFILTER_ENABLED"))
	if raw == "" {
		return true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return value
}

// intEnv reads a positive integer env var, falling back to def for missing,
// empty or invalid values (clamped to at least 1).
func intEnv(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return def
	}
	return value
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
