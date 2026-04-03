/*
HandleUserMiddleware upserts users on each routed update (commands, messages, inline).
Optional analytics records invalid Telegram payloads (zero ID).
*/

package middleware

import (
	"context"
	"fmt"
	"time"

	"telegram-v2/utils"
	"telegram-v2/utils/db"
)

type TelegramUser struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
}

type HandleUserMiddleware struct {
	store     db.DB
	analytics *utils.Analytics
	deferred  *db.DeferredWriteQueue
}

func NewHandleUserMiddleware(store db.DB, analytics *utils.Analytics, deferred *db.DeferredWriteQueue) *HandleUserMiddleware {
	return &HandleUserMiddleware{store: store, analytics: analytics, deferred: deferred}
}

func (m *HandleUserMiddleware) EnsureUser(ctx context.Context, u TelegramUser) error {
	if u.TelegramID == 0 {
		if m.analytics != nil {
			_ = m.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "user-ensure-rejected",
				Status: "invalid",
				Meta:   map[string]any{"reason": "zero_telegram_id"},
			})
		}
		return fmt.Errorf("We are not sure you're a real user 🫤")
	}

	if m.deferred != nil {
		uCopy := u
		if err := m.deferred.Enqueue(func(c context.Context, conn db.DB) error {
			return upsertUser(c, conn, uCopy)
		}); err == nil {
			return nil
		}
	}
	return upsertUser(ctx, m.store, u)
}

func upsertUser(ctx context.Context, conn db.DB, u TelegramUser) error {
	_, err := conn.ExecContext(
		ctx,
		`INSERT INTO users (telegram_id, username, first_name, last_name, total_requests, last_seen_at)
		 VALUES ($1, $2, $3, $4, 1, $5)
		 ON CONFLICT (telegram_id)
		 DO UPDATE SET
		   username = COALESCE(NULLIF(EXCLUDED.username, ''), users.username),
		   first_name = COALESCE(NULLIF(EXCLUDED.first_name, ''), users.first_name),
		   last_name = COALESCE(NULLIF(EXCLUDED.last_name, ''), users.last_name),
		   total_requests = users.total_requests + 1,
		   last_seen_at = EXCLUDED.last_seen_at`,
		u.TelegramID, u.Username, u.FirstName, u.LastName, time.Now().UTC(),
	)
	return err
}
