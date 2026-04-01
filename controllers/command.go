// Understand if /start, /start=strain+name
// Other options include /privacy-policy, /terms-of-service, /help, /about, /contact, /feedback, /support, /faq, /legal
// All of these other than strain deeplink should be md files returned from assets/policies/*.md please
// Strain deeplink is handle-message/handle-strain use-case
// Otherwise I guess it is handle-command/handle-policy use-case

package controllers

import (
	"context"
	"strings"
)

type StrainHandler interface {
	Handle(ctx context.Context, input string) (string, error)
}

type PolicyHandler interface {
	Handle(ctx context.Context, policyName string) (string, error)
}

type CommandController struct {
	strainHandler StrainHandler
	policyHandler PolicyHandler
}

func NewCommandController(strainHandler StrainHandler, policyHandler PolicyHandler) *CommandController {
	return &CommandController{
		strainHandler: strainHandler,
		policyHandler: policyHandler,
	}
}

func (c *CommandController) Handle(ctx context.Context, command string, argument string) (string, error) {
	cmd := strings.ToLower(strings.TrimSpace(command))
	arg := strings.TrimSpace(argument)

	if cmd == "/start" && arg != "" {
		return c.strainHandler.Handle(ctx, arg)
	}

	if strings.HasPrefix(cmd, "/") {
		cmd = strings.TrimPrefix(cmd, "/")
	}

	switch cmd {
	case "privacy-policy", "terms-of-service", "help", "about", "contact", "feedback", "support", "faq", "legal":
		return c.policyHandler.Handle(ctx, cmd)
	default:
		return c.policyHandler.Handle(ctx, "help")
	}
}
