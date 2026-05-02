// Return telegram msg of formatted strain detaiils from injected nuglabs client

package handlemessage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	handlestrainpress "nuglabsbot-v2/use-cases/handle-strain-press"
	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const enabledSubscriptionsReadCacheTTL = 0
const broadcastByPayloadReadCacheTTL = 0
const strainEncounterReadCacheTTL = 0

type strainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
}

type HandleStrainUseCase struct {
	nuglabsClient any
	analytics     *utils.Analytics
	store         db.DB
	deferred      *db.DeferredWriteQueue
	logger        *utils.Logger
}

func NewHandleStrainUseCase(nuglabsClient any, analytics *utils.Analytics, store db.DB, deferred *db.DeferredWriteQueue, logger *utils.Logger) *HandleStrainUseCase {
	return &HandleStrainUseCase{nuglabsClient: nuglabsClient, analytics: analytics, store: store, deferred: deferred, logger: logger}
}

func (u *HandleStrainUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (utils.OutboundMessage, error) {
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return utils.OutboundMessage{Text: "Strain search is unavailable right now."}, nil
	}

	query := strings.TrimSpace(input)
	if query == "" {
		if chatID >= 0 {
			return utils.OutboundMessage{Text: "Please provide a strain name."}, nil
		}
		return utils.OutboundMessage{}, nil
	}

	strain, err := client.GetStrain(ctx, query)
	if err != nil {
		if u.logger != nil {
			u.logger.Error("strain get failed for %q: %v", query, err)
		}
		return utils.OutboundMessage{Text: "Strain search is temporarily unavailable."}, nil
	}
	if strain != nil {
		out, err := u.fullStrainOutbound(ctx, actorUserID, strain)
		if err != nil {
			return utils.OutboundMessage{}, err
		}
		if err := u.queueStrainSubscriptionBroadcasts(ctx, anyToString(strain["name"]), query); err != nil && u.logger != nil {
			u.logger.Error("enqueue subscription broadcasts failed: %v", err)
		}
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-found",
			UserID: actorUserID,
			Status: "ok",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query, "via": "exact"}),
		})
		return out, nil
	}

	hits, err := client.SearchStrains(ctx, query)
	if err != nil {
		if u.logger != nil {
			u.logger.Error("strain search failed for %q: %v", query, err)
		}
		return utils.OutboundMessage{Text: "Strain search is temporarily unavailable."}, nil
	}
	if len(hits) == 0 {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-not-found",
			UserID: actorUserID,
			Status: "miss",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query}),
		})
		return utils.OutboundMessage{Text: "No matching strain found."}, nil
	}

	if len(hits) == 1 {
		oneName := anyToString(hits[0]["name"])
		if oneName != "" {
			full, err := client.GetStrain(ctx, oneName)
			if err == nil && full != nil {
				out, err := u.fullStrainOutbound(ctx, actorUserID, full)
				if err != nil {
					return utils.OutboundMessage{}, err
				}
				if err := u.queueStrainSubscriptionBroadcasts(ctx, anyToString(full["name"]), query); err != nil && u.logger != nil {
					u.logger.Error("enqueue subscription broadcasts failed: %v", err)
				}
				_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
					Name:   "strain-found",
					UserID: actorUserID,
					Status: "ok",
					Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query, "via": "search", "resolved": oneName}),
				})
				return out, nil
			}
		}
	}

	if len(hits) > 1 {
		orig := len(hits)
		firstName := anyToString(hits[0]["name"])
		if firstName != "" {
			full, gerr := client.GetStrain(ctx, firstName)
			if gerr == nil && full != nil {
				if err := u.queueStrainSubscriptionBroadcasts(ctx, anyToString(full["name"]), query); err != nil && u.logger != nil {
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
	return utils.OutboundMessage{Text: formatTopMatchesHTML(hits)}, nil
}

// BuildSubscriptionStrainCard rebuilds a full card (per-recipient encounter counts + press button) for subscription broadcast delivery.
func (u *HandleStrainUseCase) BuildSubscriptionStrainCard(ctx context.Context, recipientTelegramID int64, strainCanonical string) (utils.OutboundMessage, error) {
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return utils.OutboundMessage{}, fmt.Errorf("strain client unavailable")
	}
	name := strings.TrimSpace(strainCanonical)
	if name == "" {
		return utils.OutboundMessage{}, fmt.Errorf("empty strain")
	}
	full, err := client.GetStrain(ctx, name)
	if err != nil || full == nil {
		return utils.OutboundMessage{}, err
	}
	return u.fullStrainOutbound(ctx, recipientTelegramID, full)
}

func (u *HandleStrainUseCase) fullStrainOutbound(ctx context.Context, viewerTelegramID int64, strain map[string]any) (utils.OutboundMessage, error) {
	canon := strings.TrimSpace(anyToString(strain["name"]))
	if canon == "" {
		return utils.OutboundMessage{Text: formatStrain(strain)}, nil
	}
	lineFmt := utils.StrainCollectionMessages().EncounterLine
	var n int64
	if u.store != nil && viewerTelegramID != 0 {
		var err error
		n, err = u.countStrainEncounters(ctx, viewerTelegramID, canon)
		if err != nil && u.logger != nil {
			u.logger.Warn("strain encounter count failed user_id=%d strain=%q: %v", viewerTelegramID, canon, err)
		}
	}
	html := FormatStrainHTMLWithEncounter(strain, n, lineFmt)
	if u.store == nil {
		return utils.OutboundMessage{Text: html}, nil
	}
	tokenID, err := u.insertStrainPressToken(ctx, canon)
	if err != nil {
		if u.logger != nil {
			u.logger.Warn("strain press token insert failed strain=%q: %v", canon, err)
		}
		return utils.OutboundMessage{Text: html}, nil
	}
	cb := handlestrainpress.CallbackDataPrefix + strconv.FormatInt(tokenID, 36)
	copy := utils.StrainCollectionMessages()
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(copy.PressIfFound, cb),
		),
	)
	return utils.OutboundMessage{Text: html, ReplyMarkup: &kb}, nil
}

func (u *HandleStrainUseCase) countStrainEncounters(ctx context.Context, telegramUserID int64, strainCanonical string) (int64, error) {
	var n int64
	err := u.store.QueryRowContext(
		ctx,
		`SELECT COUNT(*)::bigint FROM strain_collection_encounters WHERE telegram_user_id = $1 AND strain_canonical = $2`,
		strainEncounterReadCacheTTL,
		telegramUserID,
		strainCanonical,
	).Scan(&n)
	return n, err
}

func (u *HandleStrainUseCase) insertStrainPressToken(ctx context.Context, strainCanonical string) (int64, error) {
	var id int64
	err := u.store.QueryRowContext(
		ctx,
		`INSERT INTO strain_press_tokens (strain_canonical) VALUES ($1) RETURNING id`,
		strainEncounterReadCacheTTL,
		strainCanonical,
	).Scan(&id)
	return id, err
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
	return FormatStrainHTMLWithEncounter(strain, 0, "")
}

func formatEncounterCountTailHTML(count int64, encounterLineFmt string) string {
	if count <= 0 || strings.TrimSpace(encounterLineFmt) == "" {
		return ""
	}
	tail := strings.TrimSpace(fmt.Sprintf(encounterLineFmt, count))
	tail = strings.TrimPrefix(tail, "Encountered:")
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return ""
	}
	return "<b>Encountered:</b> " + html.EscapeString(tail)
}

// FormatStrainHTMLWithEncounter appends one per-user encounter line after Name/AKA (blank line before it) when encounterCount > 0.
func FormatStrainHTMLWithEncounter(strain map[string]any, encounterCount int64, encounterLineFmt string) string {
	esc := html.EscapeString
	if encounterLineFmt == "" {
		encounterLineFmt = utils.StrainCollectionMessages().EncounterLine
	}

	var blocks []string

	var idLines []string
	name := esc(anyToString(strain["name"]))
	akaPlain := joinListPlain(strain["akas"])
	var akaLine string
	if akaPlain != "" {
		akaLine = "<b>AKA:</b> " + esc(akaPlain)
	}

	if akaLine != "" {
		if name != "" {
			idLines = append(idLines, "<b>Name:</b> "+name)
		}
		idLines = append(idLines, akaLine)
	} else if name != "" {
		idLines = append(idLines, "<b>Name:</b> "+name)
	}
	if len(idLines) > 0 {
		identityBlock := strings.Join(idLines, "\n")
		if tail := formatEncounterCountTailHTML(encounterCount, encounterLineFmt); tail != "" {
			identityBlock += "\n\n" + tail
		}
		blocks = append(blocks, identityBlock)
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
	StrainCanonical string `json:"strain_canonical,omitempty"`
	StrainKey       string `json:"strain_key"`
	Text            string `json:"text,omitempty"`
}

func normalizeStrainKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func fanoutStrainKey(strainName, searchQuery string) string {
	if k := normalizeStrainKey(strainName); k != "" {
		return k
	}
	if k := normalizeStrainKey(searchQuery); k != "" {
		return k
	}
	return "unknown"
}

// queueStrainSubscriptionBroadcasts is the strain-search → broadcast pipeline:
// 1) Reuse or create a single broadcasts row per identical body (ensureBroadcastForPayload matches payload jsonb).
// 2) Upsert broadcast_outgoing for every enabled subscription; repeat searches re-queue (sent_time cleared on conflict).
// 3) bg-services/handle-broadcast sends rows with sent_time IS NULL.
//
// DB work runs on DeferredWriteQueue when configured so strain lookup can return before Supabase fan-out finishes.
func (u *HandleStrainUseCase) queueStrainSubscriptionBroadcasts(ctx context.Context, strainName, searchQuery string) error {
	if u.store == nil || strings.TrimSpace(strainName) == "" {
		return nil
	}
	if u.deferred != nil {
		sn, sq := strainName, searchQuery
		enqErr := u.deferred.Enqueue(func(c context.Context, conn db.DB) error {
			return u.queueStrainSubscriptionBroadcastsWithStore(c, conn, sn, sq)
		})
		if enqErr == nil {
			return nil
		}
		if u.logger != nil {
			u.logger.Warn("strain broadcast deferred queue full, running sync: %v", enqErr)
		}
	}
	return u.queueStrainSubscriptionBroadcastsWithStore(ctx, u.store, strainName, searchQuery)
}

func (u *HandleStrainUseCase) queueStrainSubscriptionBroadcastsWithStore(ctx context.Context, conn db.DB, strainName, searchQuery string) error {
	key := fanoutStrainKey(strainName, searchQuery)

	rows, err := conn.QueryContext(
		ctx,
		`SELECT s.telegram_id FROM subscriptions s
		 WHERE s.enabled = TRUE
		 ORDER BY s.telegram_id ASC`,
		enabledSubscriptionsReadCacheTTL,
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

		payloadRaw, err := json.Marshal(subscriptionMessagePayload{
			StrainCanonical: strings.TrimSpace(strainName),
			StrainKey:       key,
		})
		if err != nil {
			return err
		}
		payload := string(payloadRaw)
		broadcastID, err := u.ensureBroadcastForPayload(ctx, conn, now, payload, idx, telegramID)
		if err != nil {
			if u.logger != nil {
				u.logger.Warn("skip broadcast row for chat %d: ensure broadcast failed: %v", telegramID, err)
			}
			continue
		}
		res, err := conn.ExecContext(
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

func (u *HandleStrainUseCase) ensureBroadcastForPayload(ctx context.Context, conn db.DB, now time.Time, payload string, idx int64, telegramID int64) (string, error) {
	var existingID string
	err := conn.QueryRowContext(
		ctx,
		`SELECT id FROM broadcasts WHERE type = 'message' AND payload = $1::jsonb ORDER BY created_at ASC LIMIT 1`,
		broadcastByPayloadReadCacheTTL,
		payload,
	).Scan(&existingID)
	if err == nil && existingID != "" {
		return existingID, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	broadcastID := fmt.Sprintf("strain-%d-%d-%d", now.UnixNano(), telegramID, idx)
	if _, err := conn.ExecContext(
		ctx,
		`INSERT INTO broadcasts (id, type, payload, created_at) VALUES ($1, 'message', $2::jsonb, $3)`,
		broadcastID, payload, now,
	); err != nil {
		return "", err
	}
	return broadcastID, nil
}
