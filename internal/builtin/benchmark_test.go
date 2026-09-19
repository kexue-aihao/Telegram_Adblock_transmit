package builtin

import (
	"strings"
	"testing"
)

func BenchmarkAnalyze(b *testing.B) {
	checker := New(true)
	cases := map[string]string{
		"benign_short":   "这是本周读书会的讨论摘要，欢迎分享观点。",
		"priority_ad":    "承接洗资业务，支持 USDT 结算，联系 @example_agent",
		"medical_report": "医学科普：催情药的危险成分和中毒救治。医生提醒，催情药售卖广告涉及危险成分，请勿购买。https://health.example.invalid",
		"long_benign":    strings.Repeat("今天我们讨论开源技术与社区活动。", 240),
		"long_tail_ad":   strings.Repeat("普通文字和交流内容 ", 430) + "催情药现货批发，联系 @example_agent",
	}
	for name, text := range cases {
		b.Run(name, func(b *testing.B) {
			message := msg(text)
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			for b.Loop() {
				checker.Analyze(message)
			}
		})
	}
}
