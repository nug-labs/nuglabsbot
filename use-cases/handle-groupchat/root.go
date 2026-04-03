/*
handle-groupchat orchestrates periodic required-group outreach.
required_groups rows come from zz-ops/convert-groups.go (assets/groups.yml).
Injected from composition root; runs only when APP_ENV=live (see app.go).
*/
package handlegroupchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-v2/utils"
)

type TelegramGroupClient interface {
	GetChatMember(config tgbotapi.GetChatMemberConfig) (tgbotapi.ChatMember, error)
}

type RootUseCase struct {
	db        utils.DB
	analytics *utils.Analytics
	logger    *utils.Logger
	telegram  TelegramGroupClient
}

func NewRootUseCase(db utils.DB, analytics *utils.Analytics, logger *utils.Logger, telegram TelegramGroupClient) *RootUseCase {
	return &RootUseCase{db: db, analytics: analytics, logger: logger, telegram: telegram}
}

func (u *RootUseCase) RunOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	cooldown := outreachFrequencyMinutes()
	requiredGroups, err := u.loadRequiredGroups(ctx)
	if err != nil {
		return err
	}
	if len(requiredGroups) == 0 {
		return nil
	}

	users, err := u.loadKnownUsers(ctx)
	if err != nil {
		return err
	}
	for _, userID := range users {
		eligible, err := u.canOutreachUser(ctx, userID, cooldown)
		if err != nil || !eligible {
			continue
		}
		missingLinks := make([]string, 0, len(requiredGroups))
		for _, g := range requiredGroups {
			member, err := u.telegram.GetChatMember(tgbotapi.GetChatMemberConfig{
				ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: g.ChatID, UserID: userID},
			})
			if err != nil || !isChatMember(member) {
				missingLinks = append(missingLinks, g.InviteLink)
			}
		}
		if len(missingLinks) == 0 {
			continue
		}

		msg := u.buildGroupInviteMessage(missingLinks)
		broadcastID, err := u.ensureMessageBroadcast(ctx, msg)
		if err != nil {
			if u.logger != nil {
				u.logger.Warn("group outreach broadcast ensure failed for user %d: %v", userID, err)
			}
			continue
		}
		if _, err := u.db.ExecContext(
			ctx,
			`INSERT INTO broadcast_outgoing (broadcast_id, user_id, scheduled_at)
			 VALUES ($1, $2, NOW())
			 ON CONFLICT (broadcast_id, user_id) DO NOTHING`,
			broadcastID, userID,
		); err != nil {
			continue
		}
		_, _ = u.db.ExecContext(
			ctx,
			`INSERT INTO group_outreach_log (user_id, last_sent_at)
			 VALUES ($1, NOW())
			 ON CONFLICT (user_id) DO UPDATE SET last_sent_at = NOW()`,
			userID,
		)
		_ = u.analytics.TrackEvent(ctx, utils.AnalyticsEvent{
			Name:   "group-outreach-queued",
			UserID: userID,
			Status: "ok",
			Meta:   map[string]any{"missing_groups": len(missingLinks)},
		})
	}
	return nil
}

type requiredGroup struct {
	ChatID     int64
	InviteLink string
}

func (u *RootUseCase) loadRequiredGroups(ctx context.Context) ([]requiredGroup, error) {
	rows, err := u.db.QueryContext(ctx, `SELECT chat_id, invite_link FROM required_groups WHERE enabled = TRUE ORDER BY chat_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []requiredGroup
	for rows.Next() {
		var g requiredGroup
		if err := rows.Scan(&g.ChatID, &g.InviteLink); err != nil {
			return nil, err
		}
		if g.ChatID != 0 && strings.TrimSpace(g.InviteLink) != "" {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

func (u *RootUseCase) loadKnownUsers(ctx context.Context) ([]int64, error) {
	rows, err := u.db.QueryContext(ctx, `SELECT telegram_id FROM users ORDER BY telegram_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (u *RootUseCase) canOutreachUser(ctx context.Context, userID int64, cooldown time.Duration) (bool, error) {
	var lastSent time.Time
	err := u.db.QueryRowContext(ctx, `SELECT last_sent_at FROM group_outreach_log WHERE user_id = $1`, userID).Scan(&lastSent)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return time.Since(lastSent) >= cooldown, nil
}

func (u *RootUseCase) ensureMessageBroadcast(ctx context.Context, message string) (string, error) {
	payloadRaw, err := json.Marshal(map[string]any{"text": message})
	if err != nil {
		return "", err
	}
	payload := string(payloadRaw)
	var id string
	err = u.db.QueryRowContext(
		ctx,
		`SELECT id FROM broadcasts WHERE type = 'message' AND payload = $1::jsonb ORDER BY created_at ASC LIMIT 1`,
		payload,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = fmt.Sprintf("group-outreach-%d", time.Now().UTC().UnixNano())
	_, err = u.db.ExecContext(
		ctx,
		`INSERT INTO broadcasts (id, type, payload, created_at) VALUES ($1, 'message', $2::jsonb, $3)`,
		id, payload, time.Now().UTC(),
	)
	return id, err
}

func isChatMember(m tgbotapi.ChatMember) bool {
	switch m.Status {
	case "member", "administrator", "creator":
		return true
	default:
		return false
	}
}

func (u *RootUseCase) buildGroupInviteMessage(inviteLinks []string) string {
	template := strings.TrimSpace(loadGroupInviteTemplate())
	if template != "" {
		return strings.ReplaceAll(template, "{{INVITE_LINKS}}", strings.Join(inviteLinks, "\n"))
	}
	var b strings.Builder
	b.WriteString("<b>Join our required groups</b>\n")
	b.WriteString("Please join the following groups to keep receiving updates:\n\n")
	for _, link := range inviteLinks {
		b.WriteString(link)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func loadGroupInviteTemplate() string {
	path := filepath.Join(".", "assets", "broadcasts", "msg_group_invite.yml")
	var meta map[string]any
	var body map[string]any
	if err := utils.ParseFrontMatterYAML(path, &meta, &body); err != nil {
		return ""
	}
	text, _ := body["text"].(string)
	return strings.TrimSpace(text)
}

func outreachFrequencyMinutes() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GROUPCHAT_FREQUENCY_MINUTES"))
	if raw == "" {
		return 60 * time.Minute
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(n) * time.Minute
}
