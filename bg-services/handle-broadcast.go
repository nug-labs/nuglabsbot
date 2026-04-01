// Has db util injected into it, service inits a listener poller once a minute reads from broadcast table
// zz-ops/convert-broadcasts-yml converts our assets/broadcasts yml files into table rows
// It is a separate stand-alone helper script that is not part of the main app
// This is so the app is always running during updating the broadcasts

// handle-broadcast.go is the main service that handles the broadcast logic
// It is responsible for the scheduling / running of handle-broadcasts/root.go in a minute loop

package bgservices

import (
	"context"
	"time"

	"telegram-v2/utils"
)

type BroadcastRunner interface {
	RunOnce() error
}

type HandleBroadcastService struct {
	runner BroadcastRunner
	log    *utils.Logger
}

func NewHandleBroadcastService(runner BroadcastRunner, log *utils.Logger) *HandleBroadcastService {
	return &HandleBroadcastService{runner: runner, log: log}
}

func (s *HandleBroadcastService) RunEvery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.runner.RunOnce(); err != nil && s.log != nil {
				s.log.Error("broadcast run failed: %v", err)
			}
		}
	}
}
