/*
bg-services/handle-events runs the analytics queue consumer (handle-events use case).
Started from app.go alongside other background schedulers.
*/

package bgservices

import (
	"context"

	handleevents "nuglabsbot-v2/use-cases/handle-events"
	"nuglabsbot-v2/utils"
)

type HandleEventsService struct {
	uc  *handleevents.RootUseCase
	log *utils.Logger
}

func NewHandleEventsService(uc *handleevents.RootUseCase, log *utils.Logger) *HandleEventsService {
	return &HandleEventsService{uc: uc, log: log}
}

func (s *HandleEventsService) Run(ctx context.Context) {
	s.uc.Run(ctx)
}
