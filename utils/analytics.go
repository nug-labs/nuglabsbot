/*
Package utils analytics persists events to app_analytics.
Injected from composition root into use-cases; emit TrackEvent at decision points.
TrackBroadcast was removed — callers use TrackEvent with name "broadcast" and meta.type.
See analytics.md in the telegram-v2 module root for the full event catalog.
*/
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

type AnalyticsFactory struct{}

var AnalyticsManager = NewAnalyticsFactory()

func NewAnalyticsFactory() *AnalyticsFactory {
	return &AnalyticsFactory{}
}

func (f *AnalyticsFactory) Init(db *Database, log *Logger) *Analytics {
	return NewAnalytics(db, log)
}

func NewAnalytics(db *Database, log *Logger) *Analytics {
	return &Analytics{db: db, log: log}
}

// MetaWithChatID builds meta with chat_id set last (Telegram chat where the update occurred). AnalyticsEvent.UserID should be the actor (From.ID).
func MetaWithChatID(chatID int64, fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return map[string]any{"chat_id": chatID}
	}
	out := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		out[k] = v
	}
	out["chat_id"] = chatID
	return out
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
