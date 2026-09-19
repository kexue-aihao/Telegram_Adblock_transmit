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
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/rules"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/settings"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/store"
)

const (
	ModerationNotice = "该消息因匹配广告规则已删除。"
	// BuiltinNotice names the shipped library explicitly: administrators cannot
	// see or edit its patterns, so the notice says which layer deleted the
	// message.
	BuiltinNotice    = "该信息因匹配内置广告库已删除。"
	PermissionNotice = "仅本群管理员可以管理广告规则。"
	DefaultLogLimit  = 10
	MaxLogLimit      = 20
	messageChunkSize = 3800

	// DefaultNoticeTTL is how long a deletion notice stays in the group. It is
	// short on purpose: the notice has done its job once it is read, and group
	// members should not have to scroll past old moderation chatter.
	DefaultNoticeTTL = 10 * time.Second
	// noticeDeletionTimeout bounds the API call that removes an expired notice.
	noticeDeletionTimeout = 5 * time.Second
)

var managementCommands = map[string]struct{}{
	"rule_add": {}, "rule_regex": {}, "rule_regax": {}, "rule_list": {},
	"rule_remove": {}, "rule_enable": {}, "rule_disable": {}, "rule_test": {},
	"settings": {}, "adlog": {},
}

// Service coordinates rule storage, the compiled rule cache, audit storage,
// and Telegram side effects.
type Service struct {
	ruleStore   ports.RuleStore
	cache       ports.RuleCache
	audit       ports.AuditStore
	telegram    ports.TelegramClient
	logger      *slog.Logger
	botUsername string
	builtin     *builtin.Checker
	profiles    ports.UserProfileReader
	settings    *settings.Manager

	// spamStrikeLimit / spamStrikeWindow implement the "three-strike" ban: a
	// non-admin user whose messages hit rules (or the built-in filter) that
	// many times within the window is permanently banned.
	spamStrikeLimit  int
	spamStrikeWindow time.Duration

	// noticeTTL is how long a deletion notice stays in the group. Zero keeps
	// notices until an administrator removes them.
	noticeTTL time.Duration
}

// NewService builds a moderation service. A nil logger falls back to the
// process default so tests and small integrations do not need logging setup.
func NewService(ruleStore ports.RuleStore, cache ports.RuleCache, audit ports.AuditStore, telegram ports.TelegramClient, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		ruleStore: ruleStore, cache: cache, audit: audit, telegram: telegram, logger: logger,
		noticeTTL: DefaultNoticeTTL,
	}
}

// SetBotUsername configures the username used to route /command@bot messages.
// It should be called once after Telegram getMe succeeds and before polling.
func (s *Service) SetBotUsername(username string) {
	if s == nil {
		return
	}
	s.botUsername = strings.TrimPrefix(strings.TrimSpace(username), "@")
}

// SetBuiltinFilter attaches the shipped-in advertising filter. A nil checker
// (or a disabled one) leaves the service with per-group rules only.
func (s *Service) SetBuiltinFilter(filter *builtin.Checker) {
	if s != nil {
		s.builtin = filter
	}
}

// SetUserProfileReader enables optional bio checks. Configure a bounded,
// cached reader before polling starts; nil disables all profile requests.
// Whether the check actually runs is decided per message by the runtime
// setting (see SetBotSettings), so the panel can switch it on later.
func (s *Service) SetUserProfileReader(reader ports.UserProfileReader) {
	if s != nil {
		s.profiles = reader
	}
}

// SetBotSettings attaches the runtime switches edited from the panel or from
// the /settings command. A nil manager keeps the documented defaults: profile
// checks off, cross-group management off and no bot owner.
func (s *Service) SetBotSettings(manager *settings.Manager) {
	if s != nil {
		s.settings = manager
	}
}

func (s *Service) botSettings() domain.BotSettings {
	if s == nil {
		return domain.BotSettings{}
	}
	return s.settings.Settings()
}

// managementRights describes what a sender may do with management commands.
type managementRights struct {
	allowed bool
	// trusted senders are group administrators or bot owners. Only their
	// command text is exempt from advertising moderation: a sender who is
	// allowed purely by the cross-group permission still has the message
	// moderated, so prefixing an advertisement with a command cannot hide it.
	trusted bool
}

func (s *Service) managementRights(groupAdmin bool, userID *int64) managementRights {
	if userID != nil && s.botSettings().IsOwner(*userID) {
		return managementRights{allowed: true, trusted: true}
	}
	if groupAdmin {
		return managementRights{allowed: true, trusted: true}
	}
	if s.botSettings().CrossGroupManagement {
		return managementRights{allowed: true}
	}
	return managementRights{}
}

// permissionNotice adds the sender's own user ID so the operator can paste it
// into BOT_OWNER_IDS or the panel without another tool.
func permissionNotice(message domain.ModerationMessage) string {
	return PermissionNotice + "\n" + ownerSetupHint(message)
}

func ownerSetupHint(message domain.ModerationMessage) string {
	if message.UserID == nil || *message.UserID <= 0 {
		return "如需授权，请在面板“设置 → 运行设置”或 BOT_OWNER_IDS 中配置机器人所有者。"
	}
	return "你的用户 ID：" + strconv.FormatInt(*message.UserID, 10) +
		"（如需授权，可在面板“设置 → 运行设置”或 BOT_OWNER_IDS 中把该 ID 设为机器人所有者）"
}

// SetNoticeTTL configures how long a deletion notice stays in the group before
// the bot removes it. Zero or less keeps notices until an administrator deletes
// them. Removing a notice is best effort and never changes the moderation result.
func (s *Service) SetNoticeTTL(ttl time.Duration) {
	if s != nil {
		s.noticeTTL = ttl
	}
}

// SetSpamPolicy configures the three-strike ban. A limit below 1 disables the
// ban. The window bounds how far back hit counts are considered.
func (s *Service) SetSpamPolicy(limit int, window time.Duration) {
	if s == nil {
		return
	}
	s.spamStrikeLimit = limit
	s.spamStrikeWindow = window
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
	name, _, args, ok = parseCommandTarget(content)
	return name, args, ok
}

func parseCommandTarget(content string) (name, target, args string, ok bool) {
	content = strings.TrimSpace(content)
	if content == "" || content[0] != '/' {
		return "", "", "", false
	}
	body := content[1:]
	separator := strings.IndexFunc(body, unicode.IsSpace)
	first, rest := body, ""
	if separator >= 0 {
		first, rest = body[:separator], body[separator:]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return "", "", "", false
	}
	if at := strings.IndexByte(first, '@'); at >= 0 {
		target = strings.TrimSpace(first[at+1:])
		first = first[:at]
		if target == "" {
			return "", "", "", false
		}
	}
	if first == "" {
		return "", "", "", false
	}
	return strings.ToLower(first), target, strings.TrimSpace(rest), true
}

func (s *Service) isManagementCommand(content string) bool {
	name, target, _, ok := parseCommandTarget(content)
	if !ok {
		return false
	}
	if target != "" && (s == nil || s.botUsername == "" || !strings.EqualFold(target, s.botUsername)) {
		return false
	}
	_, exists := managementCommands[name]
	return exists
}

func (s *Service) targetsOtherBot(content string) bool {
	_, target, _, ok := parseCommandTarget(content)
	return ok && target != "" && (s == nil || s.botUsername == "" || !strings.EqualFold(target, s.botUsername))
}

// HandleUpdate is the normal entry point for a converted Telegram update.
// It answers the public start/help commands in any chat type, handles a
// recognized management command, and otherwise applies moderation.
func (s *Service) HandleUpdate(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	content := ExtractContent(message)
	if s.targetsOtherBot(content) {
		return s.Process(ctx, message)
	}
	if name, _, ok := ParseCommand(content); ok && isPublicCommand(name) {
		entry, err := s.matchingEntry(ctx, message, nil)
		if err != nil {
			return false, err
		}
		if entry != nil {
			return s.enforce(ctx, message, *entry)
		}
		if message.UserIsBot {
			return false, nil
		}
		return false, s.send(ctx, message, HelpText())
	}
	if s.isManagementCommand(content) {
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
	entry, err := s.matchingEntry(ctx, message, adminKnown)
	if err != nil || entry == nil {
		return false, err
	}
	return s.enforce(ctx, message, *entry)
}

// matchingEntry evaluates a message once, keeping hit IDs and their details
// from the same library snapshot. Commands use the match result separately
// from deletion success so a failed deletion cannot produce a help response.
func (s *Service) matchingEntry(ctx context.Context, message domain.ModerationMessage, adminKnown *bool) (*domain.NewAuditEntry, error) {
	// Bot-authored messages delivered by Telegram still need moderation.
	// HandleCommand separately prevents bots from managing rules.
	if !IsSupportedGroup(message) {
		return nil, nil
	}
	content := ExtractContent(message)
	if content == "" && len(message.InlineButtons) == 0 {
		return nil, nil
	}
	if s.isManagementCommand(content) && !message.UserIsBot {
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
		if rights := s.managementRights(admin, message.UserID); rights.allowed && rights.trusted {
			return nil, nil
		}
	}
	// The built-in library runs before per-group rules. Ordinary administrator
	// messages follow the same content policy as other messages.
	if s.builtin != nil {
		if analysis := s.builtin.Analyze(message); analysis.Matched {
			hits := analysis.HitIDs()
			s.logger.Info("built-in ad filter hit", "chat_id", message.ChatID, "message_id", message.MessageID, "hits", hits, "library_version", analysis.LibraryVersion)
			return &domain.NewAuditEntry{
				ChatID: message.ChatID, ChatTitle: message.ChatTitle, MessageThreadID: message.MessageThreadID,
				UserID: message.UserID, MessageID: message.MessageID, BuiltinHits: hits,
				BuiltinDetails: analysis.Details(), Content: content,
			}, nil
		}
	}

	// Per-group regular expressions retain their original text/caption input;
	// metadata-only messages must not newly match empty-string patterns.
	if content == "" {
		return nil, nil
	}
	if s.cache == nil {
		return nil, errors.New("moderation rule cache is nil")
	}
	matched := s.cache.Match(message.ChatID, content)
	if len(matched) == 0 {
		return s.matchingBioEntry(ctx, message)
	}
	return &domain.NewAuditEntry{
		ChatID: message.ChatID, ChatTitle: message.ChatTitle, MessageThreadID: message.MessageThreadID,
		UserID: message.UserID, MessageID: message.MessageID, MatchedRuleIDs: append([]int64(nil), matched...),
		Content: content,
	}, nil
}

// enforce performs the shared delete + audit + notice sequence used by both
// the per-group rules and the built-in filter. The returned bool is true only
// when the message was successfully deleted.
func (s *Service) enforce(ctx context.Context, message domain.ModerationMessage, entry domain.NewAuditEntry) (bool, error) {
	deleteSucceeded := false
	var deletionErr error
	if s.telegram == nil {
		deletionErr = errors.New("telegram client is nil")
	} else {
		deletionErr = s.telegram.DeleteMessage(ctx, message.ChatID, message.MessageID)
		deleteSucceeded = deletionErr == nil
	}
	entry.DeleteSucceeded = deleteSucceeded
	if deletionErr != nil {
		entry.DeletionError = truncateString(deletionErr.Error(), 1000)
		s.logger.Warn("unable to delete matched message", "chat_id", message.ChatID, "message_id", message.MessageID, "matched_rule_ids", entry.MatchedRuleIDs, "builtin_hits", entry.BuiltinHits, "error", deletionErr)
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
	if auditErr == nil && entry.UserID != nil && s.spamStrikeLimit > 0 && s.audit != nil && s.telegram != nil {
		s.maybeBanSpammer(ctx, message, *entry.UserID)
	}
	if deleteSucceeded && s.telegram != nil {
		s.sendModerationNotice(ctx, message, moderationNoticeText(entry))
	}
	if auditErr != nil {
		return deleteSucceeded, auditErr
	}
	return deleteSucceeded, nil
}

// maybeBanSpammer permanently bans a non-admin user whose ad hits (built-in
// or rule-matched, all recorded in the audit log) reach the strike limit
// within the window.
func (s *Service) maybeBanSpammer(ctx context.Context, message domain.ModerationMessage, userID int64) {
	window := s.spamStrikeWindow
	if window <= 0 {
		window = 24 * time.Hour
	}
	hits, err := s.audit.CountHits(ctx, message.ChatID, userID, time.Now().Add(-window))
	if err != nil {
		s.logger.Warn("unable to count ad hits", "chat_id", message.ChatID, "user_id", userID, "error", err)
		return
	}
	if hits < int64(s.spamStrikeLimit) {
		return
	}
	admin, err := s.telegram.IsGroupAdmin(ctx, message.ChatID, userID)
	if err != nil {
		s.logger.Warn("unable to check admin status before ban", "chat_id", message.ChatID, "user_id", userID, "error", err)
		return
	}
	if admin {
		return
	}
	if err := s.telegram.BanChatMember(ctx, message.ChatID, userID); err != nil {
		s.logger.Warn("unable to ban ad spammer", "chat_id", message.ChatID, "user_id", userID, "hits", hits, "error", err)
		return
	}
	s.logger.Warn("user banned for ad spam", "chat_id", message.ChatID, "user_id", userID, "hits", hits)
}

// HandleCommand handles management commands. Unauthorized recognized commands
// receive a permission response and then go through normal moderation, so the
// command text cannot act as an advertising bypass.
func (s *Service) HandleCommand(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	if !IsSupportedGroup(message) {
		return false, nil
	}
	content := ExtractContent(message)
	if message.UserIsBot || s.targetsOtherBot(content) {
		return s.Process(ctx, message)
	}
	name, args, ok := ParseCommand(content)
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
	rights := s.managementRights(admin, message.UserID)
	if !rights.allowed {
		_ = s.send(ctx, message, permissionNotice(message))
		return s.process(ctx, message, &admin)
	}
	if !rights.trusted {
		// Allowed by the cross-group permission only: the command still runs,
		// but its text goes through moderation first so an advertisement cannot
		// hide behind a command prefix.
		if deleted, err := s.process(ctx, message, &admin); err != nil || deleted {
			return deleted, err
		}
	}

	switch name {
	case "rule_add":
		return s.commandAdd(ctx, message, args)
	case "rule_regex", "rule_regax":
		return s.commandRegex(ctx, message)
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
	case "settings":
		return s.commandSettings(ctx, message, args)
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
	if _, err := rules.ValidatePattern(pattern); err != nil {
		return false, s.send(ctx, message, "规则格式无效，请使用 Go RE2 兼容语法。")
	}
	rule, err := s.ruleStore.Add(ctx, domain.NewRule{ChatID: message.ChatID, ChatTitle: message.ChatTitle, Pattern: pattern, CreatedBy: userID(message)})
	if err != nil {
		if errors.Is(err, store.ErrRuleLimitExceeded) {
			_ = s.send(ctx, message, "本群规则数量或总长度已达到上限。")
		} else {
			_ = s.send(ctx, message, "无法保存规则，请稍后重试。")
		}
		return false, nil
	}
	if err := s.refreshCacheWithRetry(ctx, message.ChatID); err != nil {
		s.logger.Error("unable to refresh rule cache", "chat_id", message.ChatID, "error", err)
		if s.cache != nil {
			s.cache.Remove(message.ChatID)
		}
		_ = s.send(ctx, message, "规则已保存，但缓存刷新失败，请稍后重试。")
		return false, err
	}
	return false, s.send(ctx, message, fmt.Sprintf("规则 #%d 已启用。", rule.ID))
}

// ruleRegexUsage is returned when the administrator did not reply to anything.
// The alias is listed because /rule_regax is accepted as well.
const ruleRegexUsage = "用法：回复一条广告消息，再发送 /rule_regex（别名 /rule_regax）。" +
	"机器人会把被回复消息转换成正则规则并加入本群规则库。"

const settingsUsage = "用法：/settings 查看当前运行设置；" +
	"/settings bio_check on|off 开关简介辅助检测；/settings builtin on|off 开关内置广告库。修改仅限机器人所有者。"

// commandSettings reads and updates the runtime switches. These switches are
// global, so only bot owners may change them; group administrators and other
// managers can still read the current values from the same command.
func (s *Service) commandSettings(ctx context.Context, message domain.ModerationMessage, args string) (bool, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return false, s.sendChunks(ctx, message, s.settingsReport())
	}
	if len(fields) != 2 {
		return false, s.send(ctx, message, settingsUsage)
	}
	value, ok := parseToggle(fields[1])
	if !ok {
		return false, s.send(ctx, message, "开关值只能是 on 或 off。")
	}
	if message.UserID == nil || !s.botSettings().IsOwner(*message.UserID) {
		return false, s.send(ctx, message, "只有机器人所有者可以修改运行设置。"+ownerSetupHint(message))
	}
	switch strings.ToLower(fields[0]) {
	case "bio_check":
		if _, err := s.settings.SetBioCheck(ctx, value); err != nil {
			s.logger.Error("unable to save bio check setting", "error", err)
			return false, s.send(ctx, message, "保存设置失败，请稍后重试。")
		}
		s.logger.Info("bio check setting changed from chat", "user_id", *message.UserID, "enabled", value)
		return false, s.send(ctx, message, "简介辅助检测已"+onOffLabel(value)+"。")
	case "builtin":
		if s.builtin == nil {
			return false, s.send(ctx, message, "内置广告库尚未配置。")
		}
		if _, err := s.builtin.Update(ctx, &value, nil); err != nil {
			s.logger.Error("unable to save builtin master switch", "error", err)
			return false, s.send(ctx, message, "保存设置失败，请稍后重试。")
		}
		s.logger.Info("builtin master switch changed from chat", "user_id", *message.UserID, "enabled", value)
		return false, s.send(ctx, message, "内置广告库总开关已"+onOffLabel(value)+"。")
	default:
		return false, s.send(ctx, message, settingsUsage)
	}
}

// settingsReport lists the runtime switches and where each one is edited. It
// never includes credentials or message content.
func (s *Service) settingsReport() []string {
	current := s.botSettings()
	library := "未配置"
	if s.builtin != nil {
		library = onOffLabel(s.builtin.Enabled())
	}
	bio := onOffLabel(current.BioCheckEnabled)
	if current.BioCheckEnabled && s.profiles == nil {
		bio += "（尚未配置资料读取，暂不生效）"
	}
	access := "仅群管理员与机器人所有者"
	if current.CrossGroupManagement {
		access = "非群管理员也可以管理（跨群管理已开启）"
	}
	owners := "未设置"
	if len(current.OwnerUserIDs) > 0 {
		ids := make([]string, len(current.OwnerUserIDs))
		for i, id := range current.OwnerUserIDs {
			ids[i] = strconv.FormatInt(id, 10)
		}
		owners = strings.Join(ids, "、")
	}
	return []string{
		"当前运行设置（对所有群组生效）",
		"内置广告库：" + library,
		"简介辅助检测：" + bio,
		"管理权限：" + access,
		"机器人所有者：" + owners,
		"",
		"修改：/settings bio_check on|off、/settings builtin on|off（仅机器人所有者）",
		"跨群管理权限与所有者名单在面板“设置 → 运行设置”中修改。",
	}
}

func onOffLabel(enabled bool) string {
	if enabled {
		return "开启"
	}
	return "关闭"
}

func parseToggle(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "1", "enable", "enabled", "开", "开启", "启用":
		return true, true
	case "off", "false", "0", "disable", "disabled", "关", "关闭", "停用":
		return false, true
	}
	return false, false
}

// commandRegex derives a rule from the replied message. The derived pattern is
// always validated and always matches the message it came from, so a stored
// rule cannot silently miss its own sample. The conversion result is posted in
// the group so the administrator can see and immediately disable it.
func (s *Service) commandRegex(ctx context.Context, message domain.ModerationMessage) (bool, error) {
	if message.Reply == nil || strings.TrimSpace(message.Reply.Content()) == "" {
		return false, s.send(ctx, message, ruleRegexUsage)
	}
	source := message.Reply.Content()
	pattern, truncated, err := rules.DerivePattern(source)
	if err != nil {
		return false, s.send(ctx, message, "无法转换为规则："+err.Error())
	}
	if s.ruleStore == nil {
		return false, errors.New("rule store is nil")
	}
	rule, err := s.ruleStore.Add(ctx, domain.NewRule{
		ChatID: message.ChatID, ChatTitle: message.ChatTitle,
		Pattern: pattern, CreatedBy: userID(message),
	})
	if err != nil {
		if errors.Is(err, store.ErrRuleLimitExceeded) {
			_ = s.send(ctx, message, "本群规则数量或总长度已达到上限。")
		} else {
			_ = s.send(ctx, message, "无法保存规则，请稍后重试。")
		}
		return false, nil
	}
	if err := s.refreshCacheWithRetry(ctx, message.ChatID); err != nil {
		s.logger.Error("unable to refresh rule cache", "chat_id", message.ChatID, "error", err)
		if s.cache != nil {
			s.cache.Remove(message.ChatID)
		}
		_ = s.send(ctx, message, fmt.Sprintf("规则 #%d 已保存，但缓存刷新失败，请稍后重试。", rule.ID))
		return false, err
	}
	s.logger.Info("rule derived from replied message", "chat_id", message.ChatID, "rule_id", rule.ID, "truncated", truncated)
	return false, s.sendChunks(ctx, message, derivedRuleReport(rule.ID, pattern, truncated))
}

// derivedRuleReport renders the conversion result. It never echoes the quoted
// advertisement back into the group: only the pattern and its rule ID are shown.
func derivedRuleReport(id int64, pattern string, truncated bool) []string {
	lines := []string{
		fmt.Sprintf("已根据被回复消息生成规则 #%d 并启用：", id),
		pattern,
	}
	if truncated {
		lines = append(lines, "原文较长，规则只保留前半部分；如需覆盖全文请用 /rule_add 手动编写。")
	}
	lines = append(lines,
		"数字已泛化为 \\p{Nd}+，链接已替换为通用链接，@用户名保持原样。",
		fmt.Sprintf("可用 /rule_test <文本> 验证，/rule_disable %d 停用，/rule_remove %d 删除。", id, id),
	)
	return lines
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
	if err := s.refreshCacheWithRetry(ctx, message.ChatID); err != nil {
		if s.cache != nil {
			s.cache.Remove(message.ChatID)
		}
		s.logger.Error("unable to refresh rule cache", "chat_id", message.ChatID, "error", err)
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
	if err := s.refreshCacheWithRetry(ctx, message.ChatID); err != nil {
		if s.cache != nil {
			s.cache.Remove(message.ChatID)
		}
		s.logger.Error("unable to refresh rule cache", "chat_id", message.ChatID, "error", err)
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
		lines = append(lines, fmt.Sprintf("#%d %s 规则:%s%s 用户:%s 内容:%s", entry.ID, result, strings.Join(ruleIDs, ","), builtinAuditSummary(entry), user, summary))
	}
	return false, s.sendChunks(ctx, message, lines)
}

// builtinAuditSummary uses the stored names and evidence, so library updates
// do not rewrite historical explanations. Older records still show hit IDs.
func builtinAuditSummary(entry domain.AuditEntry) string {
	var hits []string
	seen := make(map[string]bool)
	if entry.BuiltinDetails != nil {
		for _, hit := range entry.BuiltinDetails.Hits {
			label := hit.Name
			if label == "" {
				label = hit.ID
			}
			label = truncateString(strings.Join(strings.Fields(label), " "), 100)
			if len(hit.Evidence) > 0 {
				reason := strings.Join(strings.Fields(strings.Join(hit.Evidence, "、")), " ")
				label += "（" + truncateString(reason, 180) + "）"
			}
			hits = append(hits, label)
			seen[hit.ID] = true
		}
	}
	for _, id := range entry.BuiltinHits {
		if !seen[id] {
			hits = append(hits, id)
			seen[id] = true
		}
	}
	if len(hits) == 0 {
		return ""
	}
	summary := " 内置:" + strings.Join(hits, "；")
	if entry.BuiltinDetails != nil && entry.BuiltinDetails.LibraryVersion != "" {
		summary += " 库版本:" + entry.BuiltinDetails.LibraryVersion
	}
	return summary
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

func (s *Service) refreshCacheWithRetry(ctx context.Context, chatID int64) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.refreshCache(ctx, chatID); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(50*(attempt+1)) * time.Millisecond):
			}
		}
	}
	return lastErr
}

// moderationNoticeText keeps the explanation aligned with what actually
// matched. A message that hit both layers reports the built-in library, because
// that is the part administrators cannot inspect themselves.
func moderationNoticeText(entry domain.NewAuditEntry) string {
	if len(entry.BuiltinHits) > 0 {
		return BuiltinNotice
	}
	return ModerationNotice
}

// sendModerationNotice posts the deletion notice and schedules its removal.
// The timer is deliberately independent of the update context: a notice may
// outlive the polling request that produced it.
func (s *Service) sendModerationNotice(ctx context.Context, message domain.ModerationMessage, text string) {
	noticeID, err := s.telegram.SendMessage(ctx, message.ChatID, message.MessageThreadID, text)
	if err != nil {
		s.logger.Warn("unable to send moderation notice", "chat_id", message.ChatID, "message_id", message.MessageID, "error", err)
		return
	}
	if s.noticeTTL <= 0 || noticeID <= 0 {
		return
	}
	chatID := message.ChatID
	time.AfterFunc(s.noticeTTL, func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), noticeDeletionTimeout)
		defer cancel()
		if err := s.telegram.DeleteMessage(cleanupCtx, chatID, noticeID); err != nil {
			s.logger.Warn("unable to delete moderation notice", "chat_id", chatID, "notice_id", noticeID, "error", err)
		}
	})
}

// send is used for command replies, which stay in the group until an
// administrator removes them.
func (s *Service) send(ctx context.Context, message domain.ModerationMessage, text string) error {
	if s.telegram == nil {
		return errors.New("telegram client is nil")
	}
	_, err := s.telegram.SendMessage(ctx, message.ChatID, message.MessageThreadID, text)
	return err
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
			return fmt.Errorf("compile stored rules for chat %d: %w", chatID, compileErr)
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
	if maxBytes <= 0 {
		return ""
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut]
}

func chunkLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	chunks := make([]string, 0, len(lines))
	current := ""
	for _, line := range lines {
		for _, piece := range splitByBytes(line, limit) {
			candidate := piece
			if current != "" {
				candidate = current + "\n" + piece
			}
			if current != "" && len(candidate) > limit {
				chunks = append(chunks, current)
				current = piece
			} else {
				current = candidate
			}
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}

func splitByBytes(value string, limit int) []string {
	if len(value) <= limit {
		return []string{value}
	}
	parts := make([]string, 0, (len(value)/limit)+1)
	start, size := 0, 0
	for index, r := range value {
		runeSize := utf8.RuneLen(r)
		if size > 0 && size+runeSize > limit {
			parts = append(parts, value[start:index])
			start = index
			size = 0
		}
		size += runeSize
	}
	if start < len(value) {
		parts = append(parts, value[start:])
	}
	return parts
}
