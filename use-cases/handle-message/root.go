package handlemessage

import (
	"context"
	"net/url"
	"strings"
	"telegram-v2/utils"
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

func (u *RootUseCase) Handle(ctx context.Context, input string) (string, error) {
	input = strings.TrimSpace(input)

	if isLikelyURL(input) {
		return u.handleURL.Handle(ctx, input)
	}

	msg, err := u.handleStrain.Handle(ctx, input)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(msg), "No matching strain found.") {
		return u.handleUnknown.Handle(ctx, input)
	}
	return msg, nil
}

func isLikelyURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme != "" && u.Host != ""
}
