/*
CommandController maps normalized Telegram commands to handlers.
/start deep links → strain use-case; other commands → policy root (HTML) or subscribe.
Emits command-requested analytics. Injected from app.go with routes/command as entry.
*/

package controllers

import (
	"context"
	"strings"
	"nuglabsbot-v2/utils"
)

type StrainHandler interface {
	Handle(ctx context.Context, actorUserID, chatID int64, input string) (utils.OutboundMessage, error)
}

type PolicyHandler interface {
	Handle(ctx context.Context, actorUserID, chatID int64, policyName string) (string, error)
}

type SubscribeHandler interface {
	Handle(ctx context.Context, chatID int64) (string, error)
}

type CommandController struct {
	strainHandler    StrainHandler
	policyHandler    PolicyHandler
	subscribeHandler SubscribeHandler
	analytics        *utils.Analytics
}

func NewCommandController(strainHandler StrainHandler, policyHandler PolicyHandler, subscribeHandler SubscribeHandler, analytics *utils.Analytics) *CommandController {
	return &CommandController{
		strainHandler:    strainHandler,
		policyHandler:    policyHandler,
		subscribeHandler: subscribeHandler,
		analytics:        analytics,
	}
}

func (c *CommandController) Handle(ctx context.Context, actorUserID, chatID int64, command string, argument string) (utils.OutboundMessage, error) {
	cmd := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command, "/")))
	arg := strings.TrimSpace(argument)

	if c.analytics != nil {
		_ = c.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "command-requested",
			UserID: actorUserID,
			Status: "ok",
			Meta: utils.MetaWithChatID(chatID, map[string]any{
				"command": cmd,
				"has_arg": arg != "",
			}),
		})
	}

	switch cmd {
	case "start":
		if arg != "" {
			normalized := strings.ReplaceAll(arg, "-", " ")
			return c.strainHandler.Handle(ctx, actorUserID, chatID, normalized)
		}
		if chatID < 0 {
			return utils.OutboundMessage{}, nil
		}
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "start")
		return utils.OutboundMessage{Text: s}, err
	case "subscribe":
		s, err := c.subscribeHandler.Handle(ctx, chatID)
		return utils.OutboundMessage{Text: s}, err
	case "help":
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "help")
		return utils.OutboundMessage{Text: s}, err
	case "about":
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "about")
		return utils.OutboundMessage{Text: s}, err
	case "legal":
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "legal")
		return utils.OutboundMessage{Text: s}, err
	case "links":
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "links")
		return utils.OutboundMessage{Text: s}, err
	default:
		s, err := c.policyHandler.Handle(ctx, actorUserID, chatID, "help")
		return utils.OutboundMessage{Text: s}, err
	}
}
