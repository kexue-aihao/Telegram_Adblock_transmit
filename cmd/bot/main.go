package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/config"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/moderation"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/retention"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/telegram"
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
	cache := rules.NewMemoryCache()
	botAPI, err := tgbotapi.NewBotAPIWithAPIEndpoint(cfg.BotToken, cfg.TelegramAPIEndpoint)
	if err != nil {
		return fmt.Errorf("initialize Telegram bot: %w", err)
	}
	telegramClient := telegram.NewClient(botAPI)
	service := moderation.NewService(ruleStore, cache, auditStore, telegramClient, logger)
	if err := service.LoadCache(ctx); err != nil {
		return fmt.Errorf("load moderation rules: %w", err)
	}

	go retention.Run(ctx, auditStore, logger)
	logger.Info("telegram moderation bot started", "bot_username", botAPI.Self.UserName)
	poller := &telegram.Poller{Bot: botAPI, Timeout: 10, Logger: logger}
	err = poller.Run(ctx, func(messageCtx context.Context, message domain.ModerationMessage) error {
		_, handleErr := service.HandleUpdate(messageCtx, message)
		return handleErr
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
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
