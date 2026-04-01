package handleinline

import "context"

type RootUseCase struct {
	inlineHandler *HandleInlineUseCase
}

func NewRootUseCase(inlineHandler *HandleInlineUseCase) *RootUseCase {
	return &RootUseCase{inlineHandler: inlineHandler}
}

func (u *RootUseCase) Handle(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, nil
}
