package builtin

import (
	"slices"
	"testing"
)

// The samples below follow the black-industry advertising family that mixes
// outcall slang, money-mule fleet slang and identity-material recruitment in a
// single message. Vocabulary is deliberately paraphrased per case so the suite
// does not depend on one exact wording.
func TestBlackIndustryAdvertising(t *testing.T) {
	checker := New(true)
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			"reported mixed sample",
			"靠谱兄弟来拍照兼职，闲着的来拍照采集，空降色 123存 招马车 汇率11 微信红包车你出人头号就行（提供老群）汇率10.5笔笔善后保司法 卸货无忧 @XM910JQK",
			[]string{HitLaunderingSlang, HitIdentityRecruit, HitSexualSlang},
		},
		{
			"fleet slang with rate card",
			"长期招马车，汇率11，点位高，善后保司法，卸货无忧，联系 @example_agent",
			[]string{HitLaunderingSlang},
		},
		{
			"red packet fleet",
			"微信红包车长期要人，日结佣金，上车私聊",
			[]string{HitLaunderingSlang},
		},
		{
			"mule head account",
			"收人头号，一个人头三张卡，包吃住，有意详谈 @example_agent",
			[]string{HitLaunderingSlang},
		},
		{
			"fleet with guarantee terms",
			"车队跑分，笔笔善后，免风控，点位美丽，联系 @example_agent",
			[]string{HitLaunderingSlang},
		},
		{
			"identity photo recruitment",
			"闲着的人来拍照兼职，手持身份证拍照采集，日结三百，联系 @example_agent",
			[]string{HitIdentityRecruit},
		},
		{
			"face collection recruitment",
			"招募人脸采集人员，刷脸认证兼职，一单四十，报销路费 @example_agent",
			[]string{HitIdentityRecruit},
		},
		{
			"outcall slang",
			"空降色 上门服务，包夜价目私聊，联系 @example_agent",
			[]string{HitSexualSlang},
		},
		{
			"outcall slang token",
			"同城上门 快餐服务，32岁以下，色 300一次，联系 @example_agent",
			[]string{HitSexualSlang},
		},
		{
			"group resource trade",
			"大量出售老群资源，精准群号源批发，报价优惠，联系 @example_agent",
			[]string{HitGroupResourceTrade},
		},
		{
			"account supply trade",
			"出老号白号，实名号成品号长期有货，价格私聊 @example_agent",
			[]string{HitGroupResourceTrade},
		},
		{
			"obfuscated fleet slang",
			"招马·车，汇 率11，善后保司法 @example_agent",
			[]string{HitLaunderingSlang},
		},
		{
			"traditional fleet slang",
			"招馬車，匯率11，善後保司法，聯繫 @example_agent",
			[]string{HitLaunderingSlang},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := checker.Detect(msg(tc.text))
			for _, want := range tc.want {
				if !slices.Contains(hits, want) {
					t.Fatalf("Detect(%q) = %v, want %s", tc.text, hits, want)
				}
			}
		})
	}
}

// Recurring advertisements from the same families that the vocabulary should
// keep catching as the wording drifts. Each case only requires one of the
// shipped IDs, so a future refinement may change which rule fires.
func TestRecurringBlackIndustryAds(t *testing.T) {
	checker := New(true)
	cases := []struct {
		text string
		want string
	}{
		{"招跑分车队，点位8.5，日结，押金勿扰，联系 @example_agent", HitLaunderingSlang},
		{"卡农招募，包吃住，日结，联系 @example_agent", HitMoneyMule},
		{"接单洗米，点位高，有意私聊", HitLaunderingSlang},
		{"出微信号，实名老号，价格私聊 @example_agent", HitGroupResourceTrade},
		{"出售微信群，500一个，联系 @example_agent", HitGroupResourceTrade},
		{"接码平台出售，全年在线接单 @example_agent", HitIdentityTrade},
		{"学生妹上门服务，包夜价目表，联系 @example_agent", HitSexualSlang},
		{"空降色 上门 妹子 300一次 @example_agent", HitSexualSlang},
		{"押车业务，笔笔善后，免风控，联系 @example_agent", HitLaunderingSlang},
		{"收人头卡，一张 800，长期要人 @example_agent", HitLaunderingSlang},
		{"接广告代发，量大优惠，支持USDT结算 @example_agent", HitBulkPosting},
		{"广告位出租，价格优惠，联系 @example_agent", HitBulkPosting},
		{"群发广告软件出售，支持群控云控，联系 @example_agent", HitBulkPosting},
		{"全网招募代理，日入过万，包教包会，加群了解", HitIdleProject},
		{"短剧推广招募，日结100 @example_agent", HitIdleProject},
		{"水货苹果手机批发，华强北统货，长期供货 @example_agent", HitGreyMarket},
		{"澳门代购港版手机，正品水货，特价 @example_agent", HitGreyMarket},
		{"招拍照采集，日结 200，微信 sample_id", HitIdentityRecruit},
	}
	for _, tc := range cases {
		if hits := checker.Detect(msg(tc.text)); !slices.Contains(hits, tc.want) {
			t.Errorf("Detect(%q) = %v, want %s", tc.text, hits, tc.want)
		}
	}
}

// Ordinary uses of the same words must survive: carriages, convoys, dorm water
// rooms, bedding, photography gigs, repair visits and old chat groups.
func TestBlackIndustryVocabularyKeepsOrdinaryMeaning(t *testing.T) {
	checker := New(true)
	cases := []string{
		"周末自驾车队出发，费用 AA，要去的联系我报名。",
		"草原旅游马车体验，包吃住，联系 @travel_guide。",
		"宿舍水房招募志愿者打扫卫生。",
		"床品四件套现货批发，咨询卖家，价格优惠。",
		"CPU 跑分终于提高了，看看测试结果 https://bench.example.invalid",
		"物流车队长期承运，运费日结，联系 @logistics_agent。",
		"新车队成立，招募车手，日结工资。",
		"微信红包车怎么设置？求教程。",
		"请教 USDT 和银行承兑的区别，汇率 7.2 合理吗？",
		"招聘摄影助理，负责整理照片，日结 200，联系 @studio_agent。",
		"数据标注兼职，整理图片数据集，长期要人 @label_agent。",
		"手机拍照采集数据的 App 评测 https://review.example.invalid",
		"医院小程序上线，拍照采集身份证信息可自助建档。",
		"空调上门服务，预约请联系师傅。",
		"上门维修空调，更换滤网，联系 @repair_agent。",
		"上门保洁 200一次，联系 @clean_agent。",
		"会所招聘服务员，包吃住，联系 @hr_agent。",
		"会所年会活动，有妹子参加，联系 @hr_agent。",
		"按摩店招聘技师，包吃住，有意私聊。",
		"我们老群最近没人说话了，大家来聊聊。",
		"我们提供老群给新同学交流，欢迎加入。",
		"长期活跃群招募管理员，联系 @admin_agent。",
		"微信群发助手怎么用？求个说明。",
		"微信群里发了通知，大家看一下。",
		"我的微信号是 sample_id，欢迎交流。",
		"招聘兼职，日结，负责拍照记录施工进度。",
		"人脸采集系统上线，员工需刷脸认证。",
		"上门送水，联系 @delivery_agent。",
		"洗米水可以用来浇花，别浪费。",
		"车队调度通知：明天早上六点集合。",
		"跑分设备出售，显卡一张 1500，联系 @pc_agent。",
		"群里做活动，拍照采集活动照片，欢迎参加。",
	}
	for _, text := range cases {
		if hits := checker.Detect(msg(text)); len(hits) != 0 {
			t.Errorf("ordinary use %q hit %v", text, hits)
		}
	}
}

// Service-style advertisements: ad posting for hire, idle-farming projects
// with income promises and grey-market device sales.
func TestBlackIndustryServiceAds(t *testing.T) {
	checker := New(true)
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			"ad posting service",
			"专业广告代发，广告投放支付，VCC虚拟卡稳定便捷",
			[]string{HitBulkPosting},
		},
		{
			"bulk posting package",
			"承接群发广告，私信群发套餐，支持虚拟卡结算，代理加群 @example_agent",
			[]string{HitBulkPosting},
		},
		{
			"idle farming project",
			"跑分|日1W|短剧挂机项目|全网招代理。",
			[]string{HitIdleProject},
		},
		{
			"risk free laundering offer",
			"无风险的来，有码来帮我洗钱日入6千，",
			[]string{HitIdleProject, HitMoneyLaundering},
		},
		{
			"quota and hand holding",
			"磐石云。名额有限，手把手带，加群看项目 一天赚8千！💰 💰。",
			[]string{HitIdleProject},
		},
		{
			"urgent photo recruitment",
			"急招拍照📷　日结百左右",
			[]string{HitIdentityRecruit},
		},
		{
			"grey market devices",
			"苹果18只要６k，买手机‧可以找我，寻·手机店合·作，特价。水。果.机，１8ｐｒo只要6k",
			[]string{HitGreyMarket},
		},
		{
			"grey market stock",
			"港版美版手机现货，华强北统货，特价出货，联系 @example_agent",
			[]string{HitGreyMarket},
		},
		{
			"virtual card supply",
			"VCC虚拟卡出售，虚拟信用卡开卡，联系 @example_agent",
			[]string{HitIdentityTrade},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits := checker.Detect(msg(tc.text))
			for _, want := range tc.want {
				if !slices.Contains(hits, want) {
					t.Fatalf("Detect(%q) = %v, want %s", tc.text, hits, want)
				}
			}
		})
	}
}

// The same service vocabulary in ordinary use stays clean: construction work
// notices, second-hand sales, marketing tips and grey-market discussion.
func TestServiceVocabularyKeepsOrdinaryMeaning(t *testing.T) {
	checker := New(true)
	cases := []string{
		"工地项目招人，日结300，联系 @site_agent。",
		"短视频广告投放技巧分享，价格实惠 https://learn.example.invalid",
		"iPhone 15 二手转让，只要 2500，联系 @second_hand。",
		"港版 iPhone 和国行有什么区别？想了解一下。",
		"我在华强北买了台美版手机，比行货便宜。",
		"公司市场部招聘广告投放专员，日结 300，联系 @hr_agent。",
		"这周短剧更新到第 20 集了，挂机下载中。",
		"游戏挂机脚本分享，欢迎讨论。",
		"宿舍楼加群讨论周末拼车。",
		"理财有风险，投资需谨慎，不要相信日入过万的项目。",
		"短视频代运营服务，价格面议，联系 @example_agent。",
		"招聘日结兼职，一天赚200，联系 @example_agent。",
		"短剧更新到第 20 集了，晚上一起看。",
		"新款 iPhone 官方售价 7999，门店现货。",
		"公司采购了一批港版设备，正在做兼容测试。",
		"广告位申请流程已更新，请到后台查看。",
		"我们项目组日结工时已提交，请查收。",
	}
	for _, text := range cases {
		if hits := checker.Detect(msg(text)); len(hits) != 0 {
			t.Errorf("ordinary use %q hit %v", text, hits)
		}
	}
}

// Warnings, reports and quoted case descriptions keep the same vocabulary but
// are not offers.
func TestBlackIndustryReportingContext(t *testing.T) {
	checker := New(true)
	cases := []string{
		"警方通报，该团伙以马车、红包车招揽卡农，汇率11，保司法，请勿参与。",
		"反诈提醒：有人用马车、汇率11招揽卡农，请联系警方举报。",
		"警方披露的话术示例：“招马车，汇率11，善后保司法，联系 @example_agent”。请勿相信。",
		"科普：所谓“卸货无忧”“保司法”是洗钱团伙的常见承诺，请勿参与。",
	}
	for _, text := range cases {
		if hits := checker.Detect(msg(text)); len(hits) != 0 {
			t.Errorf("report context %q hit %v", text, hits)
		}
	}
}

// A warning label in front of a live offer must not become an exemption, in
// line with the rest of the library.
func TestBlackIndustryWarningLabelDoesNotExempt(t *testing.T) {
	checker := New(true)
	cases := []struct {
		text string
		want string
	}{
		{"反诈提醒：招马车，汇率11，善后保司法 @example_agent", HitLaunderingSlang},
		{"请勿轻信骗局。\n\n招马车，汇率11，善后保司法 @example_agent", HitLaunderingSlang},
		{"禁止白嫖 空降色 上门服务 包夜价目 @example_agent", HitSexualSlang},
		{"严禁举报 出售老群资源，精准群号源批发 @example_agent", HitGroupResourceTrade},
	}
	for _, tc := range cases {
		if hits := checker.Detect(msg(tc.text)); !slices.Contains(hits, tc.want) {
			t.Errorf("Detect(%q) = %v, want %s", tc.text, hits, tc.want)
		}
	}
}

func TestBlackIndustryDetectorsAreIndependentlySwitchable(t *testing.T) {
	checker := New(false)
	message := msg("招马车，汇率11，善后保司法 @example_agent")
	if hits := checker.Detect(message); len(hits) != 0 {
		t.Fatalf("disabled library still matched %v", hits)
	}
	known := map[string]bool{}
	for _, item := range Catalog() {
		known[item.ID] = true
	}
	for _, id := range []string{HitLaunderingSlang, HitIdentityRecruit, HitSexualSlang, HitGroupResourceTrade} {
		if !known[id] {
			t.Fatalf("catalog is missing %s", id)
		}
	}
}
