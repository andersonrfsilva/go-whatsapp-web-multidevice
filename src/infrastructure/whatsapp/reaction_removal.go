package whatsapp

import (
	"context"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow/types/events"
)

type removedReactionCtxKey struct{}

// withRemovedReactionEmoji resolves the emoji a reaction-removal event erases
// and carries it on the context, since the row is deleted from chat storage
// before the webhook payload is built (asynchronously). Returns ctx unchanged
// for normal reactions, or when nothing is stored for that reactor/message.
func withRemovedReactionEmoji(ctx context.Context, evt *events.Message, repo domainChatStorage.IChatStorageRepository) context.Context {
	if repo == nil || evt == nil || evt.Message == nil {
		return ctx
	}
	if utils.UnwrapMessage(evt.Message).GetReactionMessage().GetText() != "" {
		return ctx
	}

	stored, err := repo.GetReactionForEvent(ctx, evt)
	if err != nil {
		log.Warnf("Failed to resolve removed reaction for %s: %v", evt.Info.ID, err)
		return ctx
	}
	if stored == nil || stored.Emoji == "" {
		return ctx
	}
	return context.WithValue(ctx, removedReactionCtxKey{}, stored.Emoji)
}

// removedReactionEmojiFromContext returns the emoji stashed by
// withRemovedReactionEmoji, or "" when unknown.
func removedReactionEmojiFromContext(ctx context.Context) string {
	emoji, _ := ctx.Value(removedReactionCtxKey{}).(string)
	return emoji
}
