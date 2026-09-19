package moderation

import (
	"strings"
)

// CommandInfo describes one bot command. Descriptions are reused for the
// setMyCommands menu (registered at startup) and the /start /help text so the
// two never drift apart. Menu order follows the slice order.
type CommandInfo struct {
	Name        string
	Description string
}

// BotMenu lists every command the bot registers through setMyCommands.
// start and help are public; the remaining commands are group-admin only.
var BotMenu = []CommandInfo{
	{Name: "start", Description: "查看机器人简介与命令说明"},
	{Name: "help", Description: "查看命令说明"},
	{Name: "rule_add", Description: "新增一条启用的广告规则（群管理员）"},
	{Name: "rule_regex", Description: "回复广告消息，自动转换为规则（群管理员）"},
	{Name: "rule_list", Description: "查看本群广告规则（群管理员）"},
	{Name: "rule_remove", Description: "删除本群规则（群管理员）"},
	{Name: "rule_enable", Description: "启用本群规则（群管理员）"},
	{Name: "rule_disable", Description: "停用本群规则（群管理员）"},
	{Name: "rule_test", Description: "测试文本命中规则（群管理员）"},
	{Name: "settings", Description: "查看或修改运行设置（机器人所有者）"},
	{Name: "adlog", Description: "查看最近广告命中记录（群管理员）"},
}

// publicCommands are answered for any user in any chat type.
var publicCommands = map[string]struct{}{
	"start": {}, "help": {},
}

const (
	// helpHeader is the static prefix of the /start and /help response.
	helpHeader = "广告拦截机器人使用说明\n\n" +
		"管理命令（请在群组内使用，仅群管理员可执行）："
	helpFooter = "\n通用命令：\n" +
		"/start — 查看本说明\n" +
		"/help — 查看本说明\n"
)

// HelpText renders the usage message returned by /start and /help. It is
// generated from BotMenu so the command list stays in sync with the menu.
func HelpText() string {
	var builder strings.Builder
	builder.WriteString(helpHeader)
	for _, command := range BotMenu {
		if _, public := publicCommands[command.Name]; public {
			continue
		}
		builder.WriteByte('\n')
		builder.WriteByte('/')
		builder.WriteString(command.Name)
		builder.WriteString(" — ")
		builder.WriteString(command.Description)
	}
	builder.WriteString(helpFooter)
	return builder.String()
}

// isPublicCommand reports whether the normalized command name is answered for
// any user (currently start and help), without requiring group membership.
func isPublicCommand(name string) bool {
	_, ok := publicCommands[name]
	return ok
}
