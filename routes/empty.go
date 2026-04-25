/*
EmptyRoute handles update messages that have no text/caption (joins, pins, settings events).
*/
package routes

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/controllers"
	"nuglabsbot-v2/utils"
)

type EmptyRoute struct {
	controller *controllers.EmptyController
	log        *utils.Logger
}

func NewEmptyRoute(controller *controllers.EmptyController, log *utils.Logger) *EmptyRoute {
	return &EmptyRoute{controller: controller, log: log}
}

func (r *EmptyRoute) Handle(ctx context.Context, update tgbotapi.Update) (string, error) {
	reply, err := r.controller.Handle(ctx, update)
	if err != nil && r.log != nil {
		r.log.Warn("empty route: handle failed: %v", err)
	}
	return reply, err
}
