// Telegram inline route calls inline.go controller

package routes

import (
	"context"

	"telegram-v2/controllers"
	"telegram-v2/middleware"
)

type InlineRoute struct {
	middleware *middleware.HandleUserMiddleware
	controller *controllers.InlineController
}

func NewInlineRoute(middleware *middleware.HandleUserMiddleware, controller *controllers.InlineController) *InlineRoute {
	return &InlineRoute{middleware: middleware, controller: controller}
}

func (r *InlineRoute) HandleQuery(ctx context.Context, user middleware.TelegramUser, query string) ([]map[string]any, error) {
	if err := r.middleware.EnsureUser(ctx, user); err != nil {
		return nil, err
	}
	return r.controller.HandleQuery(ctx, query)
}

func (r *InlineRoute) HandleTap(ctx context.Context, user middleware.TelegramUser, selected string) (string, error) {
	if err := r.middleware.EnsureUser(ctx, user); err != nil {
		return "", err
	}
	return r.controller.HandleTap(ctx, selected)
}
