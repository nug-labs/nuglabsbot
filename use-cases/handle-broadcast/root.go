// Has db stub injected in
// Pulls broadcasts and user ids from broadcast and users tables
// Pulls from broadcast_outgoing where sent_time is null
// Check that scheduled_at is in the past, if not skip
// For each broadcast, determine which broadcast type it is, message or quiz
// Uses requres use-cases to handle the broadcast
// Updates broadcast_outgoing with sent_time and telegram_message_id after a successful send

package handlebroadcast

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"telegram-v2/utils"
	"telegram-v2/utils/db"
)

type RootUseCase struct {
	store            db.DB
	analytics        *utils.Analytics
	messageBroadcast MessageBroadcaster
	quizBroadcaster  QuizBroadcaster
}

func NewRootUseCase(
	store db.DB,
	analytics *utils.Analytics,
	messageBroadcaster MessageBroadcaster,
	quizBroadcaster QuizBroadcaster,
) *RootUseCase {
	return &RootUseCase{
		store:            store,
		analytics:        analytics,
		messageBroadcast: messageBroadcaster,
		quizBroadcaster:  quizBroadcaster,
	}
}

func (u *RootUseCase) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rows, err := u.store.QueryContext(
		ctx,
		`SELECT bo.broadcast_id, bo.user_id, b.type, b.payload
		 FROM broadcast_outgoing bo
		 INNER JOIN broadcasts b ON b.id = bo.broadcast_id
		 WHERE bo.sent_time IS NULL
		   AND (bo.scheduled_at IS NULL OR bo.scheduled_at <= NOW())
		 ORDER BY bo.created_at ASC
		 LIMIT 500`,
		0,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var broadcastID string
		var userID int64
		var kind string
		var payloadRaw []byte
		if err := rows.Scan(&broadcastID, &userID, &kind, &payloadRaw); err != nil {
			return err
		}

		var payload map[string]any
		_ = json.Unmarshal(payloadRaw, &payload)

		var tgMsg sql.NullInt64
		sentOK := false

		switch kind {
		case "message":
			text, _ := payload["text"].(string)
			if text != "" && u.messageBroadcast != nil {
				mid, err := u.messageBroadcast.SendMessage(userID, text)
				if err != nil {
					continue
				}
				tgMsg = sql.NullInt64{Int64: mid, Valid: true}
				sentOK = true
			}
		case "quiz":
			if u.quizBroadcaster != nil {
				question, _ := payload["question"].(string)
				optionsAny, _ := payload["options"].([]any)
				options := make([]string, 0, len(optionsAny))
				for _, v := range optionsAny {
					s, ok := v.(string)
					if ok {
						options = append(options, s)
					}
				}
				correctIndex := 0
				if ci, ok := payload["correct_index"].(float64); ok {
					correctIndex = int(ci)
				}
				if question != "" && len(options) >= 2 {
					mid, err := u.quizBroadcaster.SendQuiz(userID, question, options, correctIndex)
					if err != nil {
						continue
					}
					tgMsg = sql.NullInt64{Int64: mid, Valid: true}
					sentOK = true
				}
			}
		}

		if !sentOK {
			continue
		}

		if _, err := u.store.ExecContext(
			ctx,
			`UPDATE broadcast_outgoing
			 SET sent_time = NOW(),
			     telegram_message_id = $3
			 WHERE broadcast_id = $1 AND user_id = $2 AND sent_time IS NULL`,
			broadcastID, userID, tgMsg,
		); err != nil {
			return err
		}

		meta := map[string]any{"type": kind}
		if tgMsg.Valid {
			meta["telegram_message_id"] = tgMsg.Int64
		}
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:     "broadcast",
			UserID:   userID,
			EntityID: broadcastID,
			Status:   "sent",
			Meta:     meta,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}
