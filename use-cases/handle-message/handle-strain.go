// Return telegram msg of formatted strain detaiils from injected nuglabs client

package handlemessage

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"
	"telegram-v2/utils"
	"time"
)

type strainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
}

type HandleStrainUseCase struct {
	nuglabsClient any
	analytics     *utils.Analytics
	db            utils.DB
	logger        *utils.Logger
}

func NewHandleStrainUseCase(nuglabsClient any, analytics *utils.Analytics, db utils.DB, logger *utils.Logger) *HandleStrainUseCase {
	return &HandleStrainUseCase{nuglabsClient: nuglabsClient, analytics: analytics, db: db, logger: logger}
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
		msg := formatStrain(strain)
		if err := u.enqueueSubscriptionBroadcasts(ctx, msg); err != nil && u.logger != nil {
			u.logger.Error("enqueue subscription broadcasts failed: %v", err)
		}
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-found",
			Status: "ok",
			Meta:   map[string]any{"query": query},
		})
		return msg, nil
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
	return formatTopMatchesHTML(hits), nil
}

func formatStrain(strain map[string]any) string {
	name := anyToString(strain["name"])
	if name == "" {
		raw, _ := json.Marshal(strain)
		return html.EscapeString(fmt.Sprintf("Strain details: %s", string(raw)))
	}
	return FormatStrainHTML(strain)
}

// FormatStrainHTML returns Telegram HTML for a strain card (bold labels). Empty fields are omitted.
func FormatStrainHTML(strain map[string]any) string {
	esc := html.EscapeString
	var blocks []string

	var idLines []string
	if s := esc(anyToString(strain["name"])); s != "" {
		idLines = append(idLines, "<b>Name:</b> "+s)
	}
	if akas := joinListPlain(strain["akas"]); akas != "" {
		idLines = append(idLines, "<b>AKA:</b> "+esc(akas))
	}
	if len(idLines) > 0 {
		blocks = append(blocks, strings.Join(idLines, "\n"))
	}

	var typeLines []string
	if t := esc(anyToString(strain["type"])); t != "" {
		typeLines = append(typeLines, "<b>Type:</b> "+t)
	}
	if avg := formatAveragingHTML(strain); avg != "" {
		typeLines = append(typeLines, avg)
	}
	if len(typeLines) > 0 {
		blocks = append(blocks, strings.Join(typeLines, "\n"))
	}

	const listCap = 3

	var senseLines []string
	if f := joinListPlainMax(strain["flavours"], listCap); f != "" {
		senseLines = append(senseLines, "<b>Flavours:</b> "+esc(f))
	}
	if a := joinListPlainMax(strain["aromas"], listCap); a != "" {
		senseLines = append(senseLines, "<b>Aromas:</b> "+esc(a))
	}
	if len(senseLines) > 0 {
		blocks = append(blocks, strings.Join(senseLines, "\n"))
	}

	if terp := joinListPlainMax(strain["terpenes"], listCap); terp != "" {
		blocks = append(blocks, "<b>Terpenes:</b> "+esc(terp))
	}

	var effectLines []string
	if e := joinListPlainMax(strain["effects"], listCap); e != "" {
		effectLines = append(effectLines, "<b>Effects:</b> "+esc(e))
	}
	if h := joinListPlainMax(strain["helps_with"], listCap); h != "" {
		effectLines = append(effectLines, "<b>Helps with:</b> "+esc(h))
	}
	if len(effectLines) > 0 {
		blocks = append(blocks, strings.Join(effectLines, "\n"))
	}

	if desc := pickDescription(strain); desc != "" {
		blocks = append(blocks, "<b>Description:</b> "+esc(desc))
	}

	out := strings.Join(blocks, "\n\n")
	if link := StrainDeeplink(anyToString(strain["name"])); link != "" {
		linkLine := link
		if !strings.HasPrefix(link, "https://") {
			linkLine = esc(link)
		}
		if out != "" {
			out += "\n\n" + linkLine
		} else {
			out = linkLine
		}
	}
	return strings.TrimSpace(out)
}

func formatTopMatchesHTML(hits []map[string]any) string {
	var parts []string
	parts = append(parts, "<b>Maybe you mean</b>")
	for _, hit := range hits {
		name := anyToString(hit["name"])
		if name == "" {
			continue
		}
		parts = append(parts, "")
		parts = append(parts, html.EscapeString(name))
		parts = append(parts, StrainDeeplink(name))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func formatAveragingHTML(strain map[string]any) string {
	v, ok := strain["thc"]
	if !ok || v == nil {
		return ""
	}
	pct := formatTHCPercent(v)
	if pct == "" {
		return ""
	}
	return "<b>Averaging:</b> THC " + pct
}

func formatTHCPercent(v any) string {
	switch t := v.(type) {
	case float64:
		s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", t), "0"), ".")
		if s == "" {
			s = "0"
		}
		return s + "%"
	case float32:
		return formatTHCPercent(float64(t))
	case int:
		return fmt.Sprintf("%d%%", t)
	case int64:
		return fmt.Sprintf("%d%%", t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return strings.TrimSpace(t.String()) + "%"
		}
		return formatTHCPercent(f)
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", t))
		if s == "" {
			return ""
		}
		if strings.HasSuffix(s, "%") {
			return s
		}
		return s + "%"
	}
}

func pickDescription(strain map[string]any) string {
	for _, key := range []string{"description_sm", "description_md", "description_lg"} {
		if s := strings.TrimSpace(anyToString(strain[key])); s != "" {
			return s
		}
	}
	return ""
}

func collectListParts(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return splitCommaList(t)
	case []any:
		var parts []string
		for _, x := range t {
			if s := strings.TrimSpace(anyToString(x)); s != "" {
				parts = append(parts, s)
			}
		}
		return parts
	case []string:
		var parts []string
		for _, s := range t {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		return parts
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", t))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

func splitCommaList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinListPlain(v any) string {
	parts := collectListParts(v)
	return strings.Join(parts, ", ")
}

func joinListPlainMax(v any, max int) string {
	if max <= 0 {
		return ""
	}
	parts := collectListParts(v)
	if len(parts) > max {
		parts = parts[:max]
	}
	return strings.Join(parts, ", ")
}

// StrainDeeplink builds the bot deep link for opening this strain via /start.
func StrainDeeplink(strainName string) string {
	username := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME"))
	if username == "" {
		return strainName
	}
	payload := url.QueryEscape(strainName)
	return "https://t.me/" + username + "?start=" + payload
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

func (u *HandleStrainUseCase) enqueueSubscriptionBroadcasts(ctx context.Context, message string) error {
	if u.db == nil || strings.TrimSpace(message) == "" {
		return nil
	}

	rows, err := u.db.QueryContext(
		ctx,
		`SELECT telegram_id FROM subscriptions WHERE enabled = TRUE ORDER BY telegram_id ASC`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now().UTC()
	var idx int64
	var queued int
	for rows.Next() {
		var telegramID int64
		if err := rows.Scan(&telegramID); err != nil {
			return err
		}
		idx++

		broadcastID := fmt.Sprintf("strain-%d-%d-%d", now.UnixNano(), telegramID, idx)
		payloadRaw, err := json.Marshal(map[string]any{"text": message})
		if err != nil {
			return err
		}
		if _, err := u.db.ExecContext(
			ctx,
			`INSERT INTO broadcasts (id, type, payload, created_at) VALUES ($1, 'message', $2::jsonb, $3)`,
			broadcastID, string(payloadRaw), now,
		); err != nil {
			if u.logger != nil {
				u.logger.Warn("skip broadcast row for chat %d: insert broadcasts failed: %v", telegramID, err)
			}
			continue
		}
		if _, err := u.db.ExecContext(
			ctx,
			`INSERT INTO broadcast_outgoing (broadcast_id, user_id, scheduled_at) VALUES ($1, $2, $3)`,
			broadcastID, telegramID, now,
		); err != nil {
			if u.logger != nil {
				u.logger.Warn("skip outgoing row for chat %d: insert broadcast_outgoing failed: %v", telegramID, err)
			}
			continue
		}
		queued++
	}

	if err := rows.Err(); err != nil {
		return err
	}
	if u.logger != nil {
		u.logger.Info("queued strain broadcast for %d enabled subscriptions", queued)
	}
	return nil
}
