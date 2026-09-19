package builtin

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

const windowRunes = 240
const windowOverlap = 80

type messageView struct {
	sections []section
	forward  bool
}

type section struct {
	text        string
	background  string
	links       linkSignals
	contact     bool
	bot         bool
	button      bool
	buttonOnly  bool
	buttonOffer bool
}

type positionedEntity struct {
	start, end int // byte positions in the original text
	visible    string
	value      domain.MessageEntityInfo
}

var paragraphBreak = regexp.MustCompile(`\r?\n(?:[\t \r]*\n)+`)
var sentenceBreak = regexp.MustCompile(`[;；。!！\n]+`)
var clauseBreak = regexp.MustCompile(`[,，]+`)
var contrastBreak = regexp.MustCompile(`但是|然而|不过|可是|但`)
var mentionPattern = regexp.MustCompile(`@[a-z][a-z0-9_]{3,31}\b`)

func buildView(message domain.ModerationMessage) messageView {
	raw := message.Content()
	view := messageView{
		forward: message.Forward != nil && message.Forward.Type == "channel",
	}
	positioned := make([]positionedEntity, 0, len(message.Entities))
	for _, entity := range message.Entities {
		start, end, ok := utf16Range(raw, entity.Offset, entity.Length)
		if !ok {
			// Missing spans are accepted for legacy domain callers, but malformed
			// nonempty spans never manufacture a link or contact.
			if entity.Offset != 0 || entity.Length != 0 {
				continue
			}
			start, end = 0, 0
		}
		positioned = append(positioned, positionedEntity{
			start: start, end: end, visible: normalize(raw[start:end]), value: entity,
		})
	}
	type paragraph struct {
		start, end int
	}
	var paragraphs []paragraph
	start := 0
	for _, cut := range paragraphBreak.FindAllStringIndex(raw, -1) {
		paragraphs = append(paragraphs, paragraph{start, cut[0]})
		start = cut[1]
	}
	paragraphs = append(paragraphs, paragraph{start, len(raw)})
	var buttons []section
	for _, button := range message.InlineButtons {
		label := normalize(button.Text)
		active, _ := contextualText(label)
		part := section{text: semanticText(active), button: true, buttonOnly: true}
		part.links = textLinks(active)
		part.links.merge(classifyURL(button.URL))
		part.contact = part.links.telegram || contactAddress.MatchString(part.text)
		part.bot = part.links.bot
		part.buttonOffer = purchaseButton.MatchString(strings.TrimSpace(part.text)) ||
			directOffer.MatchString(part.text) || selfSolicitation.MatchString(part.text) ||
			promotionOffer.MatchString(part.text)
		buttons = append(buttons, part)
	}
	pendingHeading := ""
	for _, paragraph := range paragraphs {
		original := raw[paragraph.start:paragraph.end]
		normalized := normalize(original)
		heading := pendingHeading
		pendingHeading = highRiskHeading(normalized)
		active, background := contextualText(normalized)
		if strings.TrimSpace(active) == "" {
			continue
		}
		for windowIndex, window := range textWindows(active) {
			part := section{text: semanticText(window)}
			if windowIndex == 0 && heading != "" {
				part.text = heading + " " + part.text
			}
			// A quoted topic may only participate again if an independent live
			// offer survives outside the quotation in the same small paragraph.
			if utf8.RuneCountInString(active)+utf8.RuneCountInString(background) <= windowRunes {
				part.background = semanticText(background)
			}
			part.links = textLinks(window)
			part.contact, part.bot = mentions(window)
			for _, entity := range positioned {
				if entity.start < paragraph.start || entity.start >= paragraph.end && entity.start != entity.end {
					continue
				}
				if entity.visible != "" && !strings.Contains(window, entity.visible) {
					continue
				}
				switch entity.value.Type {
				case "url", "text_link":
					target := entity.value.URL
					if target == "" && entity.value.Type == "url" {
						target = entity.visible
					}
					part.links.merge(classifyURL(target))
					if target == "" && entity.value.HasURL {
						part.links.any = true
					}
				case "mention", "text_mention":
					part.contact = true
					part.bot = part.bot || entity.value.IsBot ||
						strings.HasSuffix(strings.ToLower(entity.value.Username), "bot")
				}
			}
			part.contact = part.contact || contactAddress.MatchString(part.text)
			part.bot = part.bot || part.links.bot
			view.sections = append(view.sections, part)
			if len(buttons) == 0 {
				continue
			}
			bodyOffer := commercialIntent(part) || promotional(part)
			for _, button := range buttons {
				// Each button is a separate source. A profile button and a
				// "sell unused items" button must not form a data-sale claim.
				// Mere "consult" links cannot turn a medical question into an ad.
				if !bodyOffer && !button.buttonOffer {
					continue
				}
				combined := part
				combined.text += " " + button.text
				combined.links.merge(button.links)
				combined.contact = combined.contact || button.contact
				combined.bot = combined.bot || button.bot
				combined.button = true
				combined.buttonOffer = button.buttonOffer
				view.sections = append(view.sections, combined)
			}
		}
	}
	for _, button := range buttons {
		if strings.TrimSpace(button.text) != "" {
			view.sections = append(view.sections, button)
		}
	}
	return view
}

// Only an entire short high-risk title may carry to the immediately following
// paragraph. A sentence about a case or medical discussion cannot be a title,
// and a weak term such as "跑分/代洗/水房/个人信息" cannot supply this evidence.
func highRiskHeading(text string) string {
	title := strings.TrimFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	if title == "" || utf8.RuneCountInString(title) > 24 {
		return ""
	}
	for _, vocabulary := range []*lexicon{launderingTopic, aphrodisiacTopic} {
		for _, span := range vocabulary.FindAllStringIndex(title, 1) {
			if span[0] == 0 && span[1] == len(title) {
				return title
			}
		}
	}
	return ""
}

func mentions(text string) (contact, bot bool) {
	for _, span := range mentionPattern.FindAllStringIndex(text, -1) {
		if span[0] > 0 {
			before := text[span[0]-1]
			if before >= 'a' && before <= 'z' || before >= '0' && before <= '9' || before == '_' {
				continue
			}
		}
		contact = true
		bot = bot || strings.HasSuffix(text[span[0]:span[1]], "bot")
	}
	return
}

// utf16Range validates Telegram offsets against the unmodified original text.
func utf16Range(text string, offset, length int) (int, int, bool) {
	if offset < 0 || length < 0 || offset > int(^uint(0)>>1)-length {
		return 0, 0, false
	}
	endUnits, units, startByte, endByte := offset+length, 0, -1, -1
	for index, r := range text {
		if units == offset {
			startByte = index
		}
		if units == endUnits {
			endByte = index
			break
		}
		units++
		if r > 0xFFFF {
			units++
		}
	}
	if startByte < 0 && units == offset {
		startByte = len(text)
	}
	if units == endUnits && endByte < 0 {
		endByte = len(text)
	}
	if startByte < 0 || endByte < startByte {
		return 0, 0, false
	}
	return startByte, endByte, true
}

func textWindows(text string) []string {
	runes := []rune(text)
	if len(runes) <= windowRunes {
		return []string{text}
	}
	var windows []string
	for start := 0; start < len(runes); start += windowRunes - windowOverlap {
		end := min(start+windowRunes, len(runes))
		windows = append(windows, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return windows
}

var quoteLead = regexp.MustCompile(`(?s)(?:广告(?:声称|写着|原文|内容)|(?:警方|反诈|诈骗|案例|教材|研究|测试|样本|示例|例句|引用|话术).{0,32}(?:如下|例如|比如|样例|示例|引用|内容|话术|原文|[:：]))[ \t:：]*$`)
var negatedBusiness = regexp.MustCompile(`(?:不(?:再|会|能|可|得|要)?|拒绝|严禁|禁止|切勿|请勿|不要|不得|不能|远离|谨防|警惕|防范|抵制|打击)[ :：]*(?:(?:任何|一切|再次|继续|私下|非法|参与|从事|发布|相信|此类|相关)[ :：]*){0,2}(?:提供|承接|销售|出售|售卖|购买|买卖|代办|招募|从事|接单|下单|交易|参与|私聊|私信|联系|咨询|广告|骗局|洗钱|洗资|跑分|迷药|催情药|听话水|刷单|博彩|赌博|开盒|社工库|诈骗|毒品|个人信息买卖)`)
var unrelatedNegationObject = regexp.MustCompile(`^[ \t:：]*(?:售后|退款|退货|举报|白嫖|议价|砍价|赊账|报警|打扰|同行|警方|警察|公安)`)
var reportContext = terms(false, "警方", "公安", "法院", "检察", "记者", "报道", "通报", "反诈", "案情", "案例", "研究", "论文", "医学", "医生", "药师", "科普", "教材")
var reportDetail = terms(false, "查获", "查处", "破获", "嫌疑人", "被告", "被捕", "判刑", "判处", "涉案", "团伙", "犯罪", "违法", "风险", "危害", "成分", "药理", "副作用", "中毒", "救治", "机制", "症状", "揭秘", "分析", "讨论", "讲解", "解释")
var reportLead = regexp.MustCompile(`(?:警方|公安|法院|检察(?:院)?|医生|药师|专家|记者|研究人员)(?:通报|提醒|查获|查处|破获|判处|介绍|表示|指出|解释|披露|报道)|医学科普|(?:新闻|媒体|警方)报道|(?:研究|调查|报告|论文)(?:显示|指出|表明|发现|认为)`)
var reportedActor = terms(false, "该团伙", "该商家", "该广告", "该平台", "该案", "涉案", "嫌疑人", "被告", "受害者", "商贩", "有人", "他们", "声称", "宣称", "查获", "查处", "破获", "判刑", "被捕", "售卖广告", "销售广告", "广告涉及", "广告中", "话术中")
var educationalQuestion = regexp.MustCompile(`(?:什么是|是什么|为什么|如何识别|如何防范|如何举报|请问).{0,80}(?:洗钱|洗资|跑分|催情|迷药|博彩|刷单|社工库|开盒|毒品|黑u)|(?:洗钱|洗资|诈骗|赌博|买卖个人信息).{0,12}(?:是犯罪|属于犯罪|违法|危害)`)
var professionalReferral = regexp.MustCompile(`(?:咨询|联系|问询|询问)(?:当地|正规|专业|相关|值班|主治)?(?:警方|公安|警察|医生|药师|医师|律师|急救|医院|监管|反诈中心)`)
var contactPrivacyDiscussion = regexp.MustCompile(`(?:联系方式|联系信息|联系人资料).{0,10}(?:不应|不能|不得|不可|隐私|泄露|传播风险)`)
var antiLaundering = regexp.MustCompile(`反\s*洗\s*钱|反\s*洗\s*资|\banti[- ]money\s+laundering\b`)

// URLs are link evidence, not the sender's claims. A word in a documentation URL
// must never become a topic or a sales verb. AML terminology is likewise not an
// offer to launder money; any separate live offer stays in the remaining text.
func semanticText(text string) string {
	text = urlCandidates.ReplaceAllString(text, " ")
	return antiLaundering.ReplaceAllString(text, "合规防范")
}

func contextualText(text string) (string, string) {
	text, quoted := removeReportedQuotes(text)
	text = contrastBreak.ReplaceAllString(text, "，")
	var active, background strings.Builder
	background.WriteString(quoted)
	for _, sentence := range sentenceBreak.Split(text, -1) {
		reporting := false
		for _, clause := range clauseBreak.Split(sentence, -1) {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				continue
			}
			meaning := semanticText(clause)
			reporting = reporting || reportLead.MatchString(meaning)
			if bound := negatedBusiness.FindStringIndex(meaning); bound != nil &&
				!unrelatedNegationObject.MatchString(meaning[bound[1]:]) {
				// A negation binds to the following trade/topic expression, not
				// to an unrelated "禁止举报/白嫖" disclaimer elsewhere.
				suffix := ""
				for _, offer := range directOffer.FindAllStringIndex(meaning, -1) {
					if offer[0] >= bound[1] {
						suffix = meaning[offer[0]:]
						break
					}
				}
				if suffix == "" {
					continue
				}
				clause, meaning = suffix, suffix
			}
			liveOffer := directOffer.MatchString(meaning) && !reportedActor.MatchString(meaning)
			contextual := (reportContext.MatchString(meaning) && reportDetail.MatchString(meaning) ||
				professionalReferral.MatchString(meaning) || contactPrivacyDiscussion.MatchString(meaning) ||
				reporting && (reportedActor.MatchString(meaning) || reportDetail.MatchString(meaning))) && !liveOffer
			if contextual || educationalQuestion.MatchString(meaning) && !liveOffer {
				continue
			}
			active.WriteString(clause)
			active.WriteByte(' ')
		}
	}
	return active.String(), background.String()
}

// Quotation marks alone provide no exemption. Only an explicit reporting or
// example introduction suppresses the quoted passage; live text remains active.
func removeReportedQuotes(text string) (string, string) {
	runes := []rune(text)
	var active, background strings.Builder
	for i := 0; i < len(runes); i++ {
		closing := rune(0)
		switch runes[i] {
		case '“':
			closing = '”'
		case '‘':
			closing = '’'
		case '「':
			closing = '」'
		case '『':
			closing = '』'
		case '"':
			closing = '"'
		}
		prefix := ""
		var introduction []int
		if closing != 0 {
			prefix = string(runes[max(0, i-100):i])
			introduction = quoteLead.FindStringIndex(prefix)
		}
		if introduction != nil {
			end := i + 1
			for end < len(runes) && runes[end] != closing {
				end++
			}
			if end < len(runes) {
				written, intro := active.String(), prefix[introduction[0]:]
				if strings.HasSuffix(written, intro) {
					active.Reset()
					active.WriteString(strings.TrimSuffix(written, intro))
				}
				background.WriteString(string(runes[i+1 : end]))
				background.WriteByte(' ')
				active.WriteByte(' ')
				i = end
				continue
			}
		}
		active.WriteRune(runes[i])
	}
	return active.String(), background.String()
}
