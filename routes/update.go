/*
Package routes/update receives Telegram updates (long polling via UpdateRouter.Run) and dispatches
to command/message/inline routes with injected middleware/controller chains.
Workflow stage: transport routing (no business rules here).
*/
package routes

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"telegram-v2/middleware"
	"telegram-v2/use-cases/handle-message"
	"telegram-v2/utils"
)

type UpdateRouter struct {
	bot          *tgbotapi.BotAPI
	log          *utils.Logger
	messageRoute *MessageRoute
	commandRoute *CommandRoute
	inlineRoute  *InlineRoute
}

func NewUpdateRouter(
	bot *tgbotapi.BotAPI,
	log *utils.Logger,
	messageRoute *MessageRoute,
	commandRoute *CommandRoute,
	inlineRoute *InlineRoute,
) *UpdateRouter {
	return &UpdateRouter{
		bot:          bot,
		log:          log,
		messageRoute: messageRoute,
		commandRoute: commandRoute,
		inlineRoute:  inlineRoute,
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
			if err := r.HandleUpdate(ctx, update); err != nil && r.log != nil {
				r.log.Error("handle update: %v", err)
			}
		}
	}
}

// HandleUpdate processes a single update (long poll or tests).
func (r *UpdateRouter) HandleUpdate(ctx context.Context, update tgbotapi.Update) error {
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
			if strings.TrimSpace(reply) == "" {
				return nil
			}
			_, err = r.bot.Send(newHTMLMessageIfNeeded(update.Message.Chat.ID, reply))
			return err
		}
		// Joins, member updates, pins, and many "settings" events send a Message with no Text/Caption.
		// Without this, empty input hits strain → "Please provide a strain name." in groups.
		body := strings.TrimSpace(update.Message.Text)
		if body == "" {
			body = strings.TrimSpace(update.Message.Caption)
		}
		if body == "" {
			return nil
		}
		reply, err := r.messageRoute.Handle(ctx, user, update.Message.Chat.ID, body)
		if err != nil {
			return err
		}
		if strings.TrimSpace(reply) == "" {
			return nil
		}
		_, err = r.bot.Send(newHTMLMessageIfNeeded(update.Message.Chat.ID, reply))
		return err
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
		for i, hit := range hits {
			name := fmt.Sprintf("%v", hit["name"])
			body := handlemessage.FormatStrainHTML(hit)
			article := tgbotapi.NewInlineQueryResultArticleHTML(
				fmt.Sprintf("strain-%d", i),
				name,
				body,
			)
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

func newHTMLMessageIfNeeded(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	if strings.Contains(text, "<b>") || strings.Contains(text, "<a ") {
		m.ParseMode = "HTML"
	}
	return m
}
