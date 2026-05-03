/*
Package routes receives Telegram updates (long polling via UpdateRouter.Run) and dispatches
to command/message/inline/empty routes with injected middleware/controller chains.
Workflow stage: transport routing (no business rules here).
*/
package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"nuglabsbot-v2/middleware"
	handlemessage "nuglabsbot-v2/use-cases/handle-message"
	handlestrainpress "nuglabsbot-v2/use-cases/handle-strain-press"
	"nuglabsbot-v2/utils"
)

// StrainCollectedNotifier runs after a successful strain collection button press (optional).
type StrainCollectedNotifier interface {
	NotifyAfterStrainCollected(ctx context.Context, post func(chatID int64, msg handlemessage.OutboundMessage) error, ok *handlemessage.StrainCollectConfirm) error
}

type UpdateRouter struct {
	bot                  *tgbotapi.BotAPI
	log                  *utils.Logger
	userMiddleware       *middleware.HandleUserMiddleware
	strainPressRoute     *strainPressCallbacks
	strainCollectedNotif StrainCollectedNotifier
	messageRoute         *MessageRoute
	commandRoute         *CommandRoute
	inlineRoute          *InlineRoute
	emptyRoute           *EmptyRoute
	chatMemberRoute      *ChatMemberRoute
}

type strainPressCallbacks struct {
	useCase *handlestrainpress.RootUseCase
}

func NewUpdateRouter(
	bot *tgbotapi.BotAPI,
	log *utils.Logger,
	userMiddleware *middleware.HandleUserMiddleware,
	strainPress *handlestrainpress.RootUseCase,
	strainCollected StrainCollectedNotifier,
	messageRoute *MessageRoute,
	commandRoute *CommandRoute,
	inlineRoute *InlineRoute,
	emptyRoute *EmptyRoute,
	chatMemberRoute *ChatMemberRoute,
) *UpdateRouter {
	var sp *strainPressCallbacks
	if strainPress != nil {
		sp = &strainPressCallbacks{useCase: strainPress}
	}
	return &UpdateRouter{
		bot:                  bot,
		log:                  log,
		userMiddleware:       userMiddleware,
		strainPressRoute:     sp,
		strainCollectedNotif: strainCollected,
		messageRoute:         messageRoute,
		commandRoute:         commandRoute,
		inlineRoute:          inlineRoute,
		emptyRoute:           emptyRoute,
		chatMemberRoute:      chatMemberRoute,
	}
}

// Run receives updates via long polling only (getUpdates). There is no inbound HTTP webhook in this app.
// We still call Telegram's deleteWebhook once so their servers forget any URL from an old bot config;
// otherwise getUpdates can return nothing after a webhook was previously registered.
func (r *UpdateRouter) Run(ctx context.Context) {
	if _, err := r.bot.Request(tgbotapi.DeleteWebhookConfig{}); err != nil && r.log != nil {
		r.log.Warn("could not clear Telegram webhook URL (getUpdates may fail if a webhook was set before): %v", err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = getUpdatesTimeoutSeconds()
	// Telegram: default getUpdates does not include chat_member (see Bot API "allowed_updates").
	// Member leave/kick is often only delivered as ChatMember, not as Message.left_chat_member.
	// The bot must be an administrator in the group/supergroup to receive chat_member updates.
	u.AllowedUpdates = []string{"message", "inline_query", "callback_query", "chat_member"}
	updates := r.bot.GetUpdatesChan(u)
	defer r.bot.StopReceivingUpdates()

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			started := time.Now()
			err := r.HandleUpdate(ctx, update)
			if r.log != nil {
				durationMs := time.Since(started).Milliseconds()
				kind := updateKind(update)
				if err != nil {
					r.log.Error("event=update-handle kind=%s status=error duration_ms=%d err=%v", kind, durationMs, err)
				} else {
					r.log.Info("event=update-handle kind=%s status=ok duration_ms=%d", kind, durationMs)
				}
			}
		}
	}
}

// HandleUpdate processes a single update (long poll or tests).
func (r *UpdateRouter) HandleUpdate(ctx context.Context, update tgbotapi.Update) error {
	// TEMPORARY: full raw Telegram update as JSON; remove after debugging (join/leave, chat_member, etc.).
	if r.log != nil {
		if raw, err := json.Marshal(update); err != nil {
			r.log.Warn("event=raw-update-temp status=marshal_error err=%v", err)
		} else {
			r.log.Info("event=raw-update-temp json=%s", string(raw))
		}
	}
	if update.Message != nil {
		user := middleware.TelegramUser{
			TelegramID: update.Message.From.ID,
			Username:   update.Message.From.UserName,
			FirstName:  update.Message.From.FirstName,
			LastName:   update.Message.From.LastName,
		}
		if update.Message.IsCommand() {
			reply, err := r.commandRoute.Handle(ctx, user, update.Message.Chat.ID, "/"+update.Message.Command(), update.Message.CommandArguments())
			if err != nil {
				return err
			}
			if strings.TrimSpace(reply.Text) == "" {
				return nil
			}
			return sendChatOutbound(r.bot, update.Message.Chat.ID, reply)
		}
		// Joins, member updates, pins, and many "settings" events send a Message with no Text/Caption.
		// Without this, empty input hits strain → please-provide strain copy in groups.
		body := strings.TrimSpace(update.Message.Text)
		if body == "" {
			body = strings.TrimSpace(update.Message.Caption)
		}
		if body == "" {
			reply, err := r.chatMemberRoute.HandleMessage(ctx, update)
			if err != nil {
				return err
			}
			if strings.TrimSpace(reply) != "" {
				return sendChatOutbound(r.bot, update.Message.Chat.ID, handlemessage.OutboundMessage{Text: reply})
			}
			// Member service messages are fully handled in chat-member flow; skip empty tracker to avoid duplicate events.
			if len(update.Message.NewChatMembers) > 0 || update.Message.LeftChatMember != nil {
				return nil
			}
			reply, err = r.emptyRoute.Handle(ctx, update)
			if err != nil {
				return err
			}
			if strings.TrimSpace(reply) == "" {
				return nil
			}
			return sendChatOutbound(r.bot, update.Message.Chat.ID, handlemessage.OutboundMessage{Text: reply})
		}
		reply, err := r.messageRoute.Handle(ctx, user, update.Message.Chat.ID, body)
		if err != nil {
			return err
		}
		if strings.TrimSpace(reply.Text) == "" {
			return nil
		}
		return sendChatOutbound(r.bot, update.Message.Chat.ID, reply)
	}

	if update.CallbackQuery != nil && r.strainPressRoute != nil && r.strainPressRoute.useCase != nil {
		cq := update.CallbackQuery
		if !strings.HasPrefix(strings.TrimSpace(cq.Data), handlemessage.StrainCollectionCallbackPrefix) {
			return nil
		}
		if cq.From == nil || cq.Message == nil {
			return nil
		}
		mu := middleware.TelegramUser{
			TelegramID: cq.From.ID,
			Username:   cq.From.UserName,
			FirstName:  cq.From.FirstName,
			LastName:   cq.From.LastName,
		}
		if r.userMiddleware != nil {
			if err := r.userMiddleware.EnsureUser(ctx, mu, cq.Message.Chat.ID); err != nil {
				if r.log != nil {
					r.log.Warn("callback route: ensure user failed: %v", err)
				}
				return err
			}
		}
		ans, alert, collected := r.strainPressRoute.useCase.Handle(ctx, cq)
		cbCfg := tgbotapi.CallbackConfig{
			CallbackQueryID: cq.ID,
			Text:            ans,
			ShowAlert:       alert,
		}
		if _, err := r.bot.Request(cbCfg); err != nil {
			if r.log != nil {
				r.log.Warn("callback answer failed callback_id=%s: %v", cq.ID, err)
			}
			return err
		}
		if collected != nil && r.strainCollectedNotif != nil {
			poster := func(cid int64, m handlemessage.OutboundMessage) error { return sendChatOutbound(r.bot, cid, m) }
			if err := r.strainCollectedNotif.NotifyAfterStrainCollected(ctx, poster, collected); err != nil && r.log != nil {
				r.log.Warn("strain collected follow-up notifier err=%v", err)
			}
		}
		return nil
	}

	if update.InlineQuery != nil {
		user := middleware.TelegramUser{
			TelegramID: update.InlineQuery.From.ID,
			Username:   update.InlineQuery.From.UserName,
			FirstName:  update.InlineQuery.From.FirstName,
			LastName:   update.InlineQuery.From.LastName,
		}
		hits, err := r.inlineRoute.HandleQuery(ctx, user, update.InlineQuery.Query)
		if err != nil {
			return err
		}
		results := make([]interface{}, 0, len(hits))
		thumbURL := utils.Env.AssetsURL("nuglabsbot.png")
		for i, hit := range hits {
			name := fmt.Sprintf("%v", hit["name"])
			body := handlemessage.FormatStrainHTML(hit)
			article := tgbotapi.NewInlineQueryResultArticleHTML(
				fmt.Sprintf("strain-%d", i),
				name,
				body,
			)
			article.Description = inlineResultDescription(hit)
			if thumbURL != "" {
				article.ThumbURL = thumbURL
			}
			results = append(results, article)
		}
		cfg := tgbotapi.InlineConfig{
			InlineQueryID: update.InlineQuery.ID,
			Results:       results,
			CacheTime:     1,
			IsPersonal:    true,
		}
		_, err = r.bot.Request(cfg)
		return err
	}

	if update.ChatMember != nil {
		if err := r.chatMemberRoute.Handle(ctx, update.ChatMember); err != nil {
			return err
		}
	}
	return nil
}

// getUpdatesTimeoutSeconds returns GET_UPDATES_TIMEOUT_SECONDS or 50 (Telegram API max 50).
func getUpdatesTimeoutSeconds() int {
	raw := strings.TrimSpace(os.Getenv("GET_UPDATES_TIMEOUT_SECONDS"))
	if raw == "" {
		return 50
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 50
	}
	if n > 50 {
		return 50
	}
	return n
}

func sendChatOutbound(bot *tgbotapi.BotAPI, chatID int64, msg handlemessage.OutboundMessage) error {
	cfg := tgbotapi.NewMessage(chatID, msg.Text)
	if utils.LooksLikeTelegramHTML(msg.Text) {
		cfg.ParseMode = "HTML"
	}
	if msg.ReplyMarkup != nil {
		cfg.ReplyMarkup = msg.ReplyMarkup
	}
	_, err := bot.Send(cfg)
	return err
}

func updateKind(update tgbotapi.Update) string {
	if update.Message != nil {
		if update.Message.IsCommand() {
			return "command"
		}
		return "message"
	}
	if update.InlineQuery != nil {
		return "inline"
	}
	if update.CallbackQuery != nil {
		return "callback_query"
	}
	if update.ChatMember != nil {
		return "chat_member"
	}
	return "other"
}

func inlineResultDescription(hit map[string]any) string {
	strainType := strings.TrimSpace(fmt.Sprintf("%v", hit["type"]))
	sm := strings.TrimSpace(fmt.Sprintf("%v", hit["description_sm"]))

	base := sm
	if strainType != "" && sm != "" {
		base = strainType + " • " + sm
	} else if strainType != "" {
		base = strainType
	}
	return truncateRunes(base, 96)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= max {
		return string(rs)
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}
