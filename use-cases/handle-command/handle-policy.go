// Determine which policy and which md file to return from assets/policies/*.md

package handlecommand

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"telegram-v2/utils"
)

type HandlePolicyUseCase struct {
	analytics *utils.Analytics
}

func NewHandlePolicyUseCase(analytics *utils.Analytics) *HandlePolicyUseCase {
	return &HandlePolicyUseCase{analytics: analytics}
}

func (u *HandlePolicyUseCase) Handle(ctx context.Context, policyName string) (string, error) {
	_ = ctx
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		policyName = "help"
	}
	path := filepath.Join(".", "assets", "policies", policyName+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read policy %s: %w", policyName, err)
	}
	return string(raw), nil
}
