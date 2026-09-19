package rules

import (
	"regexp"
	"strings"
	"testing"
)

func deriveOrFail(t *testing.T, text string) string {
	t.Helper()
	pattern, _, err := DerivePattern(text)
	if err != nil {
		t.Fatalf("DerivePattern(%q) returned %v", text, err)
	}
	compiled, err := ValidatePattern(pattern)
	if err != nil {
		t.Fatalf("derived pattern %q is invalid: %v", pattern, err)
	}
	if !compiled.MatchString(text) {
		t.Fatalf("derived pattern %q does not match its own source %q", pattern, text)
	}
	return pattern
}

func TestDerivedPatternMatchesReposts(t *testing.T) {
	cases := []struct {
		name   string
		source string
		repost string
		absent string
	}{
		{
			name:   "ad posting service with decorated repost",
			source: "专业广告代发，广告投放支付，VCC虚拟卡稳定便捷",
			repost: "专业广告代发 🔥 广告投放支付 🔥 VCC虚拟卡稳定便捷",
			absent: "今天的广告投放复盘会议改到周三。",
		},
		{
			name:   "amounts and spacing change",
			source: "苹果18只要６k，买手机可以找我",
			repost: "苹果 17 只要 3k ， 买手机可以找我",
			absent: "苹果手机以旧换新的官方活动说明。",
		},
		{
			name:   "chinese numeral amounts change",
			source: "急招拍照兼职，日结三百，加我微信详聊",
			repost: "急招拍照兼职，日结五百，加我微信详聊",
			absent: "急招拍照兼职的岗位说明已经更新。",
		},
		{
			name:   "ordinary numeral words stay literal",
			source: "千万不要外传，代理日结一千",
			repost: "千万不要外传，代理日结两千",
			absent: "千万不要把验证码发给别人。",
		},
		{
			name:   "rotating link and handle",
			source: "视频会员充值 https://shop.example.invalid/pay 联系 @sample_agent",
			repost: "视频会员充值 t.me/other_channel 联系 @sample_agent",
			absent: "视频会员充值流程说明文档已经更新。",
		},
		{
			name:   "zero width insertion",
			source: "空降​色 上门服务，包夜价目私聊",
			repost: "空降色 上门服务，包夜价目私聊",
			absent: "今天上门服务预约已经排满了。",
		},
		{
			name:   "regex metacharacters stay literal",
			source: "限时5折(仅今天)+送礼品",
			repost: "限时 8 折(仅今天)+送礼品",
			absent: "限时折扣活动规则请看公告。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pattern := deriveOrFail(t, tc.source)
			compiled, err := ValidatePattern(pattern)
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.MatchString(tc.repost) {
				t.Errorf("pattern %q missed the repost %q", pattern, tc.repost)
			}
			if compiled.MatchString(tc.absent) {
				t.Errorf("pattern %q matched ordinary text %q", pattern, tc.absent)
			}
		})
	}
}

func TestDerivedPatternRefusesUnusableText(t *testing.T) {
	for _, text := range []string{
		"",
		"   ",
		"特价",
		"💰 💰 💰",
		"https://t.me/+only_link",
		"1234",
	} {
		if pattern, _, err := DerivePattern(text); err == nil {
			t.Errorf("DerivePattern(%q) = %q, want an error", text, pattern)
		}
	}
}

func TestDerivedPatternKeepsHandlesLiteral(t *testing.T) {
	pattern := deriveOrFail(t, "广告代发联系 @sample_agent 详聊")
	if !strings.Contains(pattern, "@sample_agent") {
		t.Fatalf("handle was generalized: %q", pattern)
	}
	compiled, err := ValidatePattern(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.MatchString("广告代发联系 @another_agent 详聊") {
		t.Fatalf("handle pattern %q matched a different contact", pattern)
	}
}

func TestDerivedPatternShortensLongMessages(t *testing.T) {
	long := strings.Repeat("全网招代理 日入过万 名额有限 手把手带 加群看项目 ", 40)
	pattern, truncated, err := DerivePattern(long)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatalf("expected truncation for a %d character message", len([]rune(long)))
	}
	if len([]rune(pattern)) > 512 {
		t.Fatalf("pattern exceeds the rule length limit: %d", len([]rune(pattern)))
	}
	compiled, err := ValidatePattern(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.MatchString(long) {
		t.Fatal("truncated pattern does not match its own source")
	}
}

func TestDerivedPatternIsAnchoredToItsOwnWording(t *testing.T) {
	pattern := deriveOrFail(t, "专业广告代发，广告投放支付")
	if _, err := regexp.Compile("(?i)(?:" + pattern + ")"); err != nil {
		t.Fatalf("pattern is not RE2 compatible: %v", err)
	}
	compiled, _ := ValidatePattern(pattern)
	for _, benign := range []string{
		"广告投放",
		"代发",
		"专业广告代发的流程说明在哪里？",
	} {
		if compiled.MatchString(benign) {
			t.Errorf("pattern %q matched %q", pattern, benign)
		}
	}
}
