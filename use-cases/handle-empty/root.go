/*
Package handleempty tracks empty-text Telegram messages and returns no reply.
*/
package handleempty

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/utils"
)

type RootUseCase struct {
	analytics *utils.Analytics
}

func NewRootUseCase(analytics *utils.Analytics) *RootUseCase {
	return &RootUseCase{analytics: analytics}
}

func (u *RootUseCase) Handle(ctx context.Context, update tgbotapi.Update) (string, error) {
	if update.Message == nil {
		return "", nil
	}
	actorUserID := int64(0)
	if update.Message.From != nil {
		actorUserID = update.Message.From.ID
	}

	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "empty-received",
			UserID: actorUserID,
			Status: "ok",
			Meta: utils.MetaWithChatID(update.Message.Chat.ID, map[string]any{
				"chat_type":            update.Message.Chat.Type,
				"message_id":           update.Message.MessageID,
				"has_new_chat_members": len(update.Message.NewChatMembers) > 0,
				"has_left_chat_member": update.Message.LeftChatMember != nil,
			}),
		})
	}
	return "", nil
}
