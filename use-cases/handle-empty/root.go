/*
Package handleempty deletes Telegram member join/leave service messages in group chats.
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

	isJoin := len(update.Message.NewChatMembers) > 0
	isLeave := update.Message.LeftChatMember != nil
	if !isJoin && !isLeave {
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "empty-join-cleanup",
				UserID: actorUserID,
				Status: "miss",
				Meta: utils.MetaWithChatID(update.Message.Chat.ID, map[string]any{
					"reason":    "not_member_service",
					"chat_type": update.Message.Chat.Type,
				}),
			})
		}
		return "", nil
	}

	eventName := "empty-join-cleanup"
	if isLeave {
		eventName = "empty-leave-cleanup"
	}

	chatType := update.Message.Chat.Type
	if chatType != "group" && chatType != "supergroup" {
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   eventName,
				UserID: actorUserID,
				Status: "miss",
				Meta: utils.MetaWithChatID(update.Message.Chat.ID, map[string]any{
					"reason":    "unsupported_chat_type",
					"chat_type": chatType,
				}),
			})
		}
		return "", nil
	}

	del := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
	_, err := u.bot.Request(del)
	if err != nil {
		meta := map[string]any{
			"chat_type":  chatType,
			"message_id": update.Message.MessageID,
		}
		if isJoin {
			meta["joined"] = len(update.Message.NewChatMembers)
		} else {
			meta["left_user_id"] = update.Message.LeftChatMember.ID
		}
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   eventName,
				UserID: actorUserID,
				Status: "error",
				Meta:   utils.MetaWithChatID(update.Message.Chat.ID, meta),
			})
		}
		return "", err
	}
	okMeta := map[string]any{
		"chat_type":  chatType,
		"message_id": update.Message.MessageID,
	}
	if isJoin {
		okMeta["joined"] = len(update.Message.NewChatMembers)
	} else {
		okMeta["left_user_id"] = update.Message.LeftChatMember.ID
	}
	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   eventName,
			UserID: actorUserID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(update.Message.Chat.ID, okMeta),
		})
	}
	return "", nil
}
