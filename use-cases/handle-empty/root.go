/*
Package handleempty deletes Telegram's "user joined" service tag in group chats.
*/
package handleempty

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/utils"
)

type telegramRequester interface {
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type RootUseCase struct {
	bot       telegramRequester
	analytics *utils.Analytics
}

func NewRootUseCase(bot telegramRequester, analytics *utils.Analytics) *RootUseCase {
	return &RootUseCase{bot: bot, analytics: analytics}
}

func (u *RootUseCase) Handle(ctx context.Context, update tgbotapi.Update) (string, error) {
	if update.Message == nil {
		return "", nil
	}
	actorUserID := int64(0)
	if update.Message.From != nil {
		actorUserID = update.Message.From.ID
	}
	if len(update.Message.NewChatMembers) == 0 {
		u.track(ctx, actorUserID, update.Message.Chat.ID, "miss", map[string]any{
			"reason":    "no_new_chat_members",
			"chat_type": update.Message.Chat.Type,
		})
		return "", nil
	}
	chatType := update.Message.Chat.Type
	if chatType != "group" && chatType != "supergroup" {
		u.track(ctx, actorUserID, update.Message.Chat.ID, "miss", map[string]any{
			"reason":    "unsupported_chat_type",
			"chat_type": chatType,
		})
		return "", nil
	}
	del := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
	_, err := u.bot.Request(del)
	if err != nil {
		u.track(ctx, actorUserID, update.Message.Chat.ID, "error", map[string]any{
			"chat_type":  chatType,
			"message_id": update.Message.MessageID,
			"joined":     len(update.Message.NewChatMembers),
		})
		return "", err
	}
	u.track(ctx, actorUserID, update.Message.Chat.ID, "ok", map[string]any{
		"chat_type":  chatType,
		"message_id": update.Message.MessageID,
		"joined":     len(update.Message.NewChatMembers),
	})
	return "", nil
}

func (u *RootUseCase) track(ctx context.Context, actorUserID, chatID int64, status string, meta map[string]any) {
	if u.analytics == nil {
		return
	}
	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   "empty-join-cleanup",
		UserID: actorUserID,
		Status: status,
		Meta:   utils.MetaWithChatID(chatID, meta),
	})
}
