package builtin

import "regexp"

// This file holds the second vocabulary tier: black-industry phrases that are
// ordinary words on their own. "马车" is a carriage, "车队" is a convoy,
// "水房" is a dorm water room, "四件套" is bedding, "老群" is an old chat,
// "拍照" is photography and "会所/上门" are ordinary services. None of them may
// decide alone, so every rule here requires two independent signals in the same
// analysis window plus a live recruitment, price or contact entry point.

// ---- Money-mule fleet slang (跑分车队黑话) ----

// Unambiguous fleet and account-supply slang: any settlement term is enough.
var slangRolePronounced = terms(true,
	"红包车", "微信红包车", "跑分车", "跑分车队", "资金车队", "车队跑分",
	"人头号", "人头卡", "人头户", "卡队", "押车", "号商", "卡商", "码商",
)

// Words with a normal everyday meaning; they need dedicated settlement slang.
var slangRoleAmbiguous = terms(true,
	"马车", "车队", "车手", "水房", "四件套", "三件套", "收款码", "存款通道",
	"存通道", "代存", "存款车", "老群", "群资源", "号源", "白号", "跑分",
	"过账", "走账", "洗白", "洗米", "洗q", "洗z",
)

// Specific settlement, exchange-rate and "after-sales guarantee" slang. A bare
// 点位 is architectural or logistics vocabulary, so only its priced forms count.
var slangGuarantee = terms(true,
	"按点", "善后", "保司法", "包司法", "卸货无忧", "免风控", "无风控",
	"包赔", "笔笔", "不问来源", "不限来源", "不查来源",
)
var slangRate = regexp.MustCompile(`汇率\s*[0-9]|费率\s*[0-9]|点位\s*[0-9]|汇率\s*(?:高|美丽|优惠|漂亮|给力)|点位\s*(?:高|美丽|给力)|按点\s*[0-9]`)
var slangPayout = terms(true,
	"日结", "秒结", "佣金", "提成", "手续费", "押金", "包吃住", "包住宿",
	"报销路费", "车接车送", "长期要人", "大量要人",
)

// A live recruitment or supply verb keeps reports, warnings and quoted case
// descriptions ("有人用马车、汇率11招揽卡农") out of the verdict.
var slangRecruit = regexp.MustCompile(`(?:招|收|要|缺|找|带|接)[ ]?[人马号车卡码群存单]|来[人马号车]|上车|一手|提供|出号|收人|要人|缺人`)

// ---- Identity-material collection (实名素材采集) ----

// A material word directly joined to a shooting, collection or gig word, e.g.
// 拍照兼职、拍照采集、手持身份证拍照、人脸采集. The optional separator allows the
// decorated variants that the curated vocabulary already tolerates.
var identityMaterialPair = regexp.MustCompile(`(?:拍照|拍摄|照相|录脸|刷脸|扫脸|人脸|手持|身份证|证件|实名|白纸)[\s\p{P}\p{S}]{0,3}(?:拍照|拍摄|采集|兼职|任务|照片|影像|素材|资料|信息|白纸|正反面|认证|实名|日结)`)
var identityRecruitGate = regexp.MustCompile(`兼职|日结|一单|单价|每单|佣金|报酬|元/单|收人|要人|缺人|来人|招人|代理|长期要|大量要|带做|带人|上车|包吃住|车接车送|报销路费|结算|高价收|大量收`)

// ---- Outcall solicitation slang (上门招嫖暗语) ----

var sexualSlangSignal = terms(true,
	"空降", "上门", "快餐", "包夜", "全套", "半套", "一条龙", "莞式", "桑拿",
	"会所", "洗浴", "夜场", "外围", "商务模特", "伴游", "楼凤", "站街", "技师",
	"学生妹", "兼职妹", "洋妞", "大保健", "上门服务", "上门按摩", "推油", "波推",
	"口活", "服务到位", "同城上门", "上门接送", "快餐服务", "兼职上门",
)
var sexualSlangMark = terms(true,
	"色情", "涉黄", "黄色", "卖淫", "嫖娼", "嫖", "娼", "淫秽", "空降色",
	"上门色", "小姐", "少妇", "熟女", "学生妹", "兼职妹", "特殊服务", "全套",
	"半套", "包夜", "快餐服务", "无套", "内射", "口爆", "毒龙", "慢玩", "陪睡",
	"过夜", "裸聊", "情趣", "制服诱惑", "一条龙服务",
)
var sexualSlangToken = regexp.MustCompile(`(?:^|[\s\p{P}\p{S}])色(?:$|[\s\p{P}\p{S}0-9])`)
var sexualSlangPrice = regexp.MustCompile(`[0-9]{2,4}\s*(?:元|块)?\s*(?:一?次|每次|/\s*次|包夜|一夜|过夜|一小时|/\s*小时)|包夜价|快餐\s*[0-9]|价目`)

// Ordinary home services must never be read as solicitation, even when they
// carry a price, a technician or a booking phone number. Promises such as
// 包售后 or 可定制 are deliberately absent: they are common in the slang ads.
var benignService = terms(true,
	"维修", "安装", "清洗", "保洁", "搬家", "开锁", "装修", "水电", "家政",
	"疏通", "快递", "外卖", "家教", "护理", "陪诊", "月嫂", "摄影", "跟拍",
	"取件", "送水", "巡检", "保养", "测量", "量体", "回收",
)

// ---- Group and account supply (群资源与号源买卖) ----

var groupResourceSignal = terms(true,
	"老群", "群资源", "僵尸群", "死群", "活跃群", "精准群", "宝妈群", "行业群",
	"群号", "微信群", "qq群", "拉群", "群发", "群控", "云控", "养号", "号源",
	"白号", "老号", "实名号", "成品号", "虚拟号", "接码", "邮箱号", "设备号",
	"小号", "微信号", "支付宝号",
)
var groupResourceSell = regexp.MustCompile(`出售|售卖|销售|收购|回收|收售|批发|现货|出货|有货|货源|买卖|转让|出租|租用|价格|报价|售后|担保|出\s*(?:老群|群|号|卡|码)|收\s*(?:老群|群|号|卡|码)|大量\s*(?:出|收)`)

// ---- Bulk ad posting and traffic services (广告代发与引流服务) ----

// Spam distribution and traffic services sell the delivery mechanism itself,
// so the mechanism word, the advertising object and a service term must all be
// present. "广告投放技巧" without price, package or contact stays ordinary.
var bulkPostCore = terms(true,
	"代发", "群发", "站群", "引流", "私信", "群控", "云控", "批量", "广告位",
	"投放平台", "推流", "刷量", "爆粉", "加粉",
)
var bulkPostTarget = terms(true, "广告", "推广", "引流", "私信", "群发", "评论")
var bulkPostSupport = terms(true,
	"支付", "结算", "套餐", "报价", "价格", "代理", "承接", "接单", "虚拟卡",
	"虚拟信用卡", "vcc", "卡商", "一手", "长期", "稳定", "便捷", "联系",
	"私聊", "加群", "走量", "包月", "渠道", "曝光", "量多",
)

// ---- Idle-farming projects and income promises (挂机项目与高收益招募) ----

// An income promise is a commercial solicitation in its own right. Bare "项目"
// and "代理" are deliberately absent from the project list so ordinary work
// notices such as "工地项目招人，日结300" keep their literal meaning.
var incomePromise = regexp.MustCompile(`(?:日|月|天|周)[\s\p{P}\p{S}]{0,2}(?:入|赚|收)[\s\p{P}\p{S}]{0,4}[0-9]+\s*(?:[kKwW]|万|千|百|元|块)?|日\s*[0-9]+\s*[wW万]|月入过万|日入过万|睡后收入|一天赚|日结[0-9百千万]`)
var idleProject = terms(true,
	"挂机", "短剧", "搬砖", "刷量", "养号", "脚本", "看项目", "项目带队",
	"招代理", "代理加盟", "全网招代理", "带队", "带单", "手把手", "名额有限",
	"加群", "上车", "包教", "无风险", "零风险", "稳赚", "躺赚",
)

// ---- Smuggled and grey-market devices (水货与走私数码) ----

var greyMarketSource = terms(true,
	"水货", "水机", "水果机", "港版", "美版", "日版", "韩版", "欧版",
	"亚太版", "台版", "有锁", "卡贴", "华强北", "组装机", "翻新机", "演示机",
	"海关货", "免税版", "走私", "统货", "批发机",
)
var digitalGoods = terms(true,
	"苹果", "iphone", "ipad", "手机", "平板", "笔记本", "电脑", "数码",
	"耳机", "手表", "相机", "macbook", "安卓", "华为", "小米", "三星",
)
var greyMarketTrade = terms(true,
	"只要", "特价", "低价", "超低价", "现货", "批发", "出货", "大量", "一手",
	"找我", "联系", "合作", "询价", "代购", "秒发", "清仓", "走量", "拿货",
	"优惠价", "出货价", "拿货价",
)
var resellerRecruit = terms(true,
	"合作", "合伙", "诚招", "招代理", "代理加盟", "代理", "拿货", "走量", "手机店",
)
var greyMarketPriced = regexp.MustCompile(`只要\s*[0-9]|[0-9]+\s*[kK]\s*(?:起|左右|以内)?|特价|超低价|低价|出货价|拿货价|批发价|优惠价`)
var directContact = regexp.MustCompile(`找我|联系我|加我|私聊|私信|微信|电话`)

// entryLabels mirrors the labels used by the topic detectors so audit reasons
// stay comparable across the library.
func entryLabels(part section) []string {
	switch {
	case part.button && part.links.any:
		return []string{"按钮导流"}
	case part.contact:
		return []string{"联系入口"}
	case part.links.any:
		return []string{"链接导流"}
	}
	return nil
}

func matchLaunderingSlang(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		pronounced := slangRolePronounced.MatchString(text)
		guarantee := slangGuarantee.MatchString(text) || slangRate.MatchString(text)
		switch {
		case guarantee && (pronounced || slangRoleAmbiguous.MatchString(text)):
		case pronounced && slangPayout.MatchString(text):
		default:
			continue
		}
		if !part.contact && !part.links.any && !slangRecruit.MatchString(text) && !selfSolicitation.MatchString(text) {
			continue
		}
		labels := []string{"跑分洗钱黑话", "资金车队或码号资源"}
		if guarantee {
			labels = append(labels, "汇率点位或保障承诺")
		} else {
			labels = append(labels, "结算招募术语")
		}
		return append(labels, entryLabels(part)...)
	}
	return nil
}

func matchIdentityRecruit(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if !identityMaterialPair.MatchString(text) || !identityRecruitGate.MatchString(text) {
			continue
		}
		if !part.contact && !part.links.any && !transaction.MatchString(text) && !priceOrDelivery.MatchString(text) {
			continue
		}
		return append([]string{"实名素材招募", "拍照或人脸采集", "报酬结算"}, entryLabels(part)...)
	}
	return nil
}

func matchSexualSlang(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if benignService.MatchString(text) || !sexualSlangSignal.MatchString(text) {
			continue
		}
		price := sexualSlangPrice.MatchString(text)
		if !sexualSlangMark.MatchString(text) && !sexualSlangToken.MatchString(text) && !price {
			continue
		}
		if !part.contact && !part.links.any && !solicitation.MatchString(text) &&
			!selfSolicitation.MatchString(text) && !priceOrDelivery.MatchString(text) {
			continue
		}
		labels := []string{"上门或空降暗语", "色情服务标记"}
		if price {
			labels = append(labels, "价格或时长承诺")
		}
		return append(labels, entryLabels(part)...)
	}
	return nil
}

func matchGroupResourceTrade(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if !groupResourceSignal.MatchString(text) || !groupResourceSell.MatchString(text) {
			continue
		}
		if !part.contact && !part.links.any && !priceOrDelivery.MatchString(text) {
			continue
		}
		return append([]string{"群资源或号源", "买卖或转让术语"}, entryLabels(part)...)
	}
	return nil
}

func matchBulkPosting(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if !bulkPostCore.MatchString(text) || !bulkPostTarget.MatchString(text) ||
			!bulkPostSupport.MatchString(text) {
			continue
		}
		return append([]string{"广告代发或引流服务", "投放渠道或支付工具"}, entryLabels(part)...)
	}
	return nil
}

func matchIdleProject(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if !incomePromise.MatchString(text) || !idleProject.MatchString(text) {
			continue
		}
		labels := []string{"高收益项目招募", "挂机或代理术语"}
		if priceOrDelivery.MatchString(text) || part.contact || part.links.any {
			labels = append(labels, "结算或联系入口")
		}
		return append(labels, entryLabels(part)...)
	}
	return nil
}

func matchGreyMarket(view messageView) []string {
	for _, part := range view.sections {
		text := part.text
		if !digitalGoods.MatchString(text) || !greyMarketTrade.MatchString(text) {
			continue
		}
		// Path A: an explicit grey-market or smuggled source with a sales term.
		// Path B: a shop-recruitment post with an unusual price and a direct
		// contact, which is how the same trade finds resellers.
		source, recruit := greyMarketSource.MatchString(text), resellerRecruit.MatchString(text)
		switch {
		case source:
		case recruit && greyMarketPriced.MatchString(text) && (directContact.MatchString(text) || part.contact):
		default:
			continue
		}
		labels := []string{"水货或走私货源", "数码商品推销"}
		if recruit {
			labels = append(labels, "招募店铺合作")
		}
		return append(labels, entryLabels(part)...)
	}
	return nil
}
