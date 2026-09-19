package settings

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/ports"
)

type fakeStore struct {
	stored  *domain.BotSettings
	saved   []domain.BotSettings
	saveErr error
}

func (f *fakeStore) GetBotSettings(context.Context) (domain.BotSettings, error) {
	if f.stored == nil {
		return domain.BotSettings{}, ports.ErrBotSettingsNotFound
	}
	return f.stored.Clone(), nil
}

func (f *fakeStore) SaveBotSettings(_ context.Context, next domain.BotSettings) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	clone := next.Clone()
	f.stored = &clone
	f.saved = append(f.saved, clone)
	return nil
}

func TestManagerUsesEnvironmentDefaultsUntilSomethingIsSaved(t *testing.T) {
	store := &fakeStore{}
	manager, err := NewManager(context.Background(), domain.BotSettings{
		BioCheckEnabled:      true,
		CrossGroupManagement: false,
		OwnerUserIDs:         []int64{42},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Settings()
	if !current.BioCheckEnabled || !current.IsOwner(42) || current.CrossGroupManagement {
		t.Fatalf("defaults were not applied: %+v", current)
	}
	if len(store.saved) != 0 {
		t.Fatalf("loading wrote to the store: %+v", store.saved)
	}
}

func TestManagerStoredRowWinsOverDefaults(t *testing.T) {
	stored := domain.BotSettings{BioCheckEnabled: false, CrossGroupManagement: true, OwnerUserIDs: []int64{7}}
	manager, err := NewManager(context.Background(), domain.BotSettings{BioCheckEnabled: true}, &fakeStore{stored: &stored})
	if err != nil {
		t.Fatal(err)
	}
	current := manager.Settings()
	if current.BioCheckEnabled || !current.CrossGroupManagement || !current.IsOwner(7) {
		t.Fatalf("stored settings were not loaded: %+v", current)
	}
}

func TestManagerUpdatePersistsThenPublishes(t *testing.T) {
	manager, err := NewManager(context.Background(), domain.BotSettings{}, &fakeStore{})
	if err != nil {
		t.Fatal(err)
	}
	bio, cross := true, true
	owners := []int64{9, 5, 9}
	updated, err := manager.Update(context.Background(), domain.BotSettingsPatch{
		BioCheckEnabled: &bio, CrossGroupManagement: &cross, OwnerUserIDs: &owners,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.BioCheckEnabled || !updated.CrossGroupManagement {
		t.Fatalf("update did not apply: %+v", updated)
	}
	if !slices.Equal(updated.OwnerUserIDs, []int64{5, 9}) {
		t.Fatalf("owner list was not de-duplicated and sorted: %v", updated.OwnerUserIDs)
	}
	// The published snapshot must not alias the caller's slice.
	owners[0] = 99
	if manager.Settings().IsOwner(99) {
		t.Fatal("published settings alias the caller's slice")
	}
	if manager.Settings().IsOwner(0) || manager.Settings().IsOwner(-1) {
		t.Fatal("zero or negative IDs must never be owners")
	}
}

func TestManagerKeepsRuntimeValueWhenTheWriteFails(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("database is down")}
	manager, err := NewManager(context.Background(), domain.BotSettings{BioCheckEnabled: true}, store)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := manager.Update(context.Background(), domain.BotSettingsPatch{BioCheckEnabled: &off}); err == nil {
		t.Fatal("expected the failed write to surface")
	}
	if !manager.Settings().BioCheckEnabled {
		t.Fatal("a failed write changed what the bot enforces")
	}
}

func TestManagerRejectsInvalidOwners(t *testing.T) {
	manager, err := NewManager(context.Background(), domain.BotSettings{}, &fakeStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, owners := range [][]int64{{0}, {-5}, {1, 0}} {
		candidate := owners
		if _, err := manager.Update(context.Background(), domain.BotSettingsPatch{OwnerUserIDs: &candidate}); !errors.Is(err, ErrInvalidSettings) {
			t.Fatalf("owners %v accepted: %v", owners, err)
		}
	}
	many := make([]int64, domain.MaxBotOwners+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	if _, err := manager.Update(context.Background(), domain.BotSettingsPatch{OwnerUserIDs: &many}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("an oversized owner list was accepted: %v", err)
	}
	if len(manager.Settings().OwnerUserIDs) != 0 {
		t.Fatal("invalid input changed the published settings")
	}
}

func TestManagerSetBioCheckAndMemoryVariant(t *testing.T) {
	store := &fakeStore{}
	manager, err := NewManager(context.Background(), domain.BotSettings{}, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetBioCheck(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !manager.Settings().BioCheckEnabled || len(store.saved) != 1 {
		t.Fatalf("bio check shortcut failed: %+v", store.saved)
	}
	memory := NewMemoryManager(domain.BotSettings{CrossGroupManagement: true})
	if !memory.Settings().CrossGroupManagement {
		t.Fatal("memory manager ignored its defaults")
	}
	if _, err := memory.Update(context.Background(), domain.BotSettingsPatch{}); err == nil {
		t.Fatal("memory manager must not pretend to persist")
	}
	// A nil manager reports zero values so callers keep documented defaults.
	var nilManager *Manager
	if settings := nilManager.Settings(); settings.BioCheckEnabled || settings.CrossGroupManagement {
		t.Fatalf("nil manager returned %+v", settings)
	}
}
