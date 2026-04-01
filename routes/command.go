// Telegram command route calls command.go controller

package routes

import (
	"context"

	"telegram-v2/controllers"
	"telegram-v2/middleware"
)

type CommandRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.CommandController
}

func NewCommandRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.CommandController) *CommandRoute {
	return &CommandRoute{middleware: middleware, controller: controller}
}

func (r *CommandRoute) Handle(ctx context.Context, user middleware.TelegramUser, command string, argument string) (string, error) {
	if err := r.middleware.EnsureUser(ctx, user); err != nil {
		return "", err
	}
	return r.controller.Handle(ctx, command, argument)
}
