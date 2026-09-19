package builtin

import "slices"

// Existing identifiers are deliberately retained: persisted per-rule switches
// and historical audit entries continue to identify the same detection family.
const (
	HitInviteLinkShort        = "ad_invite_link"
	HitInviteLinkJoinchat     = "ad_invite_joinchat"
	HitShortLink              = "ad_shortlink"
	HitAdKeywordWithLink      = "ad_keyword_link"
	HitBotMention             = "ad_bot_mention"
	HitChannelForwardWithLink = "ad_channel_forward_link"
	HitMoneyLaundering        = "ad_money_laundering"
	HitMoneyMule              = "ad_money_mule"
	HitAphrodisiacTrade       = "ad_aphrodisiac_trade"
	HitGambling               = "ad_gambling"
	HitScamInvestment         = "ad_scam_investment"
	HitIllicitGoods           = "ad_illicit_goods"
	HitIdentityTrade          = "ad_identity_trade"
	HitPersonalDataTrade      = "ad_personal_data_trade"
	HitSexualTrade            = "ad_sexual_trade"
)

type detector struct {
	info  RuleInfo
	match func(messageView) []string
}

// The registry is the single source of truth for matching, the WebUI catalog,
// supported setting IDs and stable output order.
var registry = []detector{
	linkDetector(HitInviteLinkShort, "邀请链接推广", "Telegram 群组邀请入口", "群组邀请入口", func(s section, _ messageView) bool { return s.links.invite }),
	linkDetector(HitInviteLinkJoinchat, "Joinchat 邀请推广", "Telegram Joinchat 邀请入口", "群组邀请入口", func(s section, _ messageView) bool { return s.links.joinchat }),
	linkDetector(HitShortLink, "短链接推广", "精确匹配内置短链接域名", "短链导流", func(s section, _ messageView) bool { return s.links.short }),
	linkDetector(HitAdKeywordWithLink, "广告招揽与链接", "文本、隐藏链接或按钮中的实际链接", "链接导流", func(s section, _ messageView) bool { return s.links.any }),
	linkDetector(HitBotMention, "机器人引流广告", "机器人提及或机器人链接", "机器人引流", func(s section, _ messageView) bool { return s.bot }),
	linkDetector(HitChannelForwardWithLink, "频道转发推广", "频道转发来源并附有链接", "频道转发链接", func(s section, view messageView) bool { return view.forward && s.links.any }),
	topicDetector(HitMoneyLaundering, "洗钱与洗资招揽", "资金洗白", "洗钱洗资主题",
		"资金洗白主题，或金融语境中的洗白、走账等组合", launderingTopic,
		func(s section) bool { return launderingWeak.MatchString(s.text) && moneyContext.MatchString(s.text) }),
	topicDetector(HitMoneyMule, "跑分与资金通道招募", "资金通道", "跑分资金通道",
		"钱骡、卡农等主题；普通跑分、代收代付需金融及异常结算证据", muleTopic,
		func(s section) bool {
			return muleWeak.MatchString(s.text) && moneyContext.MatchString(s.text) && illicitFinanceContext.MatchString(s.text)
		}),
	topicDetector(HitAphrodisiacTrade, "催情迷情药品交易", "药品交易", "催情迷情药品",
		"催情、迷情、听话水等药品词组与交易招揽组合", aphrodisiacTopic, nil),
	topicDetector(HitGambling, "博彩与赌局推广", "博彩推广", "博彩赌局主题",
		"博彩主题；彩票、棋牌等词需同时包含下注或赌资证据", gamblingTopic,
		func(s section) bool { return gamblingWeak.MatchString(s.text) && gamblingContext.MatchString(s.text) }),
	topicDetector(HitScamInvestment, "诈骗投资与刷单招募", "诈骗招募", "诈骗返佣主题",
		"刷单、资金盘、保证盈利等主题，或兼职投资与垫付等风险组合", scamTopic,
		func(s section) bool { return scamWeak.MatchString(s.text) && scamContext.MatchString(s.text) }),
	topicDetector(HitIllicitGoods, "违禁品与伪造品交易", "违禁品", "违禁伪造品主题",
		"毒品、武器、假证假钞等商品与交易招揽组合", illicitGoodsTopic, nil),
	topicDetector(HitIdentityTrade, "账号与实名工具交易", "账号与实名", "账号实名工具",
		"盗号、接码、实名工具、银行卡套件等与交易招揽组合", identityTopic, nil),
	topicDetector(HitPersonalDataTrade, "个人信息与钓鱼服务交易", "隐私与钓鱼", "隐私钓鱼服务",
		"社工库、隐私查询、个人信息或钓鱼服务与交易招揽组合", personalDataTopic, nil),
	topicDetector(HitSexualTrade, "色情资源与性交易推广", "色情交易", "色情交易主题",
		"性交易、色情资源主题；上门服务等通用词需额外性交易证据", sexTradeTopic,
		func(s section) bool { return sexTradeWeak.MatchString(s.text) && sexContext.MatchString(s.text) }),
}

func linkDetector(id, name, condition, evidence string, selector func(section, messageView) bool) detector {
	return detector{
		info: RuleInfo{
			ID: id, Name: name, Category: "通用导流",
			Description: condition + "，并在邻近内容中出现明确商业推广或交易招揽；普通分享不命中。",
			Conditions:  []string{condition, "邻近内容有商业推广或交易招揽", "排除局部否定、报道与引用语境"},
		},
		match: func(view messageView) []string {
			for _, part := range view.sections {
				if selector(part, view) && promotional(part) {
					return []string{"商业推广招揽", evidence}
				}
			}
			return nil
		},
	}
}

func topicDetector(id, name, category, evidence, condition string, strong *lexicon, weak func(section) bool) detector {
	return detector{
		info: RuleInfo{
			ID: id, Name: name, Category: category,
			Description: condition + "；单个主题词、新闻、反诈与医学讨论不单独触发。",
			Conditions:  []string{condition, "邻近内容有交易、招募或主动联系招揽", "不要求外部链接，排除局部否定与报道引用"},
		},
		match: func(view messageView) []string {
			for _, part := range view.sections {
				topic := strong.MatchString(part.text) || weak != nil && weak(part)
				if !topic && part.background != "" && directOffer.MatchString(part.text) &&
					(part.contact || part.links.any || selfSolicitation.MatchString(part.text) || priceOrDelivery.MatchString(part.text)) {
					topic = strong.MatchString(part.background)
				}
				if !topic || !commercialIntent(part) {
					continue
				}
				labels := []string{evidence, "交易或联系招揽"}
				if priceOrDelivery.MatchString(part.text) {
					labels = append(labels, "价格或结算承诺")
				}
				if part.button && part.links.any {
					labels = append(labels, "按钮导流")
				} else if part.contact {
					labels = append(labels, "联系入口")
				} else if part.links.any {
					labels = append(labels, "链接导流")
				}
				return labels
			}
			return nil
		},
	}
}

func commercialIntent(part section) bool {
	return part.buttonOffer || transaction.MatchString(part.text) ||
		selfSolicitation.MatchString(part.text) ||
		solicitation.MatchString(part.text) && (part.contact || part.links.any) ||
		priceOrDelivery.MatchString(part.text) && (part.contact || part.links.any || solicitation.MatchString(part.text))
}

func promotional(part section) bool {
	if part.buttonOnly && !part.buttonOffer {
		return false
	}
	if part.buttonOffer && part.links.any {
		return true
	}
	if promotionOffer.MatchString(part.text) &&
		(part.contact || part.links.any || selfSolicitation.MatchString(part.text)) {
		return true
	}
	return transaction.MatchString(part.text) &&
		(part.contact || part.links.any || priceOrDelivery.MatchString(part.text))
}

// Catalog returns independent metadata snapshots. Runtime choices are reported
// by Checker.Status; catalog entries describe the default enabled library.
func Catalog() []RuleInfo {
	items := make([]RuleInfo, len(registry))
	for i, detector := range registry {
		items[i] = detector.info
		items[i].Conditions = slices.Clone(detector.info.Conditions)
		items[i].Enabled = true
		items[i].Effective = true
	}
	return items
}
