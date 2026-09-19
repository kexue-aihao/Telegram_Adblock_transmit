package builtin

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// This is an explicit alias table for the shipped vocabulary, not a general
// Chinese transliterator. New aliases need both advertising and benign fixtures.
var traditionalAliases = strings.NewReplacer(
	"錢", "钱", "資", "资", "賬", "账", "帳", "账", "過", "过", "兌", "兑",
	"幣", "币", "銀", "银", "轉", "转", "凍", "冻", "結", "结", "藥", "药",
	"聽", "听", "話", "话", "憶", "忆", "姦", "奸", "貨", "货", "購", "购",
	"單", "单", "聯", "联", "係", "系", "廣", "广", "銷", "销", "盤", "盘",
	"賭", "赌", "場", "场", "虛", "虚", "詐", "诈", "騙", "骗", "盜", "盗",
	"證", "证", "號", "号", "實", "实", "驗", "验", "註", "注", "冊", "册",
	"開", "开", "戶", "户", "專", "专", "業", "业", "國", "国", "內", "内",
	"際", "际", "還", "还", "貸", "贷", "務", "务", "傭", "佣", "報", "报",
	"紅", "红", "領", "领", "獎", "奖", "勵", "励", "純", "纯", "營", "营",
	"無", "无", "碼", "码", "歡", "欢", "電", "电", "醫", "医", "療", "疗",
	"學", "学", "風", "风", "險", "险", "嚴", "严", "發", "发", "師", "师",
	"處", "处", "獲", "获", "衛", "卫", "個", "个", "隱", "隐", "軌", "轨",
	"跡", "迹", "機", "机", "車", "车", "釣", "钓", "魚", "鱼", "頁", "页",
	"訊", "讯", "槍", "枪", "彈", "弹", "搖", "摇", "頭", "头", "氾", "泛",
	"約", "约", "門", "门", "圍", "围", "樓", "楼", "鳳", "凤",
	"價", "价", "優", "优", "現", "现", "應", "应", "讓", "让", "時", "时",
	"間", "间", "從", "从", "參", "参", "與", "与", "買", "买", "賣", "卖",
	"點", "点", "擊", "击", "載", "载", "曬", "晒", "樣", "样", "錄", "录",
	"貼", "贴", "則", "则", "聲", "声", "稱", "称", "書", "书", "請", "请",
	"遠", "远", "離", "离", "職", "职", "員", "员", "萬", "万", "億", "亿",
	"線", "线", "進", "进", "獨", "独", "級", "级", "種", "种", "獵", "猎",
	"謹", "谨", "該", "该", "責", "责", "獄", "狱", "團", "团", "隊", "队",
	"網", "网", "絡", "络", "運", "运", "輸", "输", "術", "术", "擔", "担",
	"簡", "简",
	// Fleet, identity-material, outcall and group-resource vocabulary shipped in
	// library 2.2.0. Only unambiguous one-to-one forms are listed.
	"馬", "马", "匯", "汇", "後", "后", "費", "费", "續", "续", "憂", "忧",
	"賠", "赔", "長", "长", "筆", "笔", "問", "问", "傭", "佣", "帶", "带",
	"採", "采", "職", "职", "認", "认", "臉", "脸", "掃", "扫", "紙", "纸",
	"攝", "摄", "會", "会", "條", "条", "龍", "龙", "遊", "游", "黃", "黄",
	"穢", "秽", "婦", "妇", "誘", "诱", "躍", "跃", "準", "准", "雲", "云",
	"寶", "宝", "媽", "妈", "養", "养", "擬", "拟", "郵", "邮", "設", "设",
	"備", "备", "維", "维", "裝", "装", "潔", "洁", "鎖", "锁", "遞", "递",
	"護", "护", "診", "诊", "檢", "检", "測", "测",
)

func normalize(text string) string {
	text = traditionalAliases.Replace(strings.ToLower(norm.NFKC.String(text)))
	var out strings.Builder
	out.Grow(len(text))
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cf, r), r >= 0xFE00 && r <= 0xFE0F,
			r >= 0xE0100 && r <= 0xE01EF:
		case r == '\r':
		case r == '\n':
			out.WriteByte('\n')
		case unicode.IsSpace(r):
			out.WriteByte(' ')
		case unicode.IsControl(r):
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Only curated multi-character CJK terms tolerate interleaved separators.
// We do not strip all punctuation or perform fuzzy/phonetic matching on prose.
type lexicon struct {
	pattern *regexp.Regexp
	starts  string
}

func (l *lexicon) MatchString(text string) bool {
	return strings.ContainsAny(text, l.starts) && l.pattern.MatchString(text)
}

func (l *lexicon) FindAllStringIndex(text string, limit int) [][]int {
	if !strings.ContainsAny(text, l.starts) {
		return nil
	}
	return l.pattern.FindAllStringIndex(text, limit)
}

func terms(flexible bool, words ...string) *lexicon {
	patterns := make([]string, 0, len(words))
	var starts strings.Builder
	seenStarts := map[rune]bool{}
	for _, word := range words {
		word = strings.ToLower(word)
		runes := []rune(word)
		if len(runes) > 0 && !seenStarts[runes[0]] {
			starts.WriteRune(runes[0])
			seenStarts[runes[0]] = true
		}
		hasHan := false
		for _, r := range runes {
			hasHan = hasHan || unicode.Is(unicode.Han, r)
		}
		var pattern strings.Builder
		if !hasHan {
			pattern.WriteString(`\b`)
		}
		for i, r := range runes {
			if i > 0 && flexible && hasHan {
				pattern.WriteString(`[\s\p{P}\p{S}]{0,3}`)
			}
			if r == ' ' {
				pattern.WriteString(`\s+`)
			} else {
				pattern.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		if !hasHan || len(runes) > 0 && unicode.IsLetter(runes[len(runes)-1]) && runes[len(runes)-1] < utf8.RuneSelf {
			pattern.WriteString(`\b`)
		}
		patterns = append(patterns, pattern.String())
	}
	return &lexicon{pattern: regexp.MustCompile("(?:" + strings.Join(patterns, "|") + ")"), starts: starts.String()}
}
