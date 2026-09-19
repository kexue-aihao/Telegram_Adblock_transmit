package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/config"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/moderation"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/profile"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/retention"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/telegram"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/webui"
	"github.com/kexue-aihao/telegram-adblock-transmit/migrations"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		return err
	}

	ruleStore := store.NewRuleRepository(pool)
	auditStore := store.NewAuditRepository(pool)
	builtinFilter, err := builtin.NewManaged(ctx, cfg.AdFilterEnabled, store.NewBuiltinSettingsRepository(pool))
	if err != nil {
		return err
	}
	// Runtime switches. Environment values apply until an administrator saves
	// them from the panel or from the /settings command.
	botSettings, err := settings.NewManager(ctx, domain.BotSettings{
		BioCheckEnabled: cfg.BioCheckEnabled,
		OwnerUserIDs:    cfg.BotOwnerIDs,
	}, store.NewBotSettingsRepository(pool))
	if err != nil {
		return err
	}
	cache := rules.NewMemoryCache()
	httpClient := &http.Client{Timeout: cfg.TelegramHTTPTimeout}
	botAPI, err := tgbotapi.NewBotAPIWithClient(cfg.BotToken, cfg.TelegramAPIEndpoint, httpClient)
	if err != nil {
		return fmt.Errorf("initialize Telegram bot: %w", telegram.RedactTokenError(err, cfg.BotToken))
	}
	telegramClient := telegram.NewClientWithAPIEndpoint(botAPI, cfg.TelegramAPIEndpoint)
	service := moderation.NewService(ruleStore, cache, auditStore, telegramClient, logger)
	service.SetBotUsername(botAPI.Self.UserName)
	service.SetBuiltinFilter(builtinFilter)
	// The reader is always attached so the panel and /settings can switch the
	// check on at runtime; no profile request is made while it is off.
	service.SetUserProfileReader(profile.New(telegramClient, logger))
	service.SetBotSettings(botSettings)
	current := botSettings.Settings()
	logger.Info("runtime settings loaded", "bio_check", current.BioCheckEnabled,
		"cross_group_management", current.CrossGroupManagement, "owners", len(current.OwnerUserIDs))
	service.SetSpamPolicy(cfg.SpamStrikeLimit, cfg.SpamStrikeWindow)
	service.SetNoticeTTL(cfg.NoticeTTL)
	logger.Info("deletion notice policy configured", "notice_ttl", cfg.NoticeTTL)
	if err := service.LoadCache(ctx); err != nil {
		return fmt.Errorf("load moderation rules: %w", err)
	}
	if err := registerBotCommands(botAPI, logger, cfg.BotToken); err != nil {
		logger.Warn("unable to register bot command menu", "error", err)
	}

	if cfg.WebUIEnabled() {
		settingsStore := store.NewPanelSettingsRepository(pool)
		panel, err := webui.New(webui.Options{
			Addr:          cfg.WebUIAddr,
			RuleStore:     ruleStore,
			ChatStore:     ruleStore,
			AuditStore:    auditStore,
			Refresher:     service,
			SettingsStore: settingsStore,
			BuiltinFilter: builtinFilter,
			BotSettings:   botSettings,
			Username:      cfg.WebUIUsername,
			Password:      cfg.WebUIPassword,
			SessionSecret: []byte(cfg.WebUISessionSecret),
			Logger:        logger,
		})
		if err != nil {
			return err
		}
		go func() {
			if runErr := panel.Run(ctx); runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
				logger.Error("webui panel server failed", "addr", cfg.WebUIAddr, "error", runErr)
			}
		}()
		logger.Info("webui panel started", "addr", cfg.WebUIAddr)
	}

	go retention.Run(ctx, auditStore, logger)
	logger.Info("telegram moderation bot started", "bot_username", botAPI.Self.UserName)
	poller := &telegram.Poller{Bot: botAPI, APIEndpoint: cfg.TelegramAPIEndpoint, Timeout: 10, Logger: logger}
	err = poller.Run(ctx, func(messageCtx context.Context, message domain.ModerationMessage) error {
		_, handleErr := service.HandleUpdate(messageCtx, message)
		return handleErr
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// registerBotCommands publishes the command menu via setMyCommands so the
// Telegram "/" button shows available commands. It is idempotent and re-applied
// on every start. Only a warning is returned so a menu failure never blocks
// the poller.
func registerBotCommands(botAPI *tgbotapi.BotAPI, logger *slog.Logger, token string) error {
	commands := make([]tgbotapi.BotCommand, 0, len(moderation.BotMenu))
	for _, info := range moderation.BotMenu {
		commands = append(commands, tgbotapi.BotCommand{Command: info.Name, Description: info.Description})
	}
	_, err := botAPI.Request(tgbotapi.NewSetMyCommands(commands...))
	logger.Info("bot command menu registered", "commands", len(commands))
	return telegram.RedactTokenError(err, token)
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch strings.ToUpper(level) {
	case "DEBUG":
		slogLevel = slog.LevelDebug
	case "WARN", "WARNING":
		slogLevel = slog.LevelWarn
	case "ERROR":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel, AddSource: false, ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.TimeKey {
			attr.Value = slog.TimeValue(attr.Value.Time().UTC().Truncate(time.Millisecond))
		}
		return attr
	}}))
}
