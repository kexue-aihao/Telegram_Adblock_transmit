package builtin

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type memorySettings struct {
	mu       sync.Mutex
	settings *domain.BuiltinSettings
	err      error
}

func (m *memorySettings) GetBuiltinSettings(context.Context) (domain.BuiltinSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return domain.BuiltinSettings{}, m.err
	}
	if m.settings == nil {
		return domain.BuiltinSettings{}, ports.ErrBuiltinSettingsNotFound
	}
	return domain.BuiltinSettings{Enabled: m.settings.Enabled, DisabledRules: slices.Clone(m.settings.DisabledRules)}, nil
}

func (m *memorySettings) SaveBuiltinSettings(_ context.Context, settings domain.BuiltinSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	settings.DisabledRules = slices.Clone(settings.DisabledRules)
	m.settings = &settings
	return nil
}

func TestManagedSettingsPersistAndOverrideDefaults(t *testing.T) {
	ctx := context.Background()
	store := &memorySettings{}
	checker, err := NewManaged(ctx, false, store)
	if err != nil || checker.Enabled() {
		t.Fatalf("initial settings: %v, %v", checker, err)
	}
	on := true
	if _, err := checker.Update(ctx, &on, map[string]bool{HitInviteLinkShort: false}); err != nil {
		t.Fatal(err)
	}
	if hits := checker.Detect(msg("限时优惠，立即下单 t.me/+abc")); slices.Contains(hits, HitInviteLinkShort) {
		t.Fatalf("disabled invite hits: %v", hits)
	}
	if hits := checker.Detect(msg("限时优惠，立即下单 bit.ly/abc")); !slices.Contains(hits, HitShortLink) {
		t.Fatalf("other rule disabled: %v", hits)
	}
	restarted, err := NewManaged(ctx, false, store)
	if err != nil || !restarted.Enabled() {
		t.Fatalf("restart ignored database override: %v", err)
	}
	if hits := restarted.Detect(msg("限时优惠，立即下单 t.me/+abc")); slices.Contains(hits, HitInviteLinkShort) {
		t.Fatalf("restart lost per-rule setting: %v", hits)
	}
	snapshot := restarted.Settings()
	snapshot.DisabledRules[0] = HitShortLink
	if hits := restarted.Detect(msg("限时优惠，立即下单 t.me/+abc")); slices.Contains(hits, HitInviteLinkShort) {
		t.Fatal("caller mutated active settings")
	}
}

func TestManagedSettingsFailureDoesNotChangeDetection(t *testing.T) {
	ctx := context.Background()
	store := &memorySettings{}
	checker, err := NewManaged(ctx, true, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checker.Update(ctx, nil, map[string]bool{"unknown": false, HitShortLink: false}); !errors.Is(err, ErrUnknownRule) {
		t.Fatalf("unknown rule accepted: %v", err)
	}
	if store.settings != nil {
		t.Fatal("invalid patch was persisted")
	}
	store.err = errors.New("write failed")
	off := false
	if _, err := checker.Update(ctx, &off, map[string]bool{HitShortLink: false}); err == nil {
		t.Fatal("save failure ignored")
	}
	if hits := checker.Detect(msg("限时优惠，立即下单 bit.ly/abc")); !slices.Contains(hits, HitShortLink) {
		t.Fatalf("failed save changed detection: %v", hits)
	}
	if _, err := NewManaged(ctx, true, store); err == nil {
		t.Fatal("read failure silently used defaults")
	}
}

func TestManagedSettingsDisableEveryDetector(t *testing.T) {
	for id, message := range detectorFixtures() {
		t.Run(id, func(t *testing.T) {
			checker, err := NewManaged(context.Background(), true, &memorySettings{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(checker.Detect(message), id) {
				t.Fatal("fixture does not trigger detector")
			}
			if _, err := checker.Update(context.Background(), nil, map[string]bool{id: false}); err != nil {
				t.Fatal(err)
			}
			if hits := checker.Detect(message); slices.Contains(hits, id) {
				t.Fatalf("disabled detector still hit: %v", hits)
			}
		})
	}
}

func TestManagedSettingsConcurrentPartialUpdates(t *testing.T) {
	store := &memorySettings{}
	checker, err := NewManaged(context.Background(), true, store)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, info := range checker.Status().Rules {
		wg.Go(func() {
			for i := 0; i < 30; i++ {
				if _, err := checker.Update(context.Background(), nil, map[string]bool{info.ID: false}); err != nil {
					t.Error(err)
				}
				checker.Detect(msg("限时优惠，立即下单 t.me/+abc bit.ly/abc"))
				checker.Status()
			}
		})
	}
	wg.Wait()
	saved, _ := store.GetBuiltinSettings(context.Background())
	if len(saved.DisabledRules) != len(Catalog()) || !slices.Equal(saved.DisabledRules, checker.Settings().DisabledRules) {
		t.Fatalf("lost concurrent update or cache/store mismatch: %+v", saved)
	}
}
