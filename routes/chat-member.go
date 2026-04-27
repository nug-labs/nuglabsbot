/*
ChatMemberRoute handles Telegram chat_member updates.
*/
package routes

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/controllers"
	"nuglabsbot-v2/utils"
)

type ChatMemberRoute struct {
	controller *controllers.ChatMemberController
	log        *utils.Logger
}

func NewChatMemberRoute(controller *controllers.ChatMemberController, log *utils.Logger) *ChatMemberRoute {
	return &ChatMemberRoute{controller: controller, log: log}
}

func (r *ChatMemberRoute) Handle(ctx context.Context, cm *tgbotapi.ChatMemberUpdated) error {
	err := r.controller.Handle(ctx, cm)
	if err != nil && r.log != nil {
		r.log.Warn("chat-member route: handle failed: %v", err)
	}
	return err
}

func (r *ChatMemberRoute) HandleMessage(ctx context.Context, update tgbotapi.Update) (string, error) {
	reply, err := r.controller.HandleMessage(ctx, update)
	if err != nil && r.log != nil {
		r.log.Warn("chat-member route message: handle failed: %v", err)
	}
	return reply, err
}
