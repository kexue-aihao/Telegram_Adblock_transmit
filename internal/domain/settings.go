package domain

import "slices"

// MaxBotOwners bounds the configured owner list. Every owner may manage the bot
// in any group, so the list is deliberately short and reviewable.
const MaxBotOwners = 50

// BotSettings holds the runtime switches that are not per-group: the optional
// profile (bio) check, the cross-group management permission and the bot owner
// list. They are edited from the WebUI panel or from the /settings command and
// persist across restarts.
type BotSettings struct {
	// BioCheckEnabled turns the profile-assisted check on for every group.
	BioCheckEnabled bool
	// CrossGroupManagement lets users who are not administrators of the current
	// group run management commands. It never exempts their command text from
	// advertising moderation.
	CrossGroupManagement bool
	// OwnerUserIDs are the bot owners. They manage every group without being a
	// member administrator, which is the fallback for the bot creator.
	OwnerUserIDs []int64
}

// Clone returns an independent copy so callers cannot mutate a published
// snapshot through the owner slice.
func (s BotSettings) Clone() BotSettings {
	return BotSettings{
		BioCheckEnabled:      s.BioCheckEnabled,
		CrossGroupManagement: s.CrossGroupManagement,
		OwnerUserIDs:         slices.Clone(s.OwnerUserIDs),
	}
}

// IsOwner reports whether the Telegram user ID is a configured bot owner.
func (s BotSettings) IsOwner(userID int64) bool {
	return userID > 0 && slices.Contains(s.OwnerUserIDs, userID)
}

// BotSettingsPatch is a partial update. Nil fields keep their current value.
type BotSettingsPatch struct {
	BioCheckEnabled      *bool
	CrossGroupManagement *bool
	OwnerUserIDs         *[]int64
}

// Apply returns settings with the patch applied.
func (s BotSettings) Apply(patch BotSettingsPatch) BotSettings {
	next := s.Clone()
	if patch.BioCheckEnabled != nil {
		next.BioCheckEnabled = *patch.BioCheckEnabled
	}
	if patch.CrossGroupManagement != nil {
		next.CrossGroupManagement = *patch.CrossGroupManagement
	}
	if patch.OwnerUserIDs != nil {
		next.OwnerUserIDs = slices.Clone(*patch.OwnerUserIDs)
	}
	return next
}
