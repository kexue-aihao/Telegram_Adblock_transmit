package webui

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func TestAuditPreservesBuiltinExplanationSnapshot(t *testing.T) {
	s, _, audit, _, _, handler := newTestPanel(t, nil)
	// Audit display must remain available without a configured live detector.
	s.options.BuiltinFilter = nil
	details := &domain.BuiltinDetails{
		LibraryVersion: "previous-version",
		Hits:           []domain.BuiltinHit{{ID: "retired_detector", Name: "当时的检测名称", Category: "历史分类", Evidence: []string{"交易招揽", "联系方式"}}},
	}
	audit.entries[0].BuiltinHits = []string{"retired_detector"}
	audit.entries[0].BuiltinDetails = details
	for _, path := range []string{"/api/audit", "/api/audit/1"} {
		res := authedRequest(t, handler, http.MethodGet, path, "", true)
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body)
		}
		var entry auditEntryDTO
		if path == "/api/audit" {
			var page auditListResponse
			if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
				t.Fatalf("audit list: %s", res.Body)
			}
			entry = page.Items[0]
		} else if err := json.Unmarshal(res.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(entry.BuiltinDetails, details) || !reflect.DeepEqual(entry.BuiltinHits, audit.entries[0].BuiltinHits) {
			t.Fatalf("audit snapshot changed: %+v", entry)
		}
	}
	audit.entries[0].BuiltinDetails = nil
	res := authedRequest(t, handler, http.MethodGet, "/api/audit/1", "", true)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), `"builtin_details"`) || !strings.Contains(res.Body.String(), "retired_detector") {
		t.Fatalf("legacy audit compatibility: %d %s", res.Code, res.Body)
	}
}
