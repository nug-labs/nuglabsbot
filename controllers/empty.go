/*
EmptyController forwards join/system message updates to handle-empty use case.
*/
package controllers

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type EmptyHandler interface {
	Handle(ctx context.Context, update tgbotapi.Update) (string, error)
}

type EmptyController struct {
	handler EmptyHandler
}

func NewEmptyController(handler EmptyHandler) *EmptyController {
	return &EmptyController{handler: handler}
}

func (c *EmptyController) Handle(ctx context.Context, update tgbotapi.Update) (string, error) {
	return c.handler.Handle(ctx, update)
}
