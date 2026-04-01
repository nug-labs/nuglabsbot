// Has db stub injected in
// Pulls broadcasts and user ids from broadcast and users tables
// Pulls from broadcast_outgoing where sent_time is null
// Check that scheduled_at is in the past, if not skip
// For each broadcast, determine which broadcast type it is, message or quiz
// Uses requres use-cases to handle the broadcast
// Updates broadcast_outgoing table with sent_time and broadcast_id

package handlebroadcast

import (
	"context"
	"encoding/json"
	"time"

	"telegram-v2/utils"
)

type RootUseCase struct {
	db               utils.DB
	analytics        *utils.Analytics
	messageBroadcast MessageBroadcaster
	quizBroadcaster  QuizBroadcaster
}

func NewRootUseCase(
	db utils.DB,
	analytics *utils.Analytics,
	messageBroadcaster MessageBroadcaster,
	quizBroadcaster QuizBroadcaster,
) *RootUseCase {
	return &RootUseCase{
		db:               db,
		analytics:        analytics,
		messageBroadcast: messageBroadcaster,
		quizBroadcaster:  quizBroadcaster,
	}
}

func (u *RootUseCase) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rows, err := u.db.QueryContext(
		ctx,
		`SELECT bo.broadcast_id, bo.user_id, b.type, b.payload
		 FROM broadcast_outgoing bo
		 INNER JOIN broadcasts b ON b.id = bo.broadcast_id
		 WHERE bo.sent_time IS NULL
		   AND (bo.scheduled_at IS NULL OR bo.scheduled_at <= NOW())
		 ORDER BY bo.created_at ASC
		 LIMIT 500`,
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

		switch kind {
		case "message":
			text, _ := payload["text"].(string)
			if text != "" && u.messageBroadcast != nil {
				if err := u.messageBroadcast.SendMessage(userID, text); err != nil {
					continue
				}
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
					if err := u.quizBroadcaster.SendQuiz(userID, question, options, correctIndex); err != nil {
						continue
					}
				}
			}
		}

		if _, err := u.db.ExecContext(
			ctx,
			`UPDATE broadcast_outgoing
			 SET sent_time = NOW()
			 WHERE broadcast_id = $1 AND user_id = $2 AND sent_time IS NULL`,
			broadcastID, userID,
		); err != nil {
			return err
		}

		_ = u.analytics.TrackBroadcast(ctx, broadcastID, userID, kind, "sent")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}
