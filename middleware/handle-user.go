// One middleware is injected the db stub
// Sees if user telegram ID is in the table
// If not, create user record
// If yes increment total request count and last_seen timestamp

package middleware

import (
	"context"
	"fmt"
	"time"

	"telegram-v2/utils"
)

type TelegramUser struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
}

type HandleUserMiddleware struct {
	db utils.DB
}

func NewHandleUserMiddleware(db utils.DB) *HandleUserMiddleware {
	return &HandleUserMiddleware{db: db}
}

func (m *HandleUserMiddleware) EnsureUser(ctx context.Context, u TelegramUser) error {
	if u.TelegramID == 0 {
		return fmt.Errorf("telegram id is required")
	}

	_, err := m.db.ExecContext(
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
