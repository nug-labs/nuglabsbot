// Return telegram msg of formatted strain detaiils from injected nuglabs client

package handlemessage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"telegram-v2/utils"
)

type strainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
}

type HandleStrainUseCase struct {
	nuglabsClient any
	analytics     *utils.Analytics
}

func NewHandleStrainUseCase(nuglabsClient any, analytics *utils.Analytics) *HandleStrainUseCase {
	return &HandleStrainUseCase{nuglabsClient: nuglabsClient, analytics: analytics}
}

func (u *HandleStrainUseCase) Handle(ctx context.Context, input string) (string, error) {
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return "Strain search is unavailable right now.", nil
	}

	query := strings.TrimSpace(input)
	if query == "" {
		return "Please provide a strain name.", nil
	}

	strain, err := client.GetStrain(ctx, query)
	if err != nil {
		return "", err
	}
	if strain != nil {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-found",
			Status: "ok",
			Meta:   map[string]any{"query": query},
		})
		return formatStrain(strain), nil
	}

	hits, err := client.SearchStrains(ctx, query)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-not-found",
			Status: "miss",
			Meta:   map[string]any{"query": query},
		})
		return "No matching strain found.", nil
	}

	if len(hits) > 2 {
		hits = hits[:2]
	}
	out := "Top matches:\n"
	for _, hit := range hits {
		out += "- " + anyToString(hit["name"]) + "\n"
	}
	return strings.TrimSpace(out), nil
}

func formatStrain(strain map[string]any) string {
	name := anyToString(strain["name"])
	if name == "" {
		raw, _ := json.Marshal(strain)
		return fmt.Sprintf("Strain details: %s", string(raw))
	}
	out := fmt.Sprintf("%s\n", name)
	if t := anyToString(strain["type"]); t != "" {
		out += fmt.Sprintf("Type: %s\n", t)
	}
	if thc := anyToString(strain["thc"]); thc != "" {
		out += fmt.Sprintf("THC: %s\n", thc)
	}
	return strings.TrimSpace(out)
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		if t == nil {
			return ""
		}
		return fmt.Sprintf("%v", t)
	}
}
