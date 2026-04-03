/*
handle-policy loads Telegram-safe HTML from assets/policies/<name>.html.
Used via RootUseCase so policy-requested analytics fire in one place.
*/

package handlecommand

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type HandlePolicyUseCase struct{}

func NewHandlePolicyUseCase() *HandlePolicyUseCase {
	return &HandlePolicyUseCase{}
}

func (u *HandlePolicyUseCase) Handle(ctx context.Context, policyName string) (string, error) {
	_ = ctx
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		policyName = "help"
	}
	path := filepath.Join(".", "assets", "policies", policyName+".html")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read policy %s: %w", policyName, err)
	}
	return string(raw), nil
}
