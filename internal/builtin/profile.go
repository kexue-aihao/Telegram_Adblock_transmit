package builtin

import (
	"regexp"
	"strings"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

var profileInvitation = regexp.MustCompile(`(?:查看|看看|看|点开|点击|点)[ ]*(?:一下[ ]*)?(?:我|本人)[ ]*(?:的[ ]*)?(?:个人[ ]*)?(?:主页|简介|资料|头像)`)
var profileNegation = regexp.MustCompile(`(?:不(?:用|要|必|能|许|准|需|得|再)?|没(?:有)?|别|勿|禁止|无需|拒绝)[ ]*(?:(?:再|去|来|直接|随便|随意|继续)[ ]*)*$`)
var profileReport = terms(false, "反诈", "诈骗", "骗局", "骗术", "骗子", "警惕", "谨防", "话术", "引用", "示例", "例句", "不要相信", "别信", "不要信")
var profileQuotes = regexp.MustCompile("(?s)“[^”]*”|‘[^’]*’|「[^」]*」|『[^』]*』|\"[^\"]*\"|'[^']*'|`[^`]*`")

// HasProfileSolicitation recognizes a live first-person invitation only. Unlike
// standalone ad detection, a quoted or forwarded invitation cannot establish
// that the sender is promoting their own profile.
func HasProfileSolicitation(message domain.ModerationMessage) bool {
	if message.Forward != nil {
		return false
	}
	raw := message.Content()
	// Ignore Telegram-formatted quotes/code without joining their neighbours.
	visible := []byte(raw)
	for _, entity := range message.Entities {
		switch entity.Type {
		case "blockquote", "expandable_blockquote", "pre", "code":
			if start, end, ok := utf16Range(raw, entity.Offset, entity.Length); ok {
				for i := start; i < end; i++ {
					visible[i] = '\n'
				}
			}
		}
	}
	text := normalize(string(visible))
	text = profileQuotes.ReplaceAllString(text, "\n")
	for _, sentence := range sentenceBreak.Split(text, -1) {
		if profileReport.MatchString(sentence) || reportedActor.MatchString(sentence) {
			continue
		}
		active, _ := contextualText(sentence)
		active = semanticText(active)
		for _, span := range profileInvitation.FindAllStringIndex(active, -1) {
			if !profileNegation.MatchString(strings.TrimSpace(active[:span[0]])) {
				return true
			}
		}
	}
	return false
}

// AnalyzeBio keeps profile evidence independent of the message. Existing
// custom regexes never see the bio, and a cached bio uses current settings.
func (c *Checker) AnalyzeBio(message domain.ModerationMessage, bio string) Analysis {
	if !HasProfileSolicitation(message) {
		return Analysis{Enabled: c.Enabled(), LibraryVersion: LibraryVersion, Hits: []domain.BuiltinHit{}}
	}
	analysis := c.Analyze(domain.ModerationMessage{Text: bio})
	for i := range analysis.Hits {
		analysis.Hits[i].Evidence = append(analysis.Hits[i].Evidence, "消息主动引流", "证据来源：用户简介")
	}
	return analysis
}
