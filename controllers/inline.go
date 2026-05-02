/*
InlineController forwards inline queries to handle-inline (search) and tap to handle-strain.
*/

package controllers

import (
	"context"

	"nuglabsbot-v2/utils"
)

type InlineHandler interface {
	Handle(ctx context.Context, userID, chatID int64, query string) ([]map[string]any, error)
}

type InlineStrainHandler interface {
	Handle(ctx context.Context, userID, chatID int64, input string) (utils.OutboundMessage, error)
}

type InlineController struct {
	inlineHandler InlineHandler
	strainHandler InlineStrainHandler
}

func NewInlineController(inlineHandler InlineHandler, strainHandler InlineStrainHandler) *InlineController {
	return &InlineController{
		inlineHandler: inlineHandler,
		strainHandler: strainHandler,
	}
}

func (c *InlineController) HandleQuery(ctx context.Context, userID, chatID int64, query string) ([]map[string]any, error) {
	return c.inlineHandler.Handle(ctx, userID, chatID, query)
}

func (c *InlineController) HandleTap(ctx context.Context, actorUserID, chatID int64, selected string) (utils.OutboundMessage, error) {
	return c.strainHandler.Handle(ctx, actorUserID, chatID, selected)
}
