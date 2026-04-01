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

func (u *HandleInlineUseCase) Handle(ctx context.Context, query string) ([]map[string]any, error) {
	q := strings.TrimSpace(query)
	client, ok := u.nuglabsClient.(searchClient)
	if !ok {
		return []map[string]any{}, nil
	}

	hits, err := client.SearchStrains(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(hits) <= 2 {
		return hits, nil
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return scoreHit(hits[i]) > scoreHit(hits[j])
	})
	return hits[:2], nil
}

func scoreHit(hit map[string]any) int {
	name, _ := hit["name"].(string)
	if name == "" {
		return 0
	}
	return len(name)
}
