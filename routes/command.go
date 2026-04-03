/*
CommandRoute runs user middleware then CommandController.
Injected into UpdateRouter with logger for middleware failures.
*/

package routes

import (
	"context"

	"telegram-v2/controllers"
	"telegram-v2/middleware"
	"telegram-v2/utils"
)

type CommandRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.CommandController
	log        *utils.Logger
}

func NewCommandRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.CommandController, log *utils.Logger) *CommandRoute {
	return &CommandRoute{middleware: middleware, controller: controller, log: log}
}

func (r *CommandRoute) Handle(ctx context.Context, user middleware.TelegramUser, chatID int64, command string, argument string) (string, error) {
	if err := r.middleware.EnsureUser(ctx, user); err != nil {
		if r.log != nil {
			r.log.Warn("command route: ensure user failed: %v", err)
		}
		return "", err
	}
	return r.controller.Handle(ctx, user.TelegramID, chatID, command, argument)
}
