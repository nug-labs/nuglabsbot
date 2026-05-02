package handlemessage

import (
	"context"
	"net/url"
	"strings"
	"nuglabsbot-v2/utils"
)

type RootUseCase struct {
	handleURL     *HandleURLUseCase
	handleStrain  *HandleStrainUseCase
	handleUnknown *HandleUnknownUseCase
	analytics     *utils.Analytics
}

func NewRootUseCase(
	handleURL *HandleURLUseCase,
	handleStrain *HandleStrainUseCase,
	handleUnknown *HandleUnknownUseCase,
	analytics *utils.Analytics,
) *RootUseCase {
	return &RootUseCase{
		handleURL:     handleURL,
		handleStrain:  handleStrain,
		handleUnknown: handleUnknown,
		analytics:     analytics,
	}
}

func (u *RootUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (utils.OutboundMessage, error) {
	input = strings.TrimSpace(input)

	if isLikelyURL(input) {
		return u.handleURL.Handle(ctx, actorUserID, chatID, input)
	}

	out, err := u.handleStrain.Handle(ctx, actorUserID, chatID, input)
	if err != nil {
		return utils.OutboundMessage{}, err
	}
	if strings.EqualFold(strings.TrimSpace(out.Text), "No matching strain found.") {
		txt, err := u.handleUnknown.Handle(ctx, actorUserID, chatID, input)
		return utils.OutboundMessage{Text: txt}, err
	}
	return out, nil
}

func isLikelyURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme != "" && u.Host != ""
}
