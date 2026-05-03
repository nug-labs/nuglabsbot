/*
Package handlestrainpress handles inline callback presses for strain collection.

Two token kinds:

  - parity: original private-search card (+/- parity on totals; only NULL source_token encounters).
  - additive: refreshed/supplemental DM cards (+1 per press tied to token; second press removes that tied row).

Shared tokens are not consumed; each user resolves independently.
*/

package handlestrainpress

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	handlemessage "nuglabsbot-v2/use-cases/handle-message"
	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const pressInteractionAdditive = "additive"

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
	if !strings.HasPrefix(s, handlemessage.StrainCollectionCallbackPrefix) {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(s, handlemessage.StrainCollectionCallbackPrefix))
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 36, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// Handle adjusts collection state for the authenticated user according to token kind (parity vs additive).
func (u *RootUseCase) Handle(ctx context.Context, callback *tgbotapi.CallbackQuery) (answerText string, showAlert bool, ok *handlemessage.StrainCollectConfirm) {
	msgs := handlemessage.StrainCollectionMessages()
	var chatID int64
	var actorID int64
	if callback.From != nil {
		actorID = callback.From.ID
	}
	if callback.Message != nil {
		chatID = callback.Message.Chat.ID
	}

	if actorID == 0 {
		return msgs.CallbackExpired, true, nil
	}

	tokenID, parsed := ParseTokenID(callback.Data)
	if !parsed || u.store == nil {
		u.trackOptional(ctx, "strain-collection-press-invalid", actorID, chatID, "invalid", map[string]any{})
		if u.log != nil {
			u.log.Warn("event=strain-collection-press status=invalid reason=callback_payload")
		}
		return msgs.CallbackExpired, true, nil
	}

	tx, err := u.store.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		u.trackOptional(ctx, "strain-collection-press-error", actorID, chatID, "error", map[string]any{"where": "begin_tx", "error": err.Error()})
		return msgs.CallbackExpired, true, nil
	}

	var strain string
	var kind string
	err = tx.QueryRowContext(ctx,
		`SELECT strain_canonical, interaction_kind FROM strain_press_tokens WHERE id = $1`,
		tokenID,
	).Scan(&strain, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		u.trackOptional(ctx, "strain-collection-press-miss", actorID, chatID, "miss", map[string]any{"token_id": tokenID})
		if u.log != nil {
			u.log.Info("event=strain-collection-press status=miss reason=no_token token_id=%d user_id=%d", tokenID, actorID)
		}
		return msgs.CallbackExpired, true, nil
	}
	if err != nil {
		if !db.IsUndefinedColumn(err) {
			if u.log != nil {
				u.log.Warn("event=strain-collection-press status=token_read_error err=%v", err)
			}
			_ = tx.Rollback()
			return msgs.CallbackExpired, true, nil
		}
		err2 := tx.QueryRowContext(ctx,
			`SELECT strain_canonical FROM strain_press_tokens WHERE id = $1`,
			tokenID,
		).Scan(&strain)
		if errors.Is(err2, sql.ErrNoRows) {
			_ = tx.Rollback()
			u.trackOptional(ctx, "strain-collection-press-miss", actorID, chatID, "miss", map[string]any{"token_id": tokenID})
			return msgs.CallbackExpired, true, nil
		}
		if err2 != nil {
			if u.log != nil {
				u.log.Warn("event=strain-collection-press status=legacy_token_read_error err=%v", err2)
			}
			_ = tx.Rollback()
			return msgs.CallbackExpired, true, nil
		}
		kind = "parity"
		if u.log != nil {
			u.log.Info("event=strain-collection-press status=legacy_token_row interaction_kind_default=parity token_id=%d", tokenID)
		}
	}
	strain = strings.TrimSpace(strain)
	if strain == "" {
		_ = tx.Rollback()
		return msgs.CallbackExpired, true, nil
	}

	var totalBeforeAll int64
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*)::bigint FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2`,
		actorID, strain).Scan(&totalBeforeAll)
	if err != nil {
		_ = tx.Rollback()
		return msgs.CallbackExpired, true, nil
	}

	isAdditive := strings.TrimSpace(kind) == pressInteractionAdditive

	var removed bool
	var analyticsKind = kind

	if isAdditive {
		var bound int64
		err = tx.QueryRowContext(ctx,
			`SELECT COUNT(*)::bigint FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2 AND source_token_id = $3`,
			actorID, strain, tokenID).Scan(&bound)
		if err != nil {
			_ = tx.Rollback()
			return msgs.CallbackExpired, true, nil
		}

		if bound > 0 {
			_, err = tx.ExecContext(ctx,
				`DELETE FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2 AND source_token_id = $3`,
				actorID, strain, tokenID)
			if err != nil {
				_ = tx.Rollback()
				u.pressError(ctx, actorID, chatID, strain, err)
				return msgs.CallbackExpired, true, nil
			}
			removed = true
		} else {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO strain_collection_encounters (telegram_user_id, strain_canonical, source_token_id) VALUES ($1, $2, $3)`,
				actorID, strain, tokenID)
			if err != nil {
				_ = tx.Rollback()
				u.pressError(ctx, actorID, chatID, strain, err)
				return msgs.CallbackExpired, true, nil
			}
		}
	} else {
		var parityN int64
		err = tx.QueryRowContext(ctx,
			`SELECT COUNT(*)::bigint FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2 AND source_token_id IS NULL`,
			actorID, strain).Scan(&parityN)
		if err != nil {
			_ = tx.Rollback()
			return msgs.CallbackExpired, true, nil
		}
		removed = parityN%2 == 1
		if parityN%2 == 0 {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO strain_collection_encounters (telegram_user_id, strain_canonical, source_token_id) VALUES ($1, $2, NULL)`,
				actorID, strain)
		} else {
			_, err = tx.ExecContext(ctx, `
DELETE FROM strain_collection_encounters
WHERE id = (
  SELECT id FROM strain_collection_encounters
  WHERE telegram_user_id = $1 AND strain_canonical = $2 AND source_token_id IS NULL
  ORDER BY created_at DESC NULLS LAST, id DESC
  LIMIT 1
)`, actorID, strain)
		}
		if err != nil {
			_ = tx.Rollback()
			u.pressError(ctx, actorID, chatID, strain, err)
			return msgs.CallbackExpired, true, nil
		}
	}

	var total int64
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*)::bigint FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2`,
		actorID, strain).Scan(&total)
	if err != nil {
		_ = tx.Rollback()
		return msgs.CallbackExpired, true, nil
	}

	if err := tx.Commit(); err != nil {
		return msgs.CallbackExpired, true, nil
	}

	var followTxt string
	// DM only: parity first press reaches 1 encounter here; additive card used to wrongly check prior totals after parity had already incremented.
	if chatID > 0 && !removed && totalBeforeAll == 0 && total == 1 {
		if pat := strings.TrimSpace(msgs.EncounterAdditiveZeroToOneFollowUp); pat != "" {
			followTxt = pat
		}
	}

	ev := "strain-collection-confirm"
	if removed {
		ev = "strain-collection-remove"
	}
	u.trackOptional(ctx, ev, actorID, chatID, "ok", map[string]any{
		"strain":               strain,
		"encounter_cnt":        total,
		"removed":              removed,
		"interaction_kind_raw": analyticsKind,
		"additive":             isAdditive,
	})
	if u.log != nil {
		u.log.Info("event=strain-collection-press status=ok user_id=%d strain=%q encounter_cnt=%d removed=%v additive=%v token_id=%d",
			actorID, strain, total, removed, isAdditive, tokenID)
	}

	outcome := &handlemessage.StrainCollectConfirm{
		Canonical:      strain,
		ActorID:        actorID,
		ReplyChatID:    chatID,
		EncounterTotal: total,
		Removed:        removed,
		FollowUpNotice: followTxt,
	}
	return "", false, outcome
}

func (u *RootUseCase) pressError(ctx context.Context, actorID, chatID int64, strain string, err error) {
	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-collection-press-error",
			UserID: actorID,
			Status: "error",
			Meta: utils.MetaWithChatID(chatID, map[string]any{
				"strain": strain,
				"error":  err.Error(),
			}),
		})
	}
	if u.log != nil {
		u.log.Warn("strain-collection-press toggle err=%v user_id=%d strain=%q", err, actorID, strain)
	}
}

func (u *RootUseCase) trackOptional(ctx context.Context, name string, userID int64, chatID int64, status string, meta map[string]any) {
	if u.analytics == nil {
		return
	}
	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   name,
		UserID: userID,
		Status: status,
		Meta:   utils.MetaWithChatID(chatID, meta),
	})
}
