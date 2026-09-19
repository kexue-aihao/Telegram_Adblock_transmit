package builtin

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func msg(text string, modifiers ...func(*domain.ModerationMessage)) domain.ModerationMessage {
	m := domain.ModerationMessage{ChatType: "supergroup", ChatID: -100, MessageID: 1, Text: text}
	for _, modify := range modifiers {
		modify(&m)
	}
	return m
}

func withEntities(entities ...domain.MessageEntityInfo) func(*domain.ModerationMessage) {
	return func(m *domain.ModerationMessage) { m.Entities = append(m.Entities, entities...) }
}

func withForward(forward domain.ForwardInfo) func(*domain.ModerationMessage) {
	return func(m *domain.ModerationMessage) { m.Forward = &forward }
}

func detectorFixtures() map[string]domain.ModerationMessage {
	return map[string]domain.ModerationMessage{
		HitInviteLinkShort:        msg("限时优惠，立即下单 https://t.me/+sample_invite"),
		HitInviteLinkJoinchat:     msg("限时优惠，立即下单 https://t.me/joinchat/sample_invite"),
		HitShortLink:              msg("限时优惠，立即下单 bit.ly/example"),
		HitAdKeywordWithLink:      msg("限时优惠，立即下单 https://shop.example.invalid"),
		HitBotMention:             msg("限时优惠，立即下单 @example_shopbot", withEntities(domain.MessageEntityInfo{Type: "mention", Username: "example_shopbot"})),
		HitChannelForwardWithLink: msg("限时优惠，立即下单 https://shop.example.invalid", withForward(domain.ForwardInfo{Type: "channel"})),
		HitMoneyLaundering:        msg("承接洗资业务，联系 @example_agent"),
		HitMoneyMule:              msg("钱骡招募，日结佣金，联系 @example_agent"),
		HitAphrodisiacTrade:       msg("催情药现货批发，联系 @example_agent"),
		HitGambling:               msg("百家乐代理招募，联系 @example_agent"),
		HitScamInvestment:         msg("刷单招聘，垫付本金后返佣，联系 @example_agent"),
		HitIllicitGoods:           msg("假钞现货出售，联系 @example_agent"),
		HitIdentityTrade:          msg("实名号批量出售，联系 @example_agent"),
		HitPersonalDataTrade:      msg("社工库个人信息打包出售，联系 @example_agent"),
		HitSexualTrade:            msg("成人视频会员出售，联系 @example_agent"),
		HitLaunderingSlang:        msg("招马车，汇率11，善后保司法，卸货无忧，联系 @example_agent"),
		HitIdentityRecruit:        msg("拍照兼职，手持身份证拍照采集，日结报酬，联系 @example_agent"),
		HitSexualSlang:            msg("空降色 上门服务，价目详谈，联系 @example_agent"),
		HitGroupResourceTrade:     msg("大量出售老群资源，价格优惠，联系 @example_agent"),
		HitBulkPosting:            msg("专业广告代发，群发广告套餐，支持 VCC 虚拟卡支付，联系 @example_agent"),
		HitIdleProject:            msg("短剧挂机项目，日入6千，手把手带，加群看项目 @example_agent"),
		HitGreyMarket:             msg("水货手机现货，港版苹果只要6k，特价拿货，联系 @example_agent"),
	}
}

func TestEveryCatalogRuleHasAnIndependentFixture(t *testing.T) {
	checker := New(true)
	fixtures := detectorFixtures()
	if len(fixtures) != len(Catalog()) {
		t.Fatalf("catalog/fixture mismatch: %d/%d", len(Catalog()), len(fixtures))
	}
	for _, item := range Catalog() {
		t.Run(item.ID, func(t *testing.T) {
			message, ok := fixtures[item.ID]
			if !ok {
				t.Fatal("missing reviewed fixture")
			}
			if hits := checker.Detect(message); !slices.Contains(hits, item.ID) {
				t.Fatalf("Detect(%q) = %v, want %s", message.Text, hits, item.ID)
			}
		})
	}
}

func TestLegacyRulesPreserveOrdinarySharing(t *testing.T) {
	checker := New(true)
	cases := []domain.ModerationMessage{
		msg("欢迎加入读书讨论群 https://t.me/+reading_group"),
		msg("本周同学会 https://t.me/joinchat/friends"),
		msg("这是短链接 bit.ly/example"),
		msg("免费开源工具的使用说明 https://docs.example.invalid"),
		msg("有问题请问 @helpbot https://docs.example.invalid"),
		msg("普通频道文章 https://news.example.invalid", withForward(domain.ForwardInfo{Type: "channel"})),
		msg("看下 @xiaoming 这个链接 https://example.invalid"),
		msg("洗钱是什么"),
		msg("催情药"),
		msg("讨论 USDT 承兑"),
		msg("宿舍水房招募志愿者打扫卫生。"),
		msg("朋友的洗衣店承接衣服代洗。"),
		msg("洗钱案件可咨询当地警方。"),
		msg("催情药有副作用吗，咨询医生了解一下。"),
		msg("反诈提醒：如果发现洗钱广告，请联系警方举报。"),
		msg("催情药的商家联系方式不应该传播。"),
		msg("催情药的商家联系方式不应该传播 https://policy.example.invalid"),
		msg("洗钱案件可咨询当地警方 @police_example"),
	}
	for _, message := range cases {
		if hits := checker.Detect(message); len(hits) != 0 {
			t.Errorf("ordinary sharing %q hit %v", message.Text, hits)
		}
	}
}

func TestContextRemainsLocal(t *testing.T) {
	checker := New(true)
	cases := []struct {
		name, text string
		want       string
	}{
		{"plain denial", "不承接洗钱和洗资业务，禁止发送材料", ""},
		{"police report", "警方通报有人通过跑分洗钱，提醒居民不要参与 https://news.example.invalid", ""},
		{"medical explanation", "医学科普：催情药的成分和副作用有哪些，请咨询正规医生 https://health.example.invalid", ""},
		{"quoted warning", "警方披露的诈骗话术示例：“催情药现货批发，联系 @example_agent”。请勿购买。", ""},
		{"plain quote is no exemption", "“催情药现货批发，联系 @example_agent”", HitAphrodisiacTrade},
		{"fake warning label", "反诈提醒：催情药现货批发，联系 @example_agent", HitAphrodisiacTrade},
		{"self directed no address", "洗资业务有意私聊", HitMoneyLaundering},
		{"irrelevant no service disclaimer", "不提供售后 催情药现货销售 @example_agent", HitAphrodisiacTrade},
		{"bound no drug service", "不提供催情药销售服务，禁止发送订单", ""},
		{"unrelated warning", "请勿轻信骗局。\n\n承接洗资业务，联系 @example_agent", HitMoneyLaundering},
		{"warning then real offer", "警方通报洗钱案件。\n\n承接洗资业务，联系 @example_agent", HitMoneyLaundering},
		{"denial then other product", "不承接洗资业务。\n\n催情药现货批发，联系 @example_agent", HitAphrodisiacTrade},
		{"same paragraph contrast", "不做普通咨询，但是承接洗资业务，联系 @example_agent", HitMoneyLaundering},
		{"quote then live solicitation", "诈骗话术示例：“催情药”。实际现货批发，立即下单，联系 @example_agent", HitAphrodisiacTrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := checker.Detect(msg(tc.text))
			if tc.want == "" && len(hits) != 0 || tc.want != "" && !slices.Contains(hits, tc.want) {
				t.Fatalf("Detect(%q) = %v, want %q", tc.text, hits, tc.want)
			}
		})
	}
}

func TestNormalizationAndOrdering(t *testing.T) {
	checker := New(true)
	for _, text := range []string{
		"承接洗\u200b资业务，联系 @example_agent",
		"承接洗💰资业务，联系 @example_agent",
		"承接洗.资业务，联系 @example_agent",
		"承接洗 資業務，聯係 @example_agent",
		"承接洗资业务\n联系 @example_agent",
		"联系 @example_agent\n承接洗资业务",
		"承接洗\u202e资业务，联系 @example_agent",
	} {
		if hits := checker.Detect(msg(text)); !slices.Contains(hits, HitMoneyLaundering) {
			t.Errorf("missed normalization/order %q: %v", text, hits)
		}
	}
	for _, text := range []string{"催 情 藥現貨批發，聯係 @example_agent", "听✨话✨水现货批发", "ａｐｈｒｏｄｉｓｉａｃ ｆｏｒ ｓａｌｅ"} {
		if hits := checker.Detect(msg(text)); !slices.Contains(hits, HitAphrodisiacTrade) {
			t.Errorf("missed drug variation %q: %v", text, hits)
		}
	}
}

func TestEntityAndButtonTargets(t *testing.T) {
	checker := New(true)
	cases := []struct {
		name    string
		message domain.ModerationMessage
		want    string
	}{
		{"hidden invite", msg("限时优惠，立即下单", withEntities(domain.MessageEntityInfo{Type: "text_link", URL: "https://t.me/+hidden_sample"})), HitInviteLinkShort},
		{"hidden shortlink", msg("限时优惠，立即下单", withEntities(domain.MessageEntityInfo{Type: "text_link", URL: "https://bit.ly/hidden_sample"})), HitShortLink},
		{"emoji UTF16", msg("😀立即下单", withEntities(domain.MessageEntityInfo{Type: "text_link", Offset: 2, Length: 4, URL: "https://t.me/+hidden_sample"})), HitInviteLinkShort},
		{"tg resolve bot", msg("限时优惠，立即下单", withEntities(domain.MessageEntityInfo{Type: "text_link", URL: "tg://resolve?domain=example_shopbot"})), HitBotMention},
		{"bot without suffix", msg("催情药现货批发", withEntities(domain.MessageEntityInfo{Type: "text_mention", Username: "example_shop", IsBot: true})), HitBotMention},
		{"caption", domain.ModerationMessage{Caption: "催情药现货批发，联系 @example_agent"}, HitAphrodisiacTrade},
		{"only button", domain.ModerationMessage{InlineButtons: []domain.InlineButtonInfo{{Text: "催情药现货批发", URL: "https://t.me/example_agent"}}}, HitAphrodisiacTrade},
		{"button CTA", domain.ModerationMessage{Text: "催情药", InlineButtons: []domain.InlineButtonInfo{{Text: "立即下单", URL: "https://t.me/example_agent"}}}, HitAphrodisiacTrade},
		{"body plus button", domain.ModerationMessage{Text: "承接洗资业务", InlineButtons: []domain.InlineButtonInfo{{Text: "联系客服", URL: "https://t.me/example_agent"}}}, HitMoneyLaundering},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.message)
			hits := checker.Detect(tc.message)
			if !slices.Contains(hits, tc.want) {
				t.Fatalf("got %v, want %s", hits, tc.want)
			}
			after, _ := json.Marshal(tc.message)
			if string(before) != string(after) {
				t.Fatal("analysis mutated the original message")
			}
		})
	}
}

func TestIndependentButtonSourcesAndMedicalReferral(t *testing.T) {
	for _, message := range []domain.ModerationMessage{
		{Text: "便民服务菜单", InlineButtons: []domain.InlineButtonInfo{
			{Text: "个人信息", URL: "https://example.org/profile"},
			{Text: "出售闲置", URL: "https://example.org/listings"},
		}},
		{Text: "催情药有副作用吗？", InlineButtons: []domain.InlineButtonInfo{
			{Text: "咨询医生", URL: "https://hospital.example.org/consult"},
		}},
	} {
		if hits := New(true).Detect(message); len(hits) != 0 {
			t.Errorf("independent navigation/referral sources combined into ad: %v", hits)
		}
	}
}

func TestAdjacentHighRiskTitles(t *testing.T) {
	checker := New(true)
	for _, text := range []string{
		"催情药\n\n批发，私聊 @example_agent",
		"🔥催情藥🔥\n\n现货，立即下单",
		"洗资\n\n资金按单结算，有意私聊",
		"听话水\n\n\n\n现货批发",
	} {
		if hits := checker.Detect(msg(text)); len(hits) == 0 {
			t.Errorf("missed adjacent advertising title %q", text)
		}
	}
	for _, text := range []string{
		"今天学习洗钱案件的概念。\n\n二手椅子现货出售，自提。",
		"医生介绍催情药的危害。\n\n二手椅子现货出售，自提。",
		"个人信息\n\n出售闲置，自提",
		"跑分\n\n设备现货出售",
		"催情药\n\n这段是其他话题。\n\n二手椅子现货出售，自提。",
	} {
		if hits := checker.Detect(msg(text)); len(hits) != 0 {
			t.Errorf("unrelated paragraphs inherited a topic %q: %v", text, hits)
		}
	}
}

func TestAnalysisAndCatalogAreIndependentSnapshots(t *testing.T) {
	checker := New(true)
	message := msg("承接洗资业务，联系 @private_contact；催情药现货批发")
	analysis := checker.Analyze(message)
	if !analysis.Enabled || !analysis.Matched || analysis.LibraryVersion != LibraryVersion || len(analysis.Hits) < 2 {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}
	ids := analysis.HitIDs()
	if len(ids) != len(slices.Compact(slices.Clone(ids))) || !reflect.DeepEqual(ids, checker.Detect(message)) {
		t.Fatalf("unstable or duplicate IDs: %v", ids)
	}
	details := analysis.Details()
	details.Hits[0].Evidence[0] = "mutated"
	if analysis.Hits[0].Evidence[0] == "mutated" {
		t.Fatal("Details shares mutable evidence")
	}
	encoded, _ := json.Marshal(analysis)
	if strings.Contains(string(encoded), "private_contact") || strings.Contains(string(encoded), "承接洗资业务") {
		t.Fatal("analysis leaked message content")
	}
	catalog := Catalog()
	catalog[0].Conditions[0] = "mutated"
	if Catalog()[0].Conditions[0] == "mutated" {
		t.Fatal("Catalog shares condition slices")
	}
	for _, off := range []*Checker{nil, New(false), {}} {
		got := off.Analyze(message)
		if got.Enabled || got.Matched || len(got.Hits) != 0 || got.Details() != nil || got.LibraryVersion != LibraryVersion {
			t.Fatalf("disabled/nil checker returned %+v", got)
		}
	}
	benign := checker.Analyze(msg("正常讨论"))
	if !benign.Enabled || benign.Matched || benign.Details() != nil || benign.Hits == nil {
		t.Fatalf("unexpected benign response: %+v", benign)
	}
}

func TestLongMessagesDoNotLoseTailOrJoinDistantEvidence(t *testing.T) {
	checker := New(true)
	if hits := checker.Detect(msg(strings.Repeat("普通讨论文本", 1000) + " 催情药现货批发")); !slices.Contains(hits, HitAphrodisiacTrade) {
		t.Fatalf("tail missed: %v", hits)
	}
	text := "洗资 " + strings.Repeat("普通讨论文本", 100) + " 联系 @example_agent"
	if hits := checker.Detect(msg(text)); len(hits) != 0 {
		t.Fatalf("unrelated distant evidence joined: %v", hits)
	}
}
