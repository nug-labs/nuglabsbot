// Return telegram msg of formatted strain detaiils from injected nuglabs client

package handlemessage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"sort"
	"strings"
	"telegram-v2/utils"
	"telegram-v2/utils/db"
	"time"
)

type strainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
}

type HandleStrainUseCase struct {
	nuglabsClient any
	analytics     *utils.Analytics
	store         db.DB
	logger        *utils.Logger
}

func NewHandleStrainUseCase(nuglabsClient any, analytics *utils.Analytics, store db.DB, logger *utils.Logger) *HandleStrainUseCase {
	return &HandleStrainUseCase{nuglabsClient: nuglabsClient, analytics: analytics, store: store, logger: logger}
}

func (u *HandleStrainUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (string, error) {
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return "Strain search is unavailable right now.", nil
	}

	query := strings.TrimSpace(input)
	if query == "" {
		if chatID >= 0 {
			return "Please provide a strain name.", nil
		}
		return "", nil
	}

	strain, err := client.GetStrain(ctx, query)
	if err != nil {
		if u.logger != nil {
			u.logger.Error("strain get failed for %q: %v", query, err)
		}
		return "Strain search is temporarily unavailable.", nil
	}
	if strain != nil {
		msg := formatStrain(strain)
		if err := u.queueStrainSubscriptionBroadcasts(ctx, msg, anyToString(strain["name"]), query); err != nil && u.logger != nil {
			u.logger.Error("enqueue subscription broadcasts failed: %v", err)
		}
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-found",
			UserID: actorUserID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query, "via": "exact"}),
		})
		return msg, nil
	}

	hits, err := client.SearchStrains(ctx, query)
	if err != nil {
		if u.logger != nil {
			u.logger.Error("strain search failed for %q: %v", query, err)
		}
		return "Strain search is temporarily unavailable.", nil
	}
	if len(hits) == 0 {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-not-found",
			UserID: actorUserID,
			Status: "miss",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query}),
		})
		return "No matching strain found.", nil
	}

	// Single search hit: same path as exact match (full card + subscription broadcast queue).
	if len(hits) == 1 {
		oneName := anyToString(hits[0]["name"])
		if oneName != "" {
			full, err := client.GetStrain(ctx, oneName)
			if err == nil && full != nil {
				msg := formatStrain(full)
				if err := u.queueStrainSubscriptionBroadcasts(ctx, msg, anyToString(full["name"]), query); err != nil && u.logger != nil {
					u.logger.Error("enqueue subscription broadcasts failed: %v", err)
				}
				_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
					Name:   "strain-found",
					UserID: actorUserID,
					Status: "ok",
					Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query, "via": "search", "resolved": oneName}),
				})
				return msg, nil
			}
		}
	}

	if len(hits) > 1 {
		orig := len(hits)
		// Fan-out uses the same top match as the card list: subscribers get the resolved first hit.
		firstName := anyToString(hits[0]["name"])
		if firstName != "" {
			full, gerr := client.GetStrain(ctx, firstName)
			if gerr == nil && full != nil {
				msg := formatStrain(full)
				if err := u.queueStrainSubscriptionBroadcasts(ctx, msg, anyToString(full["name"]), query); err != nil && u.logger != nil {
					u.logger.Error("enqueue subscription broadcasts failed: %v", err)
				}
				_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
					Name:   "strain-found",
					UserID: actorUserID,
					Status: "ok",
					Meta: utils.MetaWithChatID(chatID, map[string]any{
						"query": query, "via": "search-multi", "resolved": firstName, "match_count": orig,
					}),
				})
			}
		}
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
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", t), "0"), ".") + "%"
	case float32:
		return formatTHCPercent(float64(t))
	case int, int64, json.Number:
		s := strings.TrimSpace(fmt.Sprintf("%v", t))
		if s == "" {
			return ""
		}
		if strings.HasSuffix(s, "%") {
			return s
		}
		return s + "%"
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
	if s := strings.TrimSpace(anyToString(strain["description_sm"])); s != "" {
		return s
	}
	if s := strings.TrimSpace(anyToString(strain["description_md"])); s != "" {
		return s
	}
	if s := strings.TrimSpace(anyToString(strain["description_lg"])); s != "" {
		return s
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
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func joinListPlainMax(v any, max int) string {
	if max <= 0 {
		return ""
	}
	parts := collectListParts(v)
	sort.Strings(parts)
	if len(parts) > max {
		parts = parts[:max]
	}
	return strings.Join(parts, ", ")
}

// StrainDeeplink builds https://t.me/<bot>?start=<payload>; TELEGRAM_BOT_USERNAME must match BotFather @name (no @ prefix in env).
func StrainDeeplink(strainName string) string {
	username := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME"))
	if username == "" {
		return strainName
	}
	payload := url.QueryEscape(strings.Join(strings.Fields(strainName), "-"))
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

// subscriptionMessagePayload must use a struct (not map) so json.Marshal order is stable; map iteration
// breaks broadcasts.payload equality dedupe and causes a new broadcast row on every lookup on live workers.
type subscriptionMessagePayload struct {
	Text      string `json:"text"`
	StrainKey string `json:"strain_key"`
}

func normalizeStrainKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// fanoutStrainKey must be stable for the same strain across lookups: prefer resolved name, then user query,
// then content hash (last resort only — HTML can drift).
func fanoutStrainKey(strainName, searchQuery, message string) string {
	if k := normalizeStrainKey(strainName); k != "" {
		return k
	}
	if k := normalizeStrainKey(searchQuery); k != "" {
		return k
	}
	sum := sha256.Sum256([]byte(message))
	return fmt.Sprintf("h%x", sum[:8])
}

// queueStrainSubscriptionBroadcasts is the strain-search → broadcast pipeline:
// 1) Reuse or create a single broadcasts row per identical body (ensureBroadcastForPayload matches payload jsonb).
// 2) Upsert broadcast_outgoing for every enabled subscription; repeat searches re-queue (sent_time cleared on conflict).
// 3) bg-services/handle-broadcast sends rows with sent_time IS NULL.
func (u *HandleStrainUseCase) queueStrainSubscriptionBroadcasts(ctx context.Context, message, strainName, searchQuery string) error {
	if u.store == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	key := fanoutStrainKey(strainName, searchQuery, message)

	rows, err := u.store.QueryContext(
		ctx,
		`SELECT s.telegram_id FROM subscriptions s
		 WHERE s.enabled = TRUE
		 ORDER BY s.telegram_id ASC`,
		0,
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

		payloadRaw, err := json.Marshal(subscriptionMessagePayload{Text: message, StrainKey: key})
		if err != nil {
			return err
		}
		payload := string(payloadRaw)
		broadcastID, err := u.ensureBroadcastForPayload(ctx, now, payload, idx, telegramID)
		if err != nil {
			if u.logger != nil {
				u.logger.Warn("skip broadcast row for chat %d: ensure broadcast failed: %v", telegramID, err)
			}
			continue
		}
		res, err := u.store.ExecContext(
			ctx,
			`INSERT INTO broadcast_outgoing (broadcast_id, user_id, scheduled_at, sent_time)
			 VALUES ($1, $2, $3, NULL)
			 ON CONFLICT (broadcast_id, user_id) DO UPDATE SET
			   scheduled_at = EXCLUDED.scheduled_at,
			   sent_time = NULL`,
			broadcastID, telegramID, now,
		)
		if err != nil {
			if u.logger != nil {
				u.logger.Warn("skip outgoing row for chat %d: insert broadcast_outgoing failed: %v", telegramID, err)
			}
			continue
		}
		if n, raErr := res.RowsAffected(); raErr == nil && n > 0 {
			queued++
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	if u.logger != nil {
		u.logger.Info("queued strain broadcast for %d enabled subscriptions", queued)
	}
	return nil
}

func (u *HandleStrainUseCase) ensureBroadcastForPayload(ctx context.Context, now time.Time, payload string, idx int64, telegramID int64) (string, error) {
	var existingID string
	err := u.store.QueryRowContext(
		ctx,
		`SELECT id FROM broadcasts WHERE type = 'message' AND payload = $1::jsonb ORDER BY created_at ASC LIMIT 1`,
		0,
		payload,
	).Scan(&existingID)
	if err == nil && existingID != "" {
		return existingID, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	broadcastID := fmt.Sprintf("strain-%d-%d-%d", now.UnixNano(), telegramID, idx)
	if _, err := u.store.ExecContext(
		ctx,
		`INSERT INTO broadcasts (id, type, payload, created_at) VALUES ($1, 'message', $2::jsonb, $3)`,
		broadcastID, payload, now,
	); err != nil {
		return "", err
	}
	return broadcastID, nil
}
