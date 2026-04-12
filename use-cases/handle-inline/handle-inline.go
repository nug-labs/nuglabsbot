// Use injected in NugLabsClient to search input query against dataset
// Return top two results please

package handleinline

import (
	"context"
	"strings"
	"nuglabsbot-v2/utils"
)

type getClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
}

type HandleInlineUseCase struct {
	nuglabsClient any
	analytics     *utils.Analytics
}

func NewHandleInlineUseCase(nuglabsClient any, analytics *utils.Analytics) *HandleInlineUseCase {
	return &HandleInlineUseCase{nuglabsClient: nuglabsClient, analytics: analytics}
}

// chatID is the Telegram chat id when known; inline updates often only have the user (private chat id equals user id).
func (u *HandleInlineUseCase) Handle(ctx context.Context, userID, chatID int64, query string) ([]map[string]any, error) {
	q := strings.TrimSpace(query)
	client, ok := u.nuglabsClient.(getClient)
	if !ok {
		return []map[string]any{}, nil
	}

	if q == "" {
		return []map[string]any{}, nil
	}

	strain, err := client.GetStrain(ctx, q)
	if err != nil {
		if u.analytics != nil && q != "" {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "inline-query",
				UserID: userID,
				Status: "error",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"query_len": len(q), "via": "get"}),
			})
		}
		return nil, err
	}

	// Intentionally no fuzzy search here — inline is "exact get only" to avoid janky suggestions.
	if strain == nil {
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "inline-query",
				UserID: userID,
				Status: "miss",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"query_len": len(q), "via": "get"}),
			})
		}
		return []map[string]any{}, nil
	}

	out := []map[string]any{strain}
	if u.analytics != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "inline-query",
			UserID: userID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"results": len(out), "via": "get"}),
		})
	}
	return out, nil
}
