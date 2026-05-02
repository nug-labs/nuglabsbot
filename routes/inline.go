/*
InlineRoute runs user middleware then InlineController for inline queries.
*/

package routes

import (
	"context"

	"nuglabsbot-v2/controllers"
	"nuglabsbot-v2/middleware"
	"nuglabsbot-v2/utils"
)

type InlineRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.InlineController
	log        *utils.Logger
}

func NewInlineRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.InlineController, log *utils.Logger) *InlineRoute {
	return &InlineRoute{middleware: middleware, controller: controller, log: log}
}

func (r *InlineRoute) HandleQuery(ctx context.Context, user middleware.TelegramUser, query string) ([]map[string]any, error) {
	if err := r.middleware.EnsureUser(ctx, user, user.TelegramID); err != nil {
		if r.log != nil {
			r.log.Warn("inline route: ensure user failed: %v", err)
		}
		return nil, err
	}
	// Private chat: chat id equals user id; inline `Update` has no Chat field.
	return r.controller.HandleQuery(ctx, user.TelegramID, user.TelegramID, query)
}

func (r *InlineRoute) HandleTap(ctx context.Context, user middleware.TelegramUser, selected string) (utils.OutboundMessage, error) {
	if err := r.middleware.EnsureUser(ctx, user, user.TelegramID); err != nil {
		if r.log != nil {
			r.log.Warn("inline route tap: ensure user failed: %v", err)
		}
		return utils.OutboundMessage{}, err
	}
	// Private chat id equals user id; inline has no separate chat in this route.
	return r.controller.HandleTap(ctx, user.TelegramID, user.TelegramID, selected)
}
