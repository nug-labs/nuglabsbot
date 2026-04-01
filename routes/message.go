// Telegram message route calls message.go controller

package routes

import (
	"context"

	"telegram-v2/controllers"
	"telegram-v2/middleware"
)

type MessageRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.MessageController
}

func NewMessageRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.MessageController) *MessageRoute {
	return &MessageRoute{middleware: middleware, controller: controller}
}

func (r *MessageRoute) Handle(ctx context.Context, user middleware.TelegramUser, message string) (string, error) {
	if err := r.middleware.EnsureUser(ctx, user); err != nil {
		return "", err
	}
	return r.controller.Handle(ctx, message)
}
