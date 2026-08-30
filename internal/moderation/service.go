// Package moderation implements command handling and the message moderation
// workflow. It deliberately depends only on ports, keeping Telegram and SQL
// details outside the policy layer.
package moderation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
)

const (
	ModerationNotice = "该消息因匹配广告规则已删除。"
	PermissionNotice = "仅本群管理员可以管理广告规则。"
	DefaultLogLimit  = 10
	MaxLogLimit      = 20
	messageChunkSize = 3800
)

var managementCommands = map[string]struct{}{
	"rule_add": {}, "rule_list": {}, "rule_remove": {}, "rule_enable": {},
	"rule_disable": {}, "rule_test": {}, "adlog": {},
}

// Service coordinates rule storage, the compiled rule cache, audit storage,
// and Telegram side effects.
type Service struct {
	ruleStore ports.RuleStore
	cache     ports.RuleCache
	audit     ports.AuditStore
	telegram  ports.TelegramClient
	logger    *slog.Logger
	clock     func() time.Time
}

// NewService builds a moderation service. A nil logger falls back to the
// process default so tests and small integrations do not need logging setup.
func NewService(ruleStore ports.RuleStore, cache ports.RuleCache, audit ports.AuditStore, telegram ports.TelegramClient, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{ruleStore: ruleStore, cache: cache, audit: audit, telegram: telegram, logger: logger, clock: time.Now}
}

// NewProcessor is retained as a descriptive alias for callers migrating from
// the Python ModerationProcessor terminology.
func NewProcessor(ruleStore ports.RuleStore, cache ports.RuleCache, audit ports.AuditStore, telegram ports.TelegramClient, logger *slog.Logger) *Service {
	return NewService(ruleStore, cache, audit, telegram, logger)
}

func IsSupportedGroup(message domain.ModerationMessage) bool {
	return message.ChatType == "group" || message.ChatType == "supergroup"
}

// ExtractContent gives text precedence over a media caption, matching Telegram
// semantics and the original service.
func ExtractContent(message domain.ModerationMessage) string { return message.Content() }

// IsManagementCommand reports whether content starts with one of the supported
// bot commands. Usernames after @ are ignored, case-insensitively.
func IsManagementCommand(content string) bool {
	name, _, ok := ParseCommand(content)
	if !ok {
		return false
	}
	_, exists := managementCommands[name]
	return exists
}

// ParseCommand returns a normalized command name and the unmodified argument
// text. It accepts Telegram's /command@bot form and ignores leading spaces.
func ParseCommand(content string) (name, args string, ok bool) {
	content = strings.TrimSpace(content)
	if content == "" || content[0] != '/' {
		return "", "", false
	}
	body := content[1:]
	separator := strings.IndexFunc(body, unicode.IsSpace)
	first, rest := body, ""
	if separator >= 0 {
		first, rest = body[:separator], body[separator:]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return "", "", false
	}
	if at := strings.IndexByte(first, '@'); at >= 0 {
		first = first[:at]
	}
	return strings.ToLower(first), strings.TrimSpace(rest), true
}

// HandleUpdate is the normal entry point for a converted Telegram update.
// It handles a recognized command and otherwise applies moderation.
func (s *Service) HandleUpdate(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	if IsManagementCommand(ExtractContent(message)) {
		return s.HandleCommand(ctx, message)
	}
	return s.Process(ctx, message)
}

// Handle adapts the service to telegram.Poller's callback signature. The
// boolean deletion result remains available through HandleUpdate/Process.
func (s *Service) Handle(ctx context.Context, message domain.ModerationMessage) error {
	_, err := s.HandleUpdate(ctx, message)
	return err
}

// Process applies moderation to one new or edited message. It returns true
// only when a matching message was successfully deleted.
func (s *Service) Process(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	return s.process(ctx, message, nil)
}

// Moderate is a convenient alias for Process.
func (s *Service) Moderate(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	return s.Process(ctx, message)
}

// HandleMessage is an alias for Process for polling integrations.
func (s *Service) HandleMessage(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	return s.Process(ctx, message)
}

func (s *Service) process(ctx context.Context, message domain.ModerationMessage, adminKnown *bool) (bool, error) {
	if !IsSupportedGroup(message) || message.UserIsBot {
		return false, nil
	}
	content := ExtractContent(message)
	if content == "" {
		return false, nil
	}
	if IsManagementCommand(content) {
		admin := false
		if adminKnown != nil {
			admin = *adminKnown
		} else if message.UserID != nil && s.telegram != nil {
			var err error
			admin, err = s.telegram.IsGroupAdmin(ctx, message.ChatID, *message.UserID)
			if err != nil {
				s.logger.Warn("unable to determine group administrator", "chat_id", message.ChatID, "user_id", *message.UserID, "error", err)
				admin = false
			}
		}
		if admin {
			return false, nil
		}
	}
	if s.cache == nil {
		return false, errors.New("moderation rule cache is nil")
	}
	matched := s.cache.Match(message.ChatID, content)
	if len(matched) == 0 {
		return false, nil
	}

	deleteSucceeded := false
	var deletionErr error
	if s.telegram == nil {
		deletionErr = errors.New("telegram client is nil")
	} else {
		deletionErr = s.telegram.DeleteMessage(ctx, message.ChatID, message.MessageID)
		deleteSucceeded = deletionErr == nil
	}
	entry := domain.NewAuditEntry{
		ChatID: message.ChatID, ChatTitle: message.ChatTitle, MessageThreadID: message.MessageThreadID,
		UserID: message.UserID, MessageID: message.MessageID, MatchedRuleIDs: append([]int64(nil), matched...),
		Content: content, DeleteSucceeded: deleteSucceeded,
	}
	if deletionErr != nil {
		entry.DeletionError = truncateString(deletionErr.Error(), 1000)
		s.logger.Warn("unable to delete matched message", "chat_id", message.ChatID, "message_id", message.MessageID, "matched_rule_ids", matched, "error", deletionErr)
	}
	var auditErr error
	if s.audit != nil {
		auditErr = s.audit.Record(ctx, entry)
		if auditErr != nil {
			s.logger.Error("unable to record moderation audit", "chat_id", message.ChatID, "message_id", message.MessageID, "error", auditErr)
		}
	} else {
		auditErr = errors.New("audit store is nil")
	}
	if deleteSucceeded && s.telegram != nil {
		if err := s.telegram.SendMessage(ctx, message.ChatID, message.MessageThreadID, ModerationNotice); err != nil {
			s.logger.Warn("unable to send moderation notice", "chat_id", message.ChatID, "message_id", message.MessageID, "error", err)
		}
	}
	if auditErr != nil {
		return deleteSucceeded, auditErr
	}
	return deleteSucceeded, nil
}

// HandleCommand handles management commands. Unauthorized recognized commands
// receive a permission response and then go through normal moderation, so the
// command text cannot act as an advertising bypass.
func (s *Service) HandleCommand(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	if !IsSupportedGroup(message) || message.UserIsBot {
		return false, nil
	}
	name, args, ok := ParseCommand(ExtractContent(message))
	if !ok {
		return s.Process(ctx, message)
	}
	if _, recognized := managementCommands[name]; !recognized {
		return s.Process(ctx, message)
	}
	admin := false
	if message.UserID != nil && s.telegram != nil {
		var err error
		admin, err = s.telegram.IsGroupAdmin(ctx, message.ChatID, *message.UserID)
		if err != nil {
			s.logger.Warn("unable to determine group administrator", "chat_id", message.ChatID, "user_id", *message.UserID, "error", err)
		}
	}
	if !admin {
		_ = s.send(ctx, message, PermissionNotice)
		return s.process(ctx, message, &admin)
	}

	switch name {
	case "rule_add":
		return s.commandAdd(ctx, message, args)
	case "rule_list":
		return s.commandList(ctx, message)
	case "rule_remove":
		return s.commandRemove(ctx, message, args)
	case "rule_enable":
		return s.commandSetEnabled(ctx, message, args, true)
	case "rule_disable":
		return s.commandSetEnabled(ctx, message, args, false)
	case "rule_test":
		return s.commandTest(ctx, message, args)
	case "adlog":
		return s.commandLog(ctx, message, args)
	default:
		return false, nil
	}
}

func (s *Service) commandAdd(ctx context.Context, message domain.ModerationMessage, pattern string) (bool, error) {
	if strings.TrimSpace(pattern) == "" {
		return false, s.send(ctx, message, "用法：/rule_add <regex>")
	}
	if s.ruleStore == nil {
		return false, errors.New("rule store is nil")
	}
	rule, err := s.ruleStore.Add(ctx, domain.NewRule{ChatID: message.ChatID, ChatTitle: message.ChatTitle, Pattern: pattern, CreatedBy: userID(message)})
	if err != nil {
		_ = s.send(ctx, message, "无法保存规则："+err.Error())
		return false, nil
	}
	if err := s.refreshCache(ctx, message.ChatID); err != nil {
		s.logger.Error("unable to refresh rule cache", "chat_id", message.ChatID, "error", err)
		return false, err
	}
	return false, s.send(ctx, message, fmt.Sprintf("规则 #%d 已启用。", rule.ID))
}

func (s *Service) commandList(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	if s.ruleStore == nil {
		return false, errors.New("rule store is nil")
	}
	stored, err := s.ruleStore.List(ctx, message.ChatID)
	if err != nil {
		return false, s.send(ctx, message, "获取规则失败："+err.Error())
	}
	if len(stored) == 0 {
		return false, s.send(ctx, message, "本群尚未设置广告规则。")
	}
	lines := []string{"本群广告规则："}
	for _, rule := range stored {
		status := "停用"
		if rule.Enabled {
			status = "启用"
		}
		lines = append(lines, fmt.Sprintf("#%d [%s] %s", rule.ID, status, rule.Pattern))
	}
	return false, s.sendChunks(ctx, message, lines)
}

func (s *Service) commandRemove(ctx context.Context, message domain.ModerationMessage, args string) (bool, error) {
	id, ok := parseRuleID(args)
	if !ok {
		return false, s.send(ctx, message, "用法：/rule_remove <规则ID>")
	}
	if s.ruleStore == nil {
		return false, errors.New("rule store is nil")
	}
	err := s.ruleStore.Remove(ctx, message.ChatID, id)
	if err != nil {
		if errors.Is(err, store.ErrRuleNotFound) {
			return false, s.send(ctx, message, "未找到本群的该规则。")
		}
		return false, s.send(ctx, message, "删除规则失败："+err.Error())
	}
	if err := s.refreshCache(ctx, message.ChatID); err != nil {
		return false, err
	}
	return false, s.send(ctx, message, fmt.Sprintf("规则 #%d 已删除。", id))
}

func (s *Service) commandSetEnabled(ctx context.Context, message domain.ModerationMessage, args string, enabled bool) (bool, error) {
	id, ok := parseRuleID(args)
	command := "rule_disable"
	if enabled {
		command = "rule_enable"
	}
	if !ok {
		return false, s.send(ctx, message, "用法：/"+command+" <规则ID>")
	}
	if s.ruleStore == nil {
		return false, errors.New("rule store is nil")
	}
	if err := s.ruleStore.SetEnabled(ctx, message.ChatID, id, enabled); err != nil {
		if errors.Is(err, store.ErrRuleNotFound) {
			return false, s.send(ctx, message, "未找到本群的该规则。")
		}
		return false, s.send(ctx, message, "更新规则失败："+err.Error())
	}
	if err := s.refreshCache(ctx, message.ChatID); err != nil {
		return false, err
	}
	status := "停用"
	if enabled {
		status = "启用"
	}
	return false, s.send(ctx, message, fmt.Sprintf("规则 #%d 已%s。", id, status))
}

func (s *Service) commandTest(ctx context.Context, message domain.ModerationMessage, args string) (bool, error) {
	if strings.TrimSpace(args) == "" {
		return false, s.send(ctx, message, "用法：/rule_test <待检测文本>")
	}
	if s.cache == nil {
		return false, errors.New("moderation rule cache is nil")
	}
	matched := s.cache.Match(message.ChatID, args)
	if len(matched) == 0 {
		return false, s.send(ctx, message, "未命中任何已启用规则。")
	}
	ids := make([]string, len(matched))
	for i, id := range matched {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return false, s.send(ctx, message, "命中规则："+strings.Join(ids, ", "))
}

func (s *Service) commandLog(ctx context.Context, message domain.ModerationMessage, args string) (bool, error) {
	limit := DefaultLogLimit
	if strings.TrimSpace(args) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(args))
		if err != nil || parsed < 1 || parsed > MaxLogLimit {
			return false, s.send(ctx, message, "用法：/adlog [1-20]")
		}
		limit = parsed
	}
	if s.audit == nil {
		return false, errors.New("audit store is nil")
	}
	entries, err := s.audit.ListRecent(ctx, message.ChatID, limit)
	if err != nil {
		return false, s.send(ctx, message, "获取审计记录失败："+err.Error())
	}
	if len(entries) == 0 {
		return false, s.send(ctx, message, "暂无广告命中记录。")
	}
	lines := []string{"最近广告命中记录："}
	for _, entry := range entries {
		result := "删除失败"
		if entry.DeleteSucceeded {
			result = "已删除"
		}
		ruleIDs := make([]string, len(entry.MatchedRuleIDs))
		for i, id := range entry.MatchedRuleIDs {
			ruleIDs[i] = strconv.FormatInt(id, 10)
		}
		user := "-"
		if entry.UserID != nil {
			user = strconv.FormatInt(*entry.UserID, 10)
		}
		summary := strings.ReplaceAll(entry.ContentSummary, "\n", " ")
		lines = append(lines, fmt.Sprintf("#%d %s 规则:%s 用户:%s 内容:%s", entry.ID, result, strings.Join(ruleIDs, ","), user, summary))
	}
	return false, s.sendChunks(ctx, message, lines)
}

func (s *Service) refreshCache(ctx context.Context, chatID int64) error {
	if s.ruleStore == nil || s.cache == nil {
		return errors.New("rule store or rule cache is nil")
	}
	stored, err := s.ruleStore.List(ctx, chatID)
	if err != nil {
		return err
	}
	enabled := make([]domain.Rule, 0, len(stored))
	for _, rule := range stored {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	compiled, err := rules.CompileRules(enabled)
	if err != nil {
		return err
	}
	s.cache.Replace(chatID, compiled)
	return nil
}

func (s *Service) send(ctx context.Context, message domain.ModerationMessage, text string) error {
	if s.telegram == nil {
		return errors.New("telegram client is nil")
	}
	return s.telegram.SendMessage(ctx, message.ChatID, message.MessageThreadID, text)
}

func (s *Service) sendChunks(ctx context.Context, message domain.ModerationMessage, lines []string) error {
	var firstErr error
	for _, chunk := range chunkLines(lines, messageChunkSize) {
		if err := s.send(ctx, message, chunk); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) RefreshChatCache(ctx context.Context, chatID int64) error {
	return s.refreshCache(ctx, chatID)
}

func (s *Service) LoadCache(ctx context.Context) error {
	if s.ruleStore == nil || s.cache == nil {
		return errors.New("rule store or rule cache is nil")
	}
	loaded, err := s.ruleStore.LoadEnabled(ctx)
	if err != nil {
		return err
	}
	for chatID, stored := range loaded {
		compiled, compileErr := rules.CompileRules(stored)
		if compileErr != nil {
			s.logger.Error("unable to compile stored rules", "chat_id", chatID, "error", compileErr)
			continue
		}
		s.cache.Replace(chatID, compiled)
	}
	return nil
}

func parseRuleID(args string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	return id, err == nil && id > 0
}

func userID(message domain.ModerationMessage) int64 {
	if message.UserID == nil {
		return 0
	}
	return *message.UserID
}

func truncateString(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes]
}

func chunkLines(lines []string, limit int) []string {
	chunks := make([]string, 0, len(lines))
	current := ""
	for _, line := range lines {
		candidate := line
		if current != "" {
			candidate = current + "\n" + line
		}
		if current != "" && len(candidate) > limit {
			chunks = append(chunks, current)
			current = line
		} else {
			current = candidate
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}
