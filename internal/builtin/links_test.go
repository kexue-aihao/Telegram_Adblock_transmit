package builtin

import (
	"slices"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestURLHostClassification(t *testing.T) {
	cases := []struct {
		url                      string
		short, invite, join, bot bool
	}{
		{"https://BIT.LY/example", true, false, false, false},
		{"https://tinyurl.com/example", true, false, false, false},
		{"https://notbit.ly/example", false, false, false, false},
		{"https://bit.ly.evil.example/example", false, false, false, false},
		{"https://example.invalid/path/bit.ly/example", false, false, false, false},
		{"https://bit.ly@example.invalid/example", false, false, false, false},
		{"https://example.invalid/?next=https://bit.ly/example", false, false, false, false},
		{"https://t.me/+example", false, true, false, false},
		{"https://telegram.me/%2Bexample", false, true, false, false},
		{"https://telegram.dog/joinchat/example", false, false, true, false},
		{"https://t.me.evil.example/+example", false, false, false, false},
		{"https://nott.me/+example", false, false, false, false},
		{"https://example.invalid/t.me/+example", false, false, false, false},
		{"tg://join?invite=example", false, true, false, false},
		{"tg://resolve?domain=example_shopbot", false, false, false, true},
		{"https://t.me/example_shopbot?start=sample", false, false, false, true},
		{"https://t.me/example_shopbot_extra", false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := classifyURL(tc.url)
			if got.short != tc.short || got.invite != tc.invite || got.joinchat != tc.join || got.bot != tc.bot {
				t.Fatalf("unexpected classification: %+v", got)
			}
			fromText := textLinks("限时优惠，立即下单 " + tc.url)
			if fromText.short != tc.short || fromText.invite != tc.invite || fromText.joinchat != tc.join || fromText.bot != tc.bot {
				t.Fatalf("text extraction changed classification: %+v", fromText)
			}
		})
	}
}

func TestURLWordsAndComplianceTermsAreNotSalesEvidence(t *testing.T) {
	checker := New(true)
	for _, text := range []string{
		"https://example.test/wiki/洗资接单",
		"资料 https://health.example.test/催情药销售风险",
		"网址 https://example.test/?q=迷药批发",
		"反洗钱技术：如何识别招聘骗局？",
		"anti-money laundering 技术招聘岗位讨论",
		"AML 合规岗位工作介绍，USDT 风控说明",
	} {
		if got := checker.Detect(msg(text)); len(got) != 0 {
			t.Errorf("semantic URL/compliance false positive %q: %v", text, got)
		}
	}
	if got := checker.Detect(msg("反洗钱资料 https://example.test/wiki\n\n承接洗资业务，联系 @example_agent")); !slices.Contains(got, HitMoneyLaundering) {
		t.Fatalf("AML mention masked separate offer: %v", got)
	}
}

func TestUTF16EntityRanges(t *testing.T) {
	cases := []struct {
		offset, length int
		want           string
		valid          bool
	}{
		{0, 2, "😀", true},
		{2, 4, "联系客服", true},
		{1, 1, "", false},
		{0, 1, "", false},
		{-1, 3, "", false},
		{2, -1, "", false},
		{100, 1, "", false},
		{6, 0, "", true},
		{0, 0, "", true},
		{2, 0, "", true},
		{int(^uint(0) >> 1), 1, "", false},
	}
	for _, tc := range cases {
		start, end, ok := utf16Range("😀联系客服", tc.offset, tc.length)
		if ok != tc.valid {
			t.Errorf("range(%d,%d) valid=%v, want %v", tc.offset, tc.length, ok, tc.valid)
			continue
		}
		if ok && "😀联系客服"[start:end] != tc.want {
			t.Errorf("range(%d,%d)=%q, want %q", tc.offset, tc.length, "😀联系客服"[start:end], tc.want)
		}
	}
}

func FuzzAnalyzeMetadata(f *testing.F) {
	f.Add("😀催情药现货批发", "https://t.me/+sample", 2, 7)
	f.Add("警方通报，请勿参与", "tg://resolve?domain=example_bot", -1, 200)
	f.Add("反洗钱", "https://bit.ly.evil.example", 0, 0)
	checker := New(true)
	f.Fuzz(func(t *testing.T, content, target string, offset, length int) {
		if len(content) > 8192 || len(target) > 2048 {
			t.Skip()
		}
		message := msg(content, withEntities(domain.MessageEntityInfo{Type: "text_link", URL: target, Offset: offset, Length: length}))
		result := checker.Analyze(message)
		if result.LibraryVersion != LibraryVersion || result.Matched != (len(result.Hits) > 0) {
			t.Fatal("inconsistent analysis")
		}
		seen := map[string]bool{}
		for _, hit := range result.Hits {
			if seen[hit.ID] || strings.ContainsAny(strings.Join(hit.Evidence, ""), "@/") {
				t.Fatal("duplicate rule or non-label evidence")
			}
			seen[hit.ID] = true
		}
	})
}
