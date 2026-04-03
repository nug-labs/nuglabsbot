/*
handlesubscribe toggles subscriptions per chat id and emits subscription-enabled / subscription-disabled analytics.
Invoked from CommandController on /subscribe.
*/
package handlesubscribe

import (
	"context"
	"database/sql"
	"errors"

	"telegram-v2/utils"
)

type RootUseCase struct {
	db        utils.DB
	analytics *utils.Analytics
}

func NewRootUseCase(db utils.DB, analytics *utils.Analytics) *RootUseCase {
	return &RootUseCase{db: db, analytics: analytics}
}

func (u *RootUseCase) Handle(ctx context.Context, chatID int64) (string, error) {
	var enabled bool
	err := u.db.QueryRowContext(
		ctx,
		`SELECT enabled FROM subscriptions WHERE telegram_id = $1`,
		chatID,
	).Scan(&enabled)

	if errors.Is(err, sql.ErrNoRows) {
		_, insErr := u.db.ExecContext(
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
	_, err = u.db.ExecContext(
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
