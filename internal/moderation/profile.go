package moderation

import (
	"context"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/builtin"
	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

func (s *Service) matchingBioEntry(ctx context.Context, message domain.ModerationMessage) (*domain.NewAuditEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.profiles == nil || !s.botSettings().BioCheckEnabled || !s.builtin.Enabled() || message.UserIsBot ||
		message.UserID == nil || *message.UserID <= 0 || message.SenderChatID != nil ||
		!builtin.HasProfileSolicitation(message) {
		return nil, nil
	}
	bio, err := s.profiles.GetUserBio(ctx, *message.UserID)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || bio == "" {
		return nil, nil
	}
	analysis := s.builtin.AnalyzeBio(message, bio)
	if !analysis.Matched {
		return nil, nil
	}
	hits := analysis.HitIDs()
	s.logger.Info("user bio ad filter hit", "chat_id", message.ChatID, "message_id", message.MessageID,
		"hits", hits, "library_version", analysis.LibraryVersion)
	return &domain.NewAuditEntry{
		ChatID: message.ChatID, ChatTitle: message.ChatTitle, MessageThreadID: message.MessageThreadID,
		UserID: message.UserID, MessageID: message.MessageID, BuiltinHits: hits,
		BuiltinDetails: analysis.Details(), Content: message.Content(),
	}, nil
}
