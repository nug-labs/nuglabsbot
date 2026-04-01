package handlecommand

import "context"

type RootUseCase struct {
	policyHandler *HandlePolicyUseCase
}

func NewRootUseCase(policyHandler *HandlePolicyUseCase) *RootUseCase {
	return &RootUseCase{policyHandler: policyHandler}
}

func (u *RootUseCase) Handle(_ context.Context, _ string) (string, error) {
	return "", nil
}
