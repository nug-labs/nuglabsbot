// call handle-inline/handle-inline use case
// if event is inline query tap call handle-message/handle-strain use case

package controllers

import "context"

type InlineHandler interface {
	Handle(ctx context.Context, query string) ([]map[string]any, error)
}

type InlineController struct {
	inlineHandler InlineHandler
	strainHandler StrainHandler
}

func NewInlineController(inlineHandler InlineHandler, strainHandler StrainHandler) *InlineController {
	return &InlineController{
		inlineHandler: inlineHandler,
		strainHandler: strainHandler,
	}
}

func (c *InlineController) HandleQuery(ctx context.Context, query string) ([]map[string]any, error) {
	return c.inlineHandler.Handle(ctx, query)
}

func (c *InlineController) HandleTap(ctx context.Context, selected string) (string, error) {
	return c.strainHandler.Handle(ctx, selected)
}
