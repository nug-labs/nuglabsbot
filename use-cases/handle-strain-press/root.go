/*
Package handlestrainpress handles inline callback presses for strain collection confirmations.
Consumes one-time press tokens atomically against strain_collection_encounters; user feedback is only AnswerCallbackQuery copy (no extra chat message).
*/

package handlestrainpress

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const CallbackDataPrefix = "scf:"

const strainPressReadCacheTTL = 0

// confirmStrainPressSQL consumes one strain_press_tokens row per token id and inserts a matching encounter atomically,
// returning the canonical strain plus the user's total encounters for that strain (including this row).
const confirmStrainPressSQL = `
WITH del AS (
  DELETE FROM strain_press_tokens WHERE id = $1 RETURNING strain_canonical
),
ins AS (
  INSERT INTO strain_collection_encounters (telegram_user_id, strain_canonical)
  SELECT $2::bigint, del.strain_canonical FROM del
  RETURNING strain_canonical
)
SELECT i.strain_canonical AS strain,
       (SELECT COUNT(*)::bigint FROM strain_collection_encounters e
        WHERE e.telegram_user_id = $2::bigint
          AND e.strain_canonical = i.strain_canonical) AS total
FROM ins i`

type RootUseCase struct {
	store     db.DB
	analytics *utils.Analytics
	log       *utils.Logger
}

func NewRootUseCase(store db.DB, analytics *utils.Analytics, log *utils.Logger) *RootUseCase {
	return &RootUseCase{store: store, analytics: analytics, log: log}
}

func ParseTokenID(callbackData string) (int64, bool) {
	s := strings.TrimSpace(callbackData)
	if !strings.HasPrefix(s, CallbackDataPrefix) {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(s, CallbackDataPrefix))
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 36, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// Handle records one encounter via single-use callback token (no rows ⇒ expired or reused). Returns text for AnswerCallbackQuery only.
func (u *RootUseCase) Handle(ctx context.Context, callback *tgbotapi.CallbackQuery) (answerText string, showAlert bool) {
	copy := utils.StrainCollectionMessages()
	var chatID int64
	var actorID int64
	if callback.From != nil {
		actorID = callback.From.ID
	}
	if callback.Message != nil {
		chatID = callback.Message.Chat.ID
	}

	if actorID == 0 {
		return copy.CallbackExpired, true
	}

	tokenID, parsed := ParseTokenID(callback.Data)
	if !parsed || u.store == nil {
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "strain-collection-press-invalid",
				UserID: actorID,
				Status: "invalid",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{}),
			})
		}
		if u.log != nil {
			u.log.Warn("event=strain-collection-press status=invalid reason=callback_payload")
		}
		return copy.CallbackExpired, true
	}

	var strain string
	var total int64
	row := u.store.QueryRowContext(ctx, confirmStrainPressSQL, strainPressReadCacheTTL, tokenID, actorID)
	if err := row.Scan(&strain, &total); err != nil {
		if err == sql.ErrNoRows {
			if u.analytics != nil {
				_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
					Name:   "strain-collection-press-miss",
					UserID: actorID,
					Status: "miss",
					Meta:   utils.MetaWithChatID(chatID, map[string]any{"token_id": tokenID}),
				})
			}
			if u.log != nil {
				u.log.Info("event=strain-collection-press status=miss reason=no_token_or_race token_id=%d user_id=%d", tokenID, actorID)
			}
			return copy.CallbackExpired, true
		}
		if u.log != nil {
			u.log.Warn("event=strain-collection-press status=error err=%v token_id=%d user_id=%d", err, tokenID, actorID)
		}
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "strain-collection-press-error",
				UserID: actorID,
				Status: "error",
				Meta: utils.MetaWithChatID(chatID, map[string]any{
					"token_id": tokenID,
					"error":    err.Error(),
				}),
			})
		}
		return copy.CallbackExpired, true
	}

	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-collection-confirm",
			UserID: actorID,
			Status: "ok",
			Meta: utils.MetaWithChatID(chatID, map[string]any{
				"strain":        strain,
				"encounter_cnt": total,
			}),
		})
	}
	if u.log != nil {
		u.log.Info("event=strain-collection-press status=ok user_id=%d strain=%q encounter_cnt=%d", actorID, strain, total)
	}

	return copy.CallbackRecorded, false
}
