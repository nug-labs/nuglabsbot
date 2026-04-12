// convert-analytics-users backfills users from:
//   - app_analytics (user_id, meta.chat_id when positive)
//   - analytics_events (from_id, chat_id when positive; telegram.fromId / telegram.chatId)
//
// Group/supergroup chat_ids are negative in Telegram and are skipped. Private chat_id == user id.
//
// Run from app/nuglabsbot-v2: go run ./zz-ops/helpers/convert-analytics-users.go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const analyticsBackfillReadCacheTTL = 0

func main() {
	utils.Env.InitOps()

	database, err := db.DatabaseManager.Init(context.Background())
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT telegram_id
		FROM (
			-- nuglabsbot-v2 app_analytics
			SELECT user_id AS telegram_id
			FROM app_analytics
			WHERE user_id IS NOT NULL AND user_id > 0

			UNION

			SELECT (meta->'chat_id')::text::bigint AS telegram_id
			FROM app_analytics
			WHERE meta ? 'chat_id'
			  AND jsonb_typeof(meta->'chat_id') = 'number'
			  AND (meta->'chat_id')::text::bigint > 0

			UNION

			SELECT (meta->>'chat_id')::bigint AS telegram_id
			FROM app_analytics
			WHERE meta ? 'chat_id'
			  AND jsonb_typeof(meta->'chat_id') = 'string'
			  AND (meta->>'chat_id') ~ '^[0-9]+$'
			  AND (meta->>'chat_id')::bigint > 0

			UNION

			-- shared analytics_events (see app/telegram bootstrapAnalytics.ts)
			SELECT from_id AS telegram_id
			FROM analytics_events
			WHERE from_id IS NOT NULL AND from_id > 0

			UNION

			SELECT chat_id AS telegram_id
			FROM analytics_events
			WHERE chat_id IS NOT NULL AND chat_id > 0

			UNION

			SELECT (NULLIF(telegram->>'fromId', ''))::bigint AS telegram_id
			FROM analytics_events
			WHERE telegram IS NOT NULL
			  AND (telegram->>'fromId') ~ '^[0-9]+$'
			  AND (NULLIF(telegram->>'fromId', ''))::bigint > 0

			UNION

			SELECT (NULLIF(telegram->>'chatId', ''))::bigint AS telegram_id
			FROM analytics_events
			WHERE telegram IS NOT NULL
			  AND (telegram->>'chatId') ~ '^[0-9]+$'
			  AND (NULLIF(telegram->>'chatId', ''))::bigint > 0

			UNION

			SELECT (NULLIF(props->>'from_id', ''))::bigint AS telegram_id
			FROM analytics_events
			WHERE props IS NOT NULL
			  AND (props->>'from_id') ~ '^[0-9]+$'
			  AND (NULLIF(props->>'from_id', ''))::bigint > 0

			UNION

			SELECT (NULLIF(props->>'fromId', ''))::bigint AS telegram_id
			FROM analytics_events
			WHERE props IS NOT NULL
			  AND (props->>'fromId') ~ '^[0-9]+$'
			  AND (NULLIF(props->>'fromId', ''))::bigint > 0

			UNION

			SELECT (NULLIF(props->>'userId', ''))::bigint AS telegram_id
			FROM analytics_events
			WHERE props IS NOT NULL
			  AND (props->>'userId') ~ '^[0-9]+$'
			  AND (NULLIF(props->>'userId', ''))::bigint > 0
		) AS ids
		WHERE telegram_id IS NOT NULL
		ORDER BY telegram_id
	`, analyticsBackfillReadCacheTTL)
	if err != nil {
		panic(fmt.Errorf("query analytics ids: %w", err))
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			panic(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}

	inserted := 0
	for _, telegramID := range ids {
		res, err := database.ExecContext(ctx,
			`INSERT INTO users (telegram_id, username, first_name, last_name, total_requests, last_seen_at)
			 VALUES ($1, NULL, NULL, NULL, 0, NOW())
			 ON CONFLICT (telegram_id) DO NOTHING`,
			telegramID,
		)
		if err != nil {
			panic(fmt.Errorf("insert user %d: %w", telegramID, err))
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}

	fmt.Fprintf(os.Stderr, "distinct telegram ids (app_analytics + analytics_events): %d\n", len(ids))
	fmt.Fprintf(os.Stderr, "users rows inserted (new): %d\n", inserted)
}
