// Look in responses table inejct in db stub
//  Normalise input aginst regex of only alpha numeric characters
// If key exists return value from messages as msg
// othersie return value from "default" key value as msg

package handlemessage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"telegram-v2/utils"
	"telegram-v2/utils/db"
)

type HandleUnknownUseCase struct {
	store     db.DB
	analytics *utils.Analytics
}

func NewHandleUnknownUseCase(store db.DB, analytics *utils.Analytics) *HandleUnknownUseCase {
	return &HandleUnknownUseCase{store: store, analytics: analytics}
}

func (u *HandleUnknownUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (string, error) {
	key := normalizeKey(input)
	msg, err := lookupResponse(ctx, u.store, key)
	if err != nil {
		return "", err
	}
	if msg == "" {
		msg, err = lookupResponse(ctx, u.store, "default")
		if err != nil {
			return "", err
		}
	}
	if msg == "" {
		msg = "Unknown query"
	}

	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   "unknown-response",
		UserID: actorUserID,
		Status: "ok",
		Meta:   utils.MetaWithChatID(chatID, map[string]any{"key": key}),
	})
	return msg, nil
}

func normalizeKey(in string) string {
	nonAlnum := regexp.MustCompile(`[^a-zA-Z0-9 ]+`)
	out := strings.ToLower(strings.TrimSpace(in))
	out = nonAlnum.ReplaceAllString(out, "")
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func lookupResponse(ctx context.Context, conn db.DB, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	var message string
	err := conn.QueryRowContext(ctx, `SELECT message FROM responses WHERE key = $1`, 2*time.Minute, key).Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup response %q: %w", key, err)
	}
	return message, nil
}
