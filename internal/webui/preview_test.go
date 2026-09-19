//go:build webuipreview

package webui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

// TestWebUIPreview serves the real panel handlers with disposable in-memory
// data. It never opens a database or connects to Telegram.
func TestWebUIPreview(t *testing.T) {
	addr := os.Getenv("WEBUI_PREVIEW_ADDR")
	if addr == "" {
		t.Skip("set WEBUI_PREVIEW_ADDR=127.0.0.1:8765 to run the local preview")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("preview must bind to 127.0.0.1")
	}
	s, ruleStore, audit, _, _, _ := newTestPanel(t, nil)
	s.auth, err = newAuthService("admin", "preview-only", []byte("webui-preview-session"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	patterns := []string{
		"免费.*领取", "返利|红包群", "t\\.me/\\+[a-zA-Z0-9_-]+", "博彩|彩票|日赚",
		"扫码.*加群", "内部.*名额", "客服.*优惠券", "@[a-zA-Z0-9_]+bot.*福利",
		"兼职.*赚钱", "bit\\.ly/|tinyurl\\.com/", "导师.*外快", "秒到.*红包",
	}
	for i := 0; i < 27; i++ {
		ruleStore.rules = append(ruleStore.rules, domain.Rule{
			ID: int64(i + 1), ChatID: -1, Pattern: patterns[i%len(patterns)],
			Enabled: i%4 != 0, CreatedBy: -1,
			CreatedAt: now.AddDate(0, 0, -i-1), UpdatedAt: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	ruleStore.nextID = 28
	ruleStore.rules[1].Pattern = "(?:免费领取|超长广告关键词|" + strings.Repeat("推广活动|", 35) + "返利).*客服"
	s.options.ChatStore = &fakeChatStore{chats: []domain.ChatSummary{
		{ID: -1001234567, Title: "技术交流社区"},
		{ID: -1002345678, Title: "开源项目讨论组"},
		{ID: -1003456789, Title: "产品反馈与问题追踪讨论组"},
	}}
	audit.entries = nil
	chats := []int64{-1001234567, -1002345678, -1003456789}
	samples := []string{
		"承接洗资业务，联系 @example_agent；催情药现货批发，联系 @example_agent",
		"催情药现货批发，联系 @example_agent",
		"社工库个人信息打包出售，联系 @example_agent",
	}
	for i := 0; i < 96; i++ {
		uid := int64(100001 + i)
		entry := domain.AuditEntry{
			ID: int64(96 - i), ChatID: chats[i%3], MessageID: 3000 + i, UserID: &uid,
			MatchedRuleIDs: []int64{int64(i%12 + 1)}, BuiltinHits: []string{"ad_bot_mention"},
			ContentSummary:  "免费领取活动名额，请联系 @adservicebot 了解详情。",
			DeleteSucceeded: i%7 != 0, OccurredAt: now.AddDate(0, 0, -i/4).Add(-time.Duration(i%4) * time.Minute),
		}
		switch i % 5 {
		case 0, 1, 2:
			entry.ContentSummary = samples[i%5]
			analysis := s.options.BuiltinFilter.Analyze(domain.ModerationMessage{Text: entry.ContentSummary})
			if !analysis.Matched {
				t.Fatalf("preview advertising sample was not detected: %d", i%5)
			}
			entry.BuiltinHits = nil
			for _, hit := range analysis.Hits {
				entry.BuiltinHits = append(entry.BuiltinHits, hit.ID)
			}
			entry.BuiltinDetails = &domain.BuiltinDetails{LibraryVersion: analysis.LibraryVersion, Hits: analysis.Hits}
		case 3:
			// Legacy records have IDs but no explanation snapshot.
			entry.BuiltinHits = []string{"ad_bot_mention"}
		case 4:
			entry.BuiltinHits = []string{"ad_legacy_unknown"}
		}
		if i%7 == 0 {
			entry.DeletionError = "Bad Request: not enough rights to delete messages"
		}
		if i == 0 {
			entry.ContentSummary = "免费领取活动名额\n" + strings.Repeat("超长消息摘要与异常内容 ", 5) + "<img src=x onerror=alert(1)>"
		}
		audit.entries = append(audit.entries, entry)
	}
	s.options.AuditStore = &previewAudit{fakePanelAudit: audit, rules: ruleStore}
	// The existing handler-test fakes are not concurrent stores.
	var mu sync.Mutex
	handler := s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("X-WebUI-Preview", "true")
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/assets/") {
			http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))).ServeHTTP(w, r)
		} else if r.Method == http.MethodGet && r.URL.Path == "/" {
			http.ServeFile(w, r, "assets/index.html")
		} else {
			s.router.ServeHTTP(w, r)
		}
	}))
	t.Logf("WebUI preview: http://%s (admin / preview-only)", addr)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	t.Fatal(server.ListenAndServe())
}

type previewAudit struct {
	*fakePanelAudit
	rules *fakeRuleStore
}

func (p *previewAudit) ListAudit(_ context.Context, q domain.AuditQuery) (domain.AuditPage, error) {
	var entries []domain.AuditEntry
	for _, e := range p.entries {
		if q.ChatID != nil && *q.ChatID != e.ChatID ||
			q.Success != nil && *q.Success != e.DeleteSucceeded ||
			q.From != nil && e.OccurredAt.Before(*q.From) ||
			q.To != nil && !e.OccurredAt.Before(*q.To) ||
			q.RuleID != nil && !slices.Contains(e.MatchedRuleIDs, *q.RuleID) {
			continue
		}
		entries = append(entries, e)
	}
	page, size := max(1, q.Page), q.PageSize
	if size == 0 {
		size = 20
	}
	start := min((page-1)*size, len(entries))
	end := min(start+size, len(entries))
	return domain.AuditPage{Items: entries[start:end], Total: int64(len(entries)), Page: page, PageSize: size}, nil
}

func (p *previewAudit) StatsOverview(context.Context) (domain.AuditStatsOverview, error) {
	stats := domain.AuditStatsOverview{TotalChats: 3, TotalRules: int64(len(p.rules.rules)), TotalHits: int64(len(p.entries))}
	for _, rule := range p.rules.rules {
		if rule.Enabled {
			stats.EnabledRules++
		}
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for _, entry := range p.entries {
		if !entry.OccurredAt.Before(today.AddDate(0, 0, -6)) {
			stats.Hits7Day++
		}
		if entry.OccurredAt.Before(today) {
			continue
		}
		stats.HitsToday++
		if entry.DeleteSucceeded {
			stats.DeletedToday++
		} else {
			stats.FailedToday++
		}
	}
	return stats, nil
}

func (p *previewAudit) StatsByDay(_ context.Context, days int, chatID *int64) ([]domain.AuditDayStat, error) {
	if days < 1 {
		return nil, fmt.Errorf("days must be positive")
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]domain.AuditDayStat, days)
	for i := range out {
		out[i].Date = today.AddDate(0, 0, i-days+1)
		for _, entry := range p.entries {
			if chatID != nil && *chatID != entry.ChatID || entry.OccurredAt.UTC().Truncate(24*time.Hour) != out[i].Date {
				continue
			}
			out[i].Hits++
			if entry.DeleteSucceeded {
				out[i].Deleted++
			} else {
				out[i].Failed++
			}
		}
	}
	return out, nil
}
