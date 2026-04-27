/*
ChatMemberController forwards chat_member updates to handle-chat-member use case.
*/
package controllers

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type ChatMemberHandler interface {
	Handle(ctx context.Context, cm *tgbotapi.ChatMemberUpdated) error
	HandleMessage(ctx context.Context, update tgbotapi.Update) (string, error)
}

type ChatMemberController struct {
	handler ChatMemberHandler
}

func NewChatMemberController(handler ChatMemberHandler) *ChatMemberController {
	return &ChatMemberController{handler: handler}
}

func (c *ChatMemberController) Handle(ctx context.Context, cm *tgbotapi.ChatMemberUpdated) error {
	return c.handler.Handle(ctx, cm)
}

func (c *ChatMemberController) HandleMessage(ctx context.Context, update tgbotapi.Update) (string, error) {
	return c.handler.HandleMessage(ctx, update)
}
