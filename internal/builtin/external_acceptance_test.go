package builtin_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// These acceptance examples were written independently of the matcher and its
// vocabulary fixtures. They exercise the promised distinction between live
// solicitation and ordinary discussions of the same subjects.
func TestIndependentAdvertisingAcceptance(t *testing.T) {
	cases := []struct {
		name, text, wantID string
	}{
		{"laundering_without_link", "洗资业务长期接单", "ad_money_laundering"},
		{"traditional_finance", "承接資金清洗業務，詳情私訊 @merchant_test", "ad_money_laundering"},
		{"controlled_separators", "洗💰资业务，长期承接，联系 @merchant_test", "ad_money_laundering"},
		{"zero_width", "洗\u200b钱业务承接，按点结算", "ad_money_laundering"},
		{"reverse_order", "@merchant_test\n承接各种洗资业务", "ad_money_laundering"},
		{"obfuscated_finance_alias", "洗q资金业务长期接单，联系 @merchant_test", "ad_money_laundering"},
		{"financial_mule", "招募跑分人员，自备银行卡收款，按点位结算", "ad_money_mule"},
		{"mule_direct", "钱骡长期招募，有意私聊", "ad_money_mule"},
		{"drug_without_link", "催情药现货供应，支持货到付款", "ad_aphrodisiac_trade"},
		{"drug_heading", "催情药\n\n批发，私聊 @merchant_test", "ad_aphrodisiac_trade"},
		{"traditional_drug", "迷情藥批發供貨，聯繫 @merchant_test", "ad_aphrodisiac_trade"},
		{"drug_variant", "催🌸情🌸药现货，批发联系 @merchant_test", "ad_aphrodisiac_trade"},
		{"drug_alias", "催q药现货批发，有意私聊", "ad_aphrodisiac_trade"},
		{"unrelated_prohibition", "严禁举报 洗资业务长期接单 @merchant_test", "ad_money_laundering"},
		{"unrelated_drug_prohibition", "禁止白嫖 催情药现货销售 @merchant_test", "ad_aphrodisiac_trade"},
		{"warning_then_ad", "反诈提醒：请勿参与洗钱。\n\n承接洗资业务，联系 @merchant_test", "ad_money_laundering"},
		{"medical_cover", "仅供科普研究。\n\n催情药现货供应，批发联系 @merchant_test", "ad_aphrodisiac_trade"},
		{"gambling", "百家乐招代理，首充优惠联系 @merchant_test", "ad_gambling"},
		{"scam_recruitment", "刷单任务招募，垫付返佣，私聊 @merchant_test", "ad_scam_investment"},
		{"identity_trade", "银行卡四件套现货出售，联系 @merchant_test", "ad_identity_trade"},
		{"private_data_trade", "出售个人信息，支持户籍查询，私聊 @merchant_test", "ad_personal_data_trade"},
		{"phishing_service_trade", "钓鱼套件现货出售，联系 @merchant_test", "ad_personal_data_trade"},
		{"illicit_goods_trade", "假证业务承接，有意私聊", "ad_illicit_goods"},
		{"sexual_trade", "同城约炮包夜，联系 @merchant_test", "ad_sexual_trade"},
		{"ordinary_invite", "周末读书会欢迎加入 https://t.me/+ExampleInvite", ""},
		{"ordinary_shortlink", "会议纪要 https://bit.ly/meeting", ""},
		{"bot_documentation", "免费的部署文档在 https://example.test/docs，有问题问 @helperbot。", ""},
		{"aml_discussion", "反洗钱技术：如何识别招聘骗局？", ""},
		{"reported_laundering", "警方通报，该团伙承接洗资业务并出售银行卡，请勿参与。", ""},
		{"medical_warning", "医生提醒，催情药售卖广告涉及危险成分，请勿购买。", ""},
		{"research_warning", "研究显示，非法销售催情药可能造成严重中毒。资料 https://health.example.test", ""},
		{"contact_police", "洗钱案件可咨询当地警方。", ""},
		{"consult_doctor", "催情药有副作用吗，咨询医生了解一下。", ""},
		{"report_contacts", "反诈提醒：如果发现洗钱广告，请联系警方举报。", ""},
		{"do_not_share_contact", "催情药的商家联系方式不应该传播。", ""},
		{"report_with_colon", "警方破获洗钱案件：嫌疑人长期接单，赃款经银行卡流转。https://news.example.test", ""},
		{"reported_ad_quote", "反诈案例：\"承接洗资，联系 @merchant_test\"。这属于违法广告，请举报。", ""},
		{"question_quote", "请问催情药广告里的‘货到付款’是否诈骗？", ""},
		{"cpu_benchmark", "CPU跑分比去年提升20%，测试报告 https://example.test/review", ""},
		{"linen_set", "四件套怎么清洗？纯棉床品需要低温烘干。", ""},
		{"ordinary_laundry", "朋友的洗衣店承接衣服代洗。", ""},
		{"dorm_water_room", "宿舍水房招募志愿者打扫卫生。", ""},
		{"crypto_transfer", "USDT转账需要核对链和手续费，参考 https://example.test/help", ""},
		{"finance_discussion", "银行代收代付接口的结算流程怎么处理？请联系技术支持。", ""},
		{"phishing_defense", "开源钓鱼页面识别工具发布，欢迎体验 https://github.com/example/detector", ""},
		{"only_document_url", "https://example.test/wiki/洗资接单", ""},
		{"drug_in_url_path", "资料 https://health.example.test/催情药销售风险", ""},
		{"separate_paragraphs", "今天学习洗钱案件的概念。\n\n二手椅子现货出售，自提。", ""},
		{"explicit_refusal", "我们不承接洗资业务，也不出售催情药。", ""},
	}
	checker := builtin.New(true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := checker.Analyze(domain.ModerationMessage{Text: tc.text})
			if tc.wantID == "" {
				if result.Matched {
					t.Fatalf("ordinary discussion classified as advertisement: %q => %v", tc.text, result.HitIDs())
				}
				return
			}
			if !result.Matched || !slices.Contains(result.HitIDs(), tc.wantID) {
				t.Fatalf("advertisement %q => %v, want %s", tc.text, result.HitIDs(), tc.wantID)
			}
		})
	}
}

func TestIndependentMetadataAcceptance(t *testing.T) {
	checker := builtin.New(true)
	cases := []struct {
		name string
		msg  domain.ModerationMessage
		want bool
	}{
		{"caption_ad", domain.ModerationMessage{Caption: "催情药现货供应，货到付款"}, true},
		{"buttons_only", domain.ModerationMessage{InlineButtons: []domain.InlineButtonInfo{{Text: "催情药现货出售", URL: "https://example.test/shop"}}}, true},
		{"ordinary_button", domain.ModerationMessage{InlineButtons: []domain.InlineButtonInfo{{Text: "阅读反诈报道", URL: "https://example.test/洗资接单"}}}, false},
		{"medical_button", domain.ModerationMessage{Text: "催情药有副作用吗？", InlineButtons: []domain.InlineButtonInfo{{Text: "咨询医生", URL: "https://hospital.example.test/consult"}}}, false},
		{"independent_button_labels", domain.ModerationMessage{InlineButtons: []domain.InlineButtonInfo{{Text: "个人信息"}, {Text: "出售闲置"}}}, false},
		{"normal_forward", domain.ModerationMessage{Text: "研究显示，非法销售催情药可能造成严重中毒。资料 https://health.example.test", Forward: &domain.ForwardInfo{Type: "channel", SourceTitle: "医学新闻"}}, false},
		{"channel_title_alone", domain.ModerationMessage{Text: "大家早上好", Forward: &domain.ForwardInfo{Type: "channel", SourceTitle: "洗资接单"}}, false},
		{"hidden_ad_link", domain.ModerationMessage{Text: "限时优惠，领取红包", Entities: []domain.MessageEntityInfo{{Type: "text_link", URL: "https://t.me/+ExampleInvite", Offset: 5, Length: 4, HasURL: true}}}, true},
		{"hidden_normal_link", domain.ModerationMessage{Text: "阅读反诈报道", Entities: []domain.MessageEntityInfo{{Type: "text_link", URL: "https://t.me/+ExampleInvite", Offset: 0, Length: 6, HasURL: true}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checker.Analyze(tc.msg); got.Matched != tc.want {
				t.Fatalf("matched=%v, want %v: %+v", got.Matched, tc.want, got)
			}
		})
	}
}

func TestIndependentAuditPrivacy(t *testing.T) {
	message := domain.ModerationMessage{Text: "承接洗资业务，联系 @unique_secret_contact https://example.test/private-token"}
	result := builtin.New(true).Analyze(message)
	if !result.Matched || result.Details() == nil {
		t.Fatal("advertisement should include an explanation")
	}
	encoded, err := json.Marshal(result.Details())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"unique_secret_contact", "example.test", "private-token", message.Text} {
		if strings.Contains(string(encoded), raw) {
			t.Fatalf("audit evidence persisted message or contact content: %s", encoded)
		}
	}
	before := result.Hits[0].Evidence[0]
	details := result.Details()
	details.Hits[0].Evidence[0] = "changed by caller"
	if result.Hits[0].Evidence[0] != before {
		t.Fatal("audit details alias the original analysis")
	}
}
