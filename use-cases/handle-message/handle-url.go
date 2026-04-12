// Extract URL from message and understand the root domain
// If domain is in whitelist table ( inject in db stub ), curl the body of the URL
// Pass body to Gemini API with sys prompt from assets/prompts/extract-strains.txt
// For each item in returned JSON list, check if valid strain in injected nug labs client
// Then return message of deeplinks to found strains and underneath say which strains have been extracted and couldn't be found
// Each strain should emit strain-found and strain-not-found events injected from analytics

package handlemessage

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

const whitelistLookupCacheTTL = 24 * time.Hour

type urlStrainClient interface {
	GetStrain(ctx context.Context, name string) (map[string]any, error)
}

type HandleURLUseCase struct {
	store         db.DB
	analytics     *utils.Analytics
	nuglabsClient urlStrainClient
	logger        *utils.Logger
}

func NewHandleURLUseCase(
	store db.DB,
	analytics *utils.Analytics,
	nuglabsClient urlStrainClient,
	logger *utils.Logger,
) *HandleURLUseCase {
	return &HandleURLUseCase{
		store:         store,
		analytics:     analytics,
		nuglabsClient: nuglabsClient,
		logger:        logger,
	}
}

func (u *HandleURLUseCase) Handle(ctx context.Context, actorUserID, chatID int64, input string) (string, error) {
	// Message root already routes here only for likely URLs; parse is defense in depth.
	parsed, err := url.Parse(strings.TrimSpace(input))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "Please send a valid URL.", nil
	}
	rawURL := parsed.String()

	allowed, err := u.isWhitelisted(ctx, rawURL, parsed.Host)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "URL domain is not whitelisted.", nil
	}

	body, err := fetchBody(ctx, rawURL)
	if err != nil {
		return "", err
	}
	body = extractBodyText(body)
	if strings.TrimSpace(body) == "" {
		return "Unable to extract readable body content from URL.", nil
	}

	candidates, err := u.extractWithGemini(ctx, actorUserID, chatID, body)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "No strain names could be extracted.", nil
	}

	foundStrains := make([]map[string]any, 0, len(candidates))
	notFound := make([]string, 0)
	for _, name := range candidates {
		strain, err := u.nuglabsClient.GetStrain(ctx, name)
		if err != nil {
			continue
		}
		if strain != nil {
			foundStrains = append(foundStrains, strain)
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "strain-found",
				UserID: actorUserID,
				Status: "ok",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"name": name}),
			})
		} else {
			notFound = append(notFound, name)
			_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
				Name:   "strain-not-found",
				UserID: actorUserID,
				Status: "miss",
				Meta:   utils.MetaWithChatID(chatID, map[string]any{"name": name}),
			})
		}
	}

	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   "url-processed",
		UserID: actorUserID,
		Status: "ok",
		Meta: utils.MetaWithChatID(chatID, map[string]any{
			"url": rawURL, "candidates": len(candidates), "found": len(foundStrains),
		}),
	})

	var b strings.Builder
	if len(foundStrains) > 0 {
		b.WriteString(formatURLFoundStrainsHTML(foundStrains))
	}
	if len(notFound) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Not found:\n")
		for _, name := range notFound {
			b.WriteString("- ")
			b.WriteString(html.EscapeString(name))
			b.WriteString("\n")
		}
	}
	msg := strings.TrimSpace(b.String())
	if msg == "" {
		msg = "No known strains found from URL content."
	}
	return msg, nil
}

func (u *HandleURLUseCase) extractWithGemini(ctx context.Context, actorUserID, chatID int64, body string) ([]string, error) {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	promptPath := filepath.Join(".", "assets", "prompts", "extract-strains.txt")
	systemPromptRaw, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("read prompt file: %w", err)
	}

	const maxBodyChars = 12000
	_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
		Name:   "url-body-length",
		UserID: actorUserID,
		Status: "ok",
		Meta:   utils.MetaWithChatID(chatID, map[string]any{"length": len(body)}),
	})
	if len(body) > maxBodyChars {
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "url-body-length-exceeded",
			UserID: actorUserID,
			Status: "truncated",
			Meta:   utils.MetaWithChatID(chatID, map[string]any{"length": len(body), "max": maxBodyChars}),
		})
		body = body[:maxBodyChars]
	}

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]any{
					{"text": string(systemPromptRaw)},
					{"text": "Raw text:\n" + body},
				},
			},
		},
	}
	payload, _ := json.Marshal(reqBody)
	if u.logger != nil {
		u.logger.Info("[gemini] body excerpt (%d chars): %s", len(body), inlineTruncate(body, 1200))
	}

	endpoint := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=" + url.QueryEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if u.logger != nil {
			u.logger.Warn("[gemini] non-200 raw response: %s", inlineTruncate(string(rawResp), 1000))
		}
		return nil, fmt.Errorf("gemini failed: status %d", resp.StatusCode)
	}

	text := extractGeminiText(rawResp)
	if u.logger != nil {
		u.logger.Info("[gemini] extracted text: %s", inlineTruncate(text, 600))
	}
	if text == "" {
		return []string{}, nil
	}
	return parseStrainJSONArray(text), nil
}

func extractGeminiText(raw []byte) string {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ""
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parsed.Candidates[0].Content.Parts[0].Text)
}

func parseStrainJSONArray(text string) []string {
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, "["); idx >= 0 {
		text = text[idx:]
	}
	if idx := strings.LastIndex(text, "]"); idx >= 0 {
		text = text[:idx+1]
	}

	var names []string
	if err := json.Unmarshal([]byte(text), &names); err != nil {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		clean := strings.TrimSpace(n)
		if clean == "" {
			continue
		}
		k := strings.ToLower(clean)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func (u *HandleURLUseCase) isWhitelisted(ctx context.Context, rawURL string, host string) (bool, error) {
	rows, err := u.store.QueryContext(ctx, `SELECT domain FROM whitelist`, whitelistLookupCacheTTL)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return false, err
		}
		entry = strings.TrimSpace(strings.ToLower(entry))
		if entry == "" {
			continue
		}

		rawLower := strings.ToLower(rawURL)
		hostLower := strings.ToLower(host)

		// Exact URL match.
		if entry == rawLower {
			return true, nil
		}

		// URL origin/prefix match
		if strings.HasPrefix(entry, "http://") || strings.HasPrefix(entry, "https://") {
			if strings.HasPrefix(rawLower, entry) {
				return true, nil
			}
			parsed, err := url.Parse(entry)
			if err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, hostLower) {
				return true, nil
			}
			continue
		}

		// Host-only match.
		if entry == hostLower {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func fetchBody(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("url fetch failed with status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2_000_000))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func extractBodyText(pageHTML string) string {
	reBody := regexp.MustCompile(`(?is)<body[^>]*>(.*?)</body>`)
	m := reBody.FindStringSubmatch(pageHTML)
	if len(m) < 2 {
		return ""
	}
	block := m[1]

	reBreaks := regexp.MustCompile(`(?is)<\s*(br|/p|/div|/li)\s*/?\s*>`)
	block = reBreaks.ReplaceAllString(block, "\n")

	reTags := regexp.MustCompile(`(?is)<[^>]+>`)
	text := reTags.ReplaceAllString(block, " ")
	text = html.UnescapeString(text)

	spaceRe := regexp.MustCompile(`[ \t\r\f\v]+`)
	text = spaceRe.ReplaceAllString(text, " ")
	newlineRe := regexp.MustCompile(`\n+`)
	text = newlineRe.ReplaceAllString(text, "\n")
	return strings.TrimSpace(text)
}

func formatURLFoundStrainsHTML(found []map[string]any) string {
	var blocks []string
	blocks = append(blocks, "Found strains")
	for _, strain := range found {
		display := anyToString(strain["name"])
		if display == "" {
			continue
		}
		line := "<b>" + html.EscapeString(display) + "</b>\n" + StrainDeeplink(display)
		blocks = append(blocks, line)
	}
	return strings.Join(blocks, "\n\n")
}

func inlineTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...<truncated>"
}
