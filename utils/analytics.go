package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type AnalyticsEvent struct {
	Name      string
	UserID    int64
	EntityID  string
	Status    string
	Timestamp time.Time
	Meta      map[string]any
}

type Analytics struct {
	db  *Database
	log *Logger
}

func NewAnalytics(db *Database, log *Logger) *Analytics {
	return &Analytics{db: db, log: log}
}

func (a *Analytics) TrackBroadcast(ctx context.Context, broadcastID string, userID int64, kind string, status string) error {
	return a.TrackEvent(ctx, AnalyticsEvent{
		Name:     "broadcast",
		UserID:   userID,
		EntityID: broadcastID,
		Status:   status,
		Meta: map[string]any{
			"type": kind,
		},
	})
}

func (a *Analytics) TrackEvent(ctx context.Context, e AnalyticsEvent) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	meta, err := json.Marshal(e.Meta)
	if err != nil {
		return fmt.Errorf("marshal analytics meta: %w", err)
	}

	_, err = a.db.ExecContext(
		ctx,
		`INSERT INTO app_analytics (event_name, user_id, entity_id, event_status, event_at, meta)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		e.Name, e.UserID, e.EntityID, e.Status, e.Timestamp, string(meta),
	)
	if err != nil {
		if a.log != nil {
			a.log.Warn("analytics event dropped: %v", err)
		}
		return err
	}
	return nil
}
