// Use injected in NugLabsClient to search input query against dataset
// Return top two results please

package handleinline

import (
	"context"
	"sort"
	"strings"
	"telegram-v2/utils"
)

type searchClient interface {
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
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
	client, ok := u.nuglabsClient.(searchClient)
	if !ok {
		return []map[string]any{}, nil
	}

	hits, err := client.SearchStrains(ctx, q)
	if err != nil {
		if u.analytics != nil && q != "" {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "inline-query",
				UserID: userID,
				Status: "error",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"query_len": len(q)}),
			})
		}
		return nil, err
	}
	if len(hits) <= 2 {
		if u.analytics != nil && q != "" {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "inline-query",
				UserID: userID,
				Status: "ok",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"results": len(hits)}),
			})
		}
		return hits, nil
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return scoreHit(hits[i]) > scoreHit(hits[j])
	})
	out := hits[:2]
	if u.analytics != nil && q != "" {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "inline-query",
			UserID: userID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"results": len(out)}),
		})
	}
	return out, nil
}

func scoreHit(hit map[string]any) int {
	name, _ := hit["name"].(string)
	if name == "" {
		return 0
	}
	return len(name)
}
