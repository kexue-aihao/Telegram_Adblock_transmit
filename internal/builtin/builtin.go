// Package builtin implements the offline advertising library shipped with the
// application. A topic alone is never an advertising verdict: detectors require
// an offer, solicitation or other independent commercial evidence.
package builtin

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

const LibraryVersion = "2.0.0"

// Analysis is also the response used by the non-destructive preview API.
// Evidence contains fixed labels only, never excerpts or contact information.
type Analysis struct {
	Enabled        bool                `json:"enabled"`
	Matched        bool                `json:"matched"`
	LibraryVersion string              `json:"library_version"`
	Hits           []domain.BuiltinHit `json:"hits"`
}

func (a Analysis) HitIDs() []string {
	if len(a.Hits) == 0 {
		return nil
	}
	ids := make([]string, len(a.Hits))
	for i, hit := range a.Hits {
		ids[i] = hit.ID
	}
	return ids
}

func cloneHits(hits []domain.BuiltinHit) []domain.BuiltinHit {
	result := slices.Clone(hits)
	for i := range result {
		result[i].Evidence = slices.Clone(result[i].Evidence)
	}
	return result
}

func (a Analysis) Details() *domain.BuiltinDetails {
	if len(a.Hits) == 0 {
		return nil
	}
	return &domain.BuiltinDetails{LibraryVersion: a.LibraryVersion, Hits: cloneHits(a.Hits)}
}

// Checker publishes immutable settings snapshots and is safe for concurrent use.
type Checker struct {
	settings atomic.Pointer[domain.BuiltinSettings]
	updateMu sync.Mutex
	store    ports.BuiltinSettingsStore
}

func New(enabled bool) *Checker {
	c := &Checker{}
	c.settings.Store(&domain.BuiltinSettings{Enabled: enabled})
	return c
}

func (c *Checker) Enabled() bool { return c != nil && c.Settings().Enabled }

// Detect preserves the original interface for existing callers.
func (c *Checker) Detect(message domain.ModerationMessage) []string {
	return c.Analyze(message).HitIDs()
}

// Analyze performs no network or storage operations and never changes message.
func (c *Checker) Analyze(message domain.ModerationMessage) Analysis {
	result := Analysis{LibraryVersion: LibraryVersion, Hits: []domain.BuiltinHit{}}
	if c == nil {
		return result
	}
	settings := c.Settings()
	result.Enabled = settings.Enabled
	if !settings.Enabled {
		return result
	}
	view := buildView(message)
	for _, detector := range registry {
		if slices.Contains(settings.DisabledRules, detector.info.ID) {
			continue
		}
		if evidence := detector.match(view); len(evidence) > 0 {
			result.Hits = append(result.Hits, domain.BuiltinHit{
				ID: detector.info.ID, Name: detector.info.Name,
				Category: detector.info.Category, Evidence: evidence,
			})
		}
	}
	result.Matched = len(result.Hits) > 0
	return result
}
