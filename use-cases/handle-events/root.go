// Package handleevents drains the in-memory analytics queue and inserts into app_analytics.
// Started from bg-services/handle-events; keeps update handlers off the DB insert path.
package handleevents

import (
	"context"
	"encoding/json"
	"time"

	"telegram-v2/utils"
	"telegram-v2/utils/db"
)

const insertTimeout = 15 * time.Second

type RootUseCase struct {
	store db.DB
	log   *utils.Logger
	q     *utils.Analytics
}

func NewRootUseCase(store db.DB, q *utils.Analytics, log *utils.Logger) *RootUseCase {
	return &RootUseCase{store: store, q: q, log: log}
}

func (u *RootUseCase) Run(ctx context.Context) {
	for {
		e, err := u.q.Next(ctx)
		if err != nil {
			u.flushPending()
			return
		}
		u.persist(e)
	}
}

func (u *RootUseCase) flushPending() {
	for {
		e, ok := u.q.TryDequeue()
		if !ok {
			return
		}
		u.persist(e)
	}
}

func (u *RootUseCase) persist(e utils.AnalyticsEvent) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	meta, err := json.Marshal(e.Meta)
	if err != nil {
		if u.log != nil {
			u.log.Warn("analytics marshal meta: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), insertTimeout)
	defer cancel()

	_, err = u.store.ExecContext(
		ctx,
		`INSERT INTO app_analytics (event_name, user_id, entity_id, event_status, event_at, meta)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		e.Name, e.UserID, e.EntityID, e.Status, e.Timestamp, string(meta),
	)
	if err != nil && u.log != nil {
		u.log.Warn("analytics insert failed: %v", err)
	}
}
