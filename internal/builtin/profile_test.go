package builtin

import (
	"slices"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestHasProfileSolicitation(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"看我主页", true},
		{"详情请查看本人的资料", true},
		{"点开我的个人主页，有联系方式", true},
		{"看看我的简介", true},
		{"点我头像", true},
		{"點開我的簡介", true},
		{"看\u200b我主\u200d页", true},
		{"看我 简介", true},
		{"大家好", false},
		{"看他的主页", false},
		{"我的主页是 https://example.org", false},
		{"不要看我主页", false},
		{"我没看我主页", false},
		{"看我“引用”主页", false},
		{"不用查看我的资料", false},
		{"别去点我头像", false},
		{"无需查看我的简介", false},
		{"禁止点击我的头像", false},
		{"反诈提醒：看我主页是常见话术", false},
		{"警惕看我主页这种引流", false},
		{"有人说看我主页", false},
		{"“看我主页”", false},
		{"案例引用：『点我头像』", false},
		{"示例：看我主页", false},
		{"`看我主页` 是一句话", false},
		{"“看我主页”。现在请点我头像", true},
		{"不要看我的资料；现在点我头像", true},
		{"https://example.org/看我主页", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := HasProfileSolicitation(domain.ModerationMessage{Text: tc.text}); got != tc.want {
				t.Fatalf("HasProfileSolicitation = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProfileSolicitationSources(t *testing.T) {
	if !HasProfileSolicitation(domain.ModerationMessage{Caption: "看我主页"}) {
		t.Fatal("caption not checked")
	}
	for _, kind := range []string{"blockquote", "expandable_blockquote", "code", "pre"} {
		message := domain.ModerationMessage{Text: "🙂看我主页", Entities: []domain.MessageEntityInfo{{Type: kind, Offset: 2, Length: 4}}}
		if HasProfileSolicitation(message) {
			t.Fatalf("quoted entity %s considered a live invitation", kind)
		}
		message.Text += "。点我头像"
		if !HasProfileSolicitation(message) {
			t.Fatalf("live invitation after %s ignored", kind)
		}
	}
	if HasProfileSolicitation(domain.ModerationMessage{Text: "看我主页", Forward: &domain.ForwardInfo{Type: "user"}}) {
		t.Fatal("forward attributed to sender profile")
	}
}

func TestAnalyzeBioRequiresIndependentAdEvidence(t *testing.T) {
	const ad = "承接洗资业务，联系 @example_agent"
	checker := New(true)
	message := domain.ModerationMessage{Text: "看我主页"}
	analysis := checker.AnalyzeBio(message, ad)
	if !analysis.Matched || !slices.Contains(analysis.HitIDs(), HitMoneyLaundering) {
		t.Fatalf("missing profile hit: %+v", analysis)
	}
	for _, hit := range analysis.Hits {
		if !slices.Contains(hit.Evidence, "消息主动引流") || !slices.Contains(hit.Evidence, "证据来源：用户简介") {
			t.Fatalf("missing evidence: %+v", hit)
		}
	}
	for _, bio := range []string{"", "个人网站 https://example.org", "我的频道 https://t.me/example_channel", "反诈科普，远离洗钱骗局", "洗钱"} {
		if checker.AnalyzeBio(message, bio).Matched {
			t.Fatalf("benign bio matched: %q", bio)
		}
	}
	if checker.AnalyzeBio(domain.ModerationMessage{Text: "大家好"}, ad).Matched {
		t.Fatal("normal message plus advertising bio matched")
	}
	if checker.AnalyzeBio(domain.ModerationMessage{Text: "洗钱，看我主页"}, "联系 @example_agent").Matched {
		t.Fatal("message and bio were combined into an ad")
	}
	checker.settings.Store(&domain.BuiltinSettings{Enabled: true, DisabledRules: []string{HitMoneyLaundering}})
	if checker.AnalyzeBio(message, ad).Matched {
		t.Fatal("disabled detector still matched cached source text")
	}
	if New(false).AnalyzeBio(message, ad).Matched {
		t.Fatal("disabled library matched")
	}
}
