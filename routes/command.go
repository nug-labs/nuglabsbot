/*
CommandRoute runs user middleware then CommandController.
Injected into UpdateRouter with logger for middleware failures.
*/

package routes

import (
	"context"

	"nuglabsbot-v2/controllers"
	"nuglabsbot-v2/middleware"
	"nuglabsbot-v2/utils"
)

type CommandRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.CommandController
	log        *utils.Logger
}

func NewCommandRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.CommandController, log *utils.Logger) *CommandRoute {
	return &CommandRoute{middleware: middleware, controller: controller, log: log}
}

func (r *CommandRoute) Handle(ctx context.Context, user middleware.TelegramUser, chatID int64, command string, argument string) (utils.OutboundMessage, error) {
	if err := r.middleware.EnsureUser(ctx, user, chatID); err != nil {
		if r.log != nil {
			r.log.Warn("command route: ensure user failed: %v", err)
		}
		return utils.OutboundMessage{}, err
	}
	return r.controller.Handle(ctx, user.TelegramID, chatID, command, argument)
}
