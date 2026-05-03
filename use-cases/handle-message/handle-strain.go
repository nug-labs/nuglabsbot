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
	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const enabledSubscriptionsReadCacheTTL = 0
const broadcastByPayloadReadCacheTTL = 0
const strainEncounterReadCacheTTL = 0

// strain_press_tokens.interaction_kind (minted alongside inline keyboard).
const (
	pressInteractionParity   = "parity"
	pressInteractionAdditive = "additive"
)

// Plain DM text search uses additive mints so each new card is "+1 / undo this card only" and does not toggle
// global parity (which would remove a null-source row on an "add" tap). /start deep links use parity — global +/−.

// strainCardAudience controls collection UI on strain cards:
// Private DM (chat ID > 0) → encounter tally + Press-if-found; groups/subscribers → plain strain only.
type strainCardAudience int

const (
	strainCardPrivateDM strainCardAudience = iota
	strainCardGroupOrFanoutPlain
)

// deliveryAudience returns PrivateDM for user chats only; negatives are groups/channels/etc.
func deliveryAudience(chatID int64) strainCardAudience {
	if chatID < 0 {
		return strainCardGroupOrFanoutPlain
	}
	return strainCardPrivateDM
}

type strainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
	SearchStrains(ctx context.Context, query string) ([]map[string]any, error)
}

type HandleStrainUseCase struct {
	nuglabsClient       any
	analytics           *utils.Analytics
	store               db.DB
	deferred            *db.DeferredWriteQueue
	logger              *utils.Logger
	subscriptionPlainTx SubscriptionPlainSender // optional; collection adds fan out plain strain immediately when set
}

func NewHandleStrainUseCase(nuglabsClient any, analytics *utils.Analytics, store db.DB, deferred *db.DeferredWriteQueue, logger *utils.Logger, subscriptionPlainTx SubscriptionPlainSender) *HandleStrainUseCase {
	return &HandleStrainUseCase{nuglabsClient: nuglabsClient, analytics: analytics, store: store, deferred: deferred, logger: logger, subscriptionPlainTx: subscriptionPlainTx}
}

// Handle serves plain chat messages (organic strain text). Private cards mint additive press tokens.
func (u *HandleStrainUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (OutboundMessage, error) {
	return u.handleStrainLookup(ctx, actorUserID, chatID, input, pressInteractionAdditive)
}

// HandleDeeplink serves /start <strain> (t.me deep links). Private cards mint parity tokens for global +/− on null-source rows.
func (u *HandleStrainUseCase) HandleDeeplink(ctx context.Context, actorUserID, chatID int64, input string) (OutboundMessage, error) {
	return u.handleStrainLookup(ctx, actorUserID, chatID, input, pressInteractionParity)
}

func (u *HandleStrainUseCase) handleStrainLookup(ctx context.Context, actorUserID, chatID int64, input string, privatePressMintKind string) (OutboundMessage, error) {
	sc := StrainCollectionMessages()
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return OutboundMessage{Text: sc.StrainSearchDisabled}, nil
	}

	query := strings.TrimSpace(input)
	if query == "" {
		if chatID >= 0 {
			return OutboundMessage{Text: sc.StrainPleaseProvideName}, nil
		}
		return OutboundMessage{}, nil
	}

	strain, err := client.GetStrain(ctx, query)
	if err != nil {
		if u.logger != nil {
			u.logger.Error("strain get failed for %q: %v", query, err)
		}
		return OutboundMessage{Text: sc.StrainSearchTemporarilyUnavailable}, nil
	}
	if strain != nil {
		out, err := u.fullStrainOutbound(ctx, actorUserID, strain, deliveryAudience(chatID), privatePressMintKind)
		if err != nil {
			return OutboundMessage{}, err
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
		return OutboundMessage{Text: sc.StrainSearchTemporarilyUnavailable}, nil
	}
	if len(hits) == 0 {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "strain-not-found",
			UserID: actorUserID,
			Status: "miss",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"query": query}),
		})
		return OutboundMessage{Text: sc.StrainNoMatching}, nil
	}

	if len(hits) == 1 {
		oneName := anyToString(hits[0]["name"])
		if oneName != "" {
			full, err := client.GetStrain(ctx, oneName)
			if err == nil && full != nil {
				out, err := u.fullStrainOutbound(ctx, actorUserID, full, deliveryAudience(chatID), privatePressMintKind)
				if err != nil {
					return OutboundMessage{}, err
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
		topTwo := hits
		if len(topTwo) > 2 {
			topTwo = topTwo[:2]
		}
		firstName := anyToString(topTwo[0]["name"])
		var fullFirst map[string]any
		if firstName != "" {
			var gerr error
			fullFirst, gerr = client.GetStrain(ctx, firstName)
			if gerr == nil && fullFirst != nil {
				if err := u.queueStrainSubscriptionBroadcasts(ctx, anyToString(fullFirst["name"]), query); err != nil && u.logger != nil {
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

		if deliveryAudience(chatID) == strainCardPrivateDM && fullFirst != nil {
			out, err := u.fullStrainOutbound(ctx, actorUserID, fullFirst, strainCardPrivateDM, privatePressMintKind)
			if err != nil {
				return OutboundMessage{}, err
			}
			if sec := formatSecondaryMatchesHTML(topTwo[1:]); sec != "" {
				out.Text = strings.TrimSpace(out.Text + "\n\n" + sec)
			}
			return out, nil
		}
		return OutboundMessage{Text: formatTopMatchesHTML(topTwo)}, nil
	}
	return OutboundMessage{Text: formatTopMatchesHTML(hits)}, nil
}

// fanOutPlainStrainAfterCollection sends plain strain HTML to every enabled subscriber immediately (no Encounter line, no keyboard).
func (u *HandleStrainUseCase) fanOutPlainStrainAfterCollection(ctx context.Context, strainCanonical string) error {
	if u.subscriptionPlainTx == nil || u.store == nil {
		return nil
	}
	canon := strings.TrimSpace(strainCanonical)
	if canon == "" {
		return nil
	}

	rows, err := u.store.QueryContext(ctx,
		`SELECT s.telegram_id FROM subscriptions s WHERE s.enabled = TRUE ORDER BY s.telegram_id ASC`,
		enabledSubscriptionsReadCacheTTL,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ok, failed int
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return err
		}
		out, berr := u.BuildSubscriptionStrainCard(ctx, chatID, canon)
		if berr != nil {
			if u.logger != nil {
				u.logger.Warn("strain subscription plain fan-out: build failed chat_id=%d strain=%q: %v", chatID, canon, berr)
			}
			failed++
			continue
		}
		if strings.TrimSpace(out.Text) == "" {
			failed++
			continue
		}
		if _, serr := u.subscriptionPlainTx.SendOutbound(chatID, out); serr != nil {
			if u.logger != nil {
				u.logger.Warn("strain subscription plain fan-out: send failed chat_id=%d strain=%q: %v", chatID, canon, serr)
			}
			failed++
			continue
		}
		ok++
		if u.analytics != nil {
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "strain-subscription-plain",
				UserID: chatID,
				Status: "ok",
				Meta:   map[string]any{"strain": canon, "via": "collection-add"},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if u.logger != nil {
		u.logger.Info("strain subscription plain fan-out strain=%q sent_ok=%d send_failed=%d", canon, ok, failed)
	}
	return nil
}

// BuildSubscriptionStrainCard rebuilds a strain card for subscription broadcast delivery — strain body only (no Encountered line, no button).
func (u *HandleStrainUseCase) BuildSubscriptionStrainCard(ctx context.Context, recipientTelegramID int64, strainCanonical string) (OutboundMessage, error) {
	client, ok := u.nuglabsClient.(strainClient)
	if !ok {
		return OutboundMessage{}, fmt.Errorf("strain client unavailable")
	}
	name := strings.TrimSpace(strainCanonical)
	if name == "" {
		return OutboundMessage{}, fmt.Errorf("empty strain")
	}
	full, err := client.GetStrain(ctx, name)
	if err != nil || full == nil {
		return OutboundMessage{}, err
	}
	return u.fullStrainOutbound(ctx, recipientTelegramID, full, strainCardGroupOrFanoutPlain, "")
}

func (u *HandleStrainUseCase) fullStrainOutbound(ctx context.Context, viewerTelegramID int64, strain map[string]any, aud strainCardAudience, pressKind string) (OutboundMessage, error) {
	canon := strings.TrimSpace(anyToString(strain["name"]))
	if canon == "" {
		return OutboundMessage{Text: formatStrain(strain)}, nil
	}
	priv := aud == strainCardPrivateDM

	lineFmt := StrainCollectionMessages().EncounterLine
	var n int64
	if priv && u.store != nil && viewerTelegramID != 0 {
		var err error
		n, err = u.countStrainEncounters(ctx, viewerTelegramID, canon)
		if err != nil && u.logger != nil {
			u.logger.Warn("strain encounter count failed user_id=%d strain=%q: %v", viewerTelegramID, canon, err)
		}
	}
	html := FormatStrainHTMLWithEncounter(strain, n, lineFmt)
	if !priv || u.store == nil {
		return OutboundMessage{Text: html}, nil
	}

	mint := strings.TrimSpace(pressKind)
	if mint != pressInteractionParity && mint != pressInteractionAdditive {
		mint = pressInteractionParity
	}
	tokenID, err := u.insertStrainPressToken(ctx, canon, mint)
	if err != nil {
		if u.logger != nil {
			u.logger.Warn("strain press token insert failed strain=%q: %v", canon, err)
		}
		return OutboundMessage{Text: html}, nil
	}
	cb := StrainCollectionCallbackPrefix + strconv.FormatInt(tokenID, 36)
	sc := StrainCollectionMessages()
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(sc.PressIfFound, cb),
		),
	)
	return OutboundMessage{Text: html, ReplyMarkup: &kb}, nil
}

// NotifyAfterStrainCollected posts an updated strain card to the finder after a collection button press.
// Subscribers (when Removed is false): plain strain HTML (no Encountered line, no keyboard) via
// fanOutPlainStrainAfterCollection when SubscriptionPlainSender is wired; otherwise queued for handle-broadcast.
// Removed=true skips subscriber fan-out.
func (u *HandleStrainUseCase) NotifyAfterStrainCollected(ctx context.Context, post func(chatID int64, msg OutboundMessage) error, ok *StrainCollectConfirm) error {
	if ok == nil || post == nil || u.store == nil {
		return nil
	}
	client, typed := u.nuglabsClient.(strainClient)
	if !typed {
		return nil
	}
	canon := strings.TrimSpace(ok.Canonical)
	if canon == "" {
		return nil
	}

	if !ok.Removed {
		if u.subscriptionPlainTx != nil {
			if err := u.fanOutPlainStrainAfterCollection(ctx, canon); err != nil && u.logger != nil {
				u.logger.Error("strain collected notify: plain subscription fan-out failed: %v", err)
			}
		} else {
			if err := u.queueStrainSubscriptionBroadcasts(ctx, canon, "collection-confirmed"); err != nil && u.logger != nil {
				u.logger.Error("strain collected notify: subscription enqueue failed: %v", err)
			}
		}
	}

	strain, err := client.GetStrain(ctx, canon)
	if err != nil || strain == nil {
		if u.logger != nil {
			u.logger.Warn("strain collected notify: get strain failed strain=%q: %v", canon, err)
		}
		return nil
	}
	out, ferr := u.fullStrainOutbound(ctx, ok.ActorID, strain, deliveryAudience(ok.ReplyChatID), pressInteractionAdditive)
	if ferr != nil {
		return ferr
	}
	if err := post(ok.ReplyChatID, out); err != nil && u.logger != nil {
		u.logger.Warn("strain collected notify: send finder updated card failed chat_id=%d err=%v", ok.ReplyChatID, err)
	}
	if t := strings.TrimSpace(ok.FollowUpNotice); t != "" {
		if err := post(ok.ReplyChatID, OutboundMessage{Text: t}); err != nil && u.logger != nil {
			u.logger.Warn("strain collected notify: follow-up message failed chat_id=%d err=%v", ok.ReplyChatID, err)
		}
	}
	return nil
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

func (u *HandleStrainUseCase) insertStrainPressToken(ctx context.Context, strainCanonical, interactionKind string) (int64, error) {
	k := strings.TrimSpace(interactionKind)
	if k != pressInteractionParity && k != pressInteractionAdditive {
		k = pressInteractionParity
	}

	var id int64
	err := u.store.QueryRowContext(
		ctx,
		`INSERT INTO strain_press_tokens (strain_canonical, interaction_kind) VALUES ($1, $2) RETURNING id`,
		strainEncounterReadCacheTTL,
		strainCanonical,
		k,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	if k != pressInteractionParity || !db.IsUndefinedColumn(err) {
		return 0, err
	}

	// Legacy deployments: CREATE TABLE strain_press_tokens without interaction_kind — parity tokens only.
	err2 := u.store.QueryRowContext(
		ctx,
		`INSERT INTO strain_press_tokens (strain_canonical) VALUES ($1) RETURNING id`,
		strainEncounterReadCacheTTL,
		strainCanonical,
	).Scan(&id)
	if err2 == nil && u.logger != nil {
		u.logger.Warn("strain_press_tokens: used legacy insert without interaction_kind; apply ALTER from assets/db.sql (additive cards need the column)")
	}
	if err2 != nil {
		return 0, err2
	}
	return id, nil
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
	if count <= 0 {
		return ""
	}
	msgs := StrainCollectionMessages()
	pluralFmt := encounterLineFmt
	if strings.TrimSpace(pluralFmt) == "" {
		pluralFmt = msgs.EncounterLine
	}
	if strings.TrimSpace(pluralFmt) == "" {
		return ""
	}
	lineFmt := pluralFmt
	if count == 1 && strings.TrimSpace(msgs.EncounterLineSingular) != "" {
		lineFmt = msgs.EncounterLineSingular
	}
	tail := strings.TrimSpace(fmt.Sprintf(lineFmt, count))
	tail = strings.TrimPrefix(tail, "Encountered:")
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return ""
	}
	return "<b>Encountered:</b> " + html.EscapeString(tail)
}

const strainDescBlockPrefix = "<b>Description:</b> "

// FormatStrainHTMLWithEncounter appends Encountered below Description when present (blank line above and below via block joins).
func FormatStrainHTMLWithEncounter(strain map[string]any, encounterCount int64, encounterLineFmt string) string {
	esc := html.EscapeString
	if encounterLineFmt == "" {
		encounterLineFmt = StrainCollectionMessages().EncounterLine
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
		blocks = append(blocks, strainDescBlockPrefix+esc(desc))
	}

	if tail := formatEncounterCountTailHTML(encounterCount, encounterLineFmt); tail != "" {
		inserted := false
		for i, b := range blocks {
			if strings.HasPrefix(b, strainDescBlockPrefix) {
				blocks = append(blocks[:i+1], append([]string{tail}, blocks[i+1:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			blocks = append(blocks, tail)
		}
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

// formatSecondaryMatchesHTML lists additional search hits beneath the primary full card (private DM ambiguous query).
func formatSecondaryMatchesHTML(hits []map[string]any) string {
	if len(hits) == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, "<b>Also close matches</b>")
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
