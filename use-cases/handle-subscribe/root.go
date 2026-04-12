/*
handlesubscribe toggles subscriptions per chat id and emits subscription-enabled / subscription-disabled analytics.
Invoked from CommandController on /subscribe.
*/
package handlesubscribe

import (
	"context"
	"database/sql"
	"errors"

	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const subscriptionStateReadCacheTTL = 0

type RootUseCase struct {
	store     db.DB
	analytics *utils.Analytics
}

func NewRootUseCase(store db.DB, analytics *utils.Analytics) *RootUseCase {
	return &RootUseCase{store: store, analytics: analytics}
}

func (u *RootUseCase) Handle(ctx context.Context, chatID int64) (string, error) {
	var enabled bool
	err := u.store.QueryRowContext(
		ctx,
		`SELECT enabled FROM subscriptions WHERE telegram_id = $1`,
		subscriptionStateReadCacheTTL,
		chatID,
	).Scan(&enabled)

	if errors.Is(err, sql.ErrNoRows) {
		_, insErr := u.store.ExecContext(
			ctx,
			`INSERT INTO subscriptions (telegram_id, enabled) VALUES ($1, TRUE)
			 ON CONFLICT (telegram_id) DO UPDATE SET enabled = TRUE, updated_at = NOW()`,
			chatID,
		)
		if insErr != nil {
			return "", insErr
		}
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "subscription-enabled",
			UserID: chatID,
			Status: "ok",
		})
		return "Subscription enabled.", nil
	}
	if err != nil {
		return "", err
	}

	nextEnabled := !enabled
	_, err = u.store.ExecContext(
		ctx,
		`UPDATE subscriptions SET enabled = $2, updated_at = NOW() WHERE telegram_id = $1`,
		chatID, nextEnabled,
	)
	if err != nil {
		return "", err
	}

	if nextEnabled {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "subscription-enabled",
			UserID: chatID,
			Status: "ok",
		})
		return "Subscription enabled.", nil
	}

	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   "subscription-disabled",
		UserID: chatID,
		Status: "ok",
	})
	return "Subscription disabled.", nil
}
