/*
Package routes/update receives raw Telegram updates and dispatches
to command/message/inline routes with injected middleware/controller chains.
Workflow stage: transport routing (no business rules here).
*/
package routes

import (
	"context"
	"fmt"
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

func (r *UpdateRouter) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := r.bot.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := r.handleUpdate(ctx, update); err != nil && r.log != nil {
				r.log.Error("update handling failed: %v", err)
			}
		}
	}
}

func (r *UpdateRouter) handleUpdate(ctx context.Context, update tgbotapi.Update) error {
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
		reply, err := r.messageRoute.Handle(ctx, user, update.Message.Chat.ID, update.Message.Text)
		if err != nil {
			return err
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

func newHTMLMessageIfNeeded(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	if strings.Contains(text, "<b>") || strings.Contains(text, "<a ") {
		m.ParseMode = "HTML"
	}
	return m
}
