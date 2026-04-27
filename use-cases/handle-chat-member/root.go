/*
Package handlechatmember tracks leave/kick member updates from Telegram chat_member updates.
*/
package handlechatmember

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/utils"
)

type telegramBot interface {
	GetChatMembersCount(config tgbotapi.ChatMemberCountConfig) (int, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type RootUseCase struct {
	bot       telegramBot
	analytics *utils.Analytics
}

func NewRootUseCase(bot telegramBot, analytics *utils.Analytics) *RootUseCase {
	return &RootUseCase{bot: bot, analytics: analytics}
}

// HandleMessage handles member service messages that include message_id
// so we can delete the visible service tag and emit cleanup analytics.
func (u *RootUseCase) HandleMessage(ctx context.Context, update tgbotapi.Update) (string, error) {
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
		return "", nil
	}
	chatType := update.Message.Chat.Type
	if chatType != "group" && chatType != "supergroup" {
		return "", nil
	}

	eventName := "empty-join-cleanup"
	if isLeave {
		eventName = "empty-leave-cleanup"
	}
	chatID := update.Message.Chat.ID
	meta := map[string]any{
		"chat_type":               chatType,
		"message_id":              update.Message.MessageID,
		"via":                     "message",
		"service_message_deleted": false,
	}
	if n, err := u.bot.GetChatMembersCount(tgbotapi.ChatMemberCountConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}); err == nil && n >= 0 {
		meta["member_count"] = n
	}
	if isJoin {
		meta["joined"] = len(update.Message.NewChatMembers)
		var joinedUsernames []string
		for _, member := range update.Message.NewChatMembers {
			if member.UserName != "" {
				joinedUsernames = append(joinedUsernames, member.UserName)
			}
		}
		if len(joinedUsernames) > 0 {
			meta["joined_usernames"] = joinedUsernames
		}
		if update.Message.From != nil && update.Message.From.UserName != "" {
			meta["actor_username"] = update.Message.From.UserName
		}
	} else {
		meta["left_user_id"] = update.Message.LeftChatMember.ID
		if update.Message.LeftChatMember != nil && update.Message.LeftChatMember.UserName != "" {
			meta["left_username"] = update.Message.LeftChatMember.UserName
		}
		if update.Message.From != nil && update.Message.From.UserName != "" {
			meta["actor_username"] = update.Message.From.UserName
		}
	}

	del := tgbotapi.NewDeleteMessage(chatID, update.Message.MessageID)
	_, err := u.bot.Request(del)
	if err != nil {
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   eventName,
				UserID: actorUserID,
				Status: "error",
				Meta:   utils.MetaWithChatID(chatID, meta),
			})
		}
		return "", err
	}
	meta["service_message_deleted"] = true
	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   eventName,
			UserID: actorUserID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, meta),
		})
	}
	return "", nil
}

func (u *RootUseCase) Handle(ctx context.Context, cm *tgbotapi.ChatMemberUpdated) error {
	if cm == nil {
		return nil
	}
	chatType := cm.Chat.Type
	if chatType != "group" && chatType != "supergroup" {
		return nil
	}
	if !cm.NewChatMember.HasLeft() && !cm.NewChatMember.WasKicked() {
		return nil
	}
	if cm.NewChatMember.User == nil {
		return nil
	}

	leaverID := cm.NewChatMember.User.ID
	chatID := cm.Chat.ID
	meta := map[string]any{
		"chat_type":               chatType,
		"via":                     "chat_member",
		"left_user_id":            leaverID,
		"service_message_deleted": false,
	}
	if uname := cm.NewChatMember.User.UserName; uname != "" {
		meta["left_username"] = uname
	}
	if cm.From.ID != 0 && cm.From.ID != leaverID {
		meta["actor_user_id"] = cm.From.ID
		if actorUname := cm.From.UserName; actorUname != "" {
			meta["actor_username"] = actorUname
		}
	}
	if n, err := u.bot.GetChatMembersCount(tgbotapi.ChatMemberCountConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}); err == nil && n >= 0 {
		meta["member_count"] = n
	}

	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "empty-leave-cleanup",
			UserID: leaverID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, meta),
		})
	}
	return nil
}
