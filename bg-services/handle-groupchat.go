/*
bg-services/handle-groupchat triggers periodic group outreach checks.
Started from app.go only when APP_ENV=live (alongside broadcast scheduler).
*/
package bgservices

import (
	"context"
	"time"

	"telegram-v2/utils"
)

type GroupchatRunner interface {
	RunOnce() error
}

type HandleGroupchatService struct {
	runner GroupchatRunner
	log    *utils.Logger
}

func NewHandleGroupchatService(runner GroupchatRunner, log *utils.Logger) *HandleGroupchatService {
	return &HandleGroupchatService{runner: runner, log: log}
}

func (s *HandleGroupchatService) RunEvery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.runner.RunOnce(); err != nil && s.log != nil {
				s.log.Error("groupchat run failed: %v", err)
			}
		}
	}
}
