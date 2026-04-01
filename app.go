package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	nuglabs "github.com/nug-labs/go-sdk"
	bgservices "telegram-v2/bg-services"
	"telegram-v2/controllers"
	"telegram-v2/middleware"
	"telegram-v2/routes"
	handlebroadcast "telegram-v2/use-cases/handle-broadcast"
	handlecommand "telegram-v2/use-cases/handle-command"
	handleinline "telegram-v2/use-cases/handle-inline"
	handlemessage "telegram-v2/use-cases/handle-message"
	"telegram-v2/utils"
)

// App composition root
// Import dendencies like routes, controllers, use-cases, utils, middlewares etc
// Initialize dependencies
// conditional on whether to use .env or .env.test

const LIVE = false

func main() {
	loadEnv()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := utils.NewLogger()

	db, err := utils.OpenDatabaseFromEnv(ctx)
	if err != nil {
		logger.Error("database init failed: %v", err)
		panic(err)
	}
	defer db.Close()

	analytics := utils.NewAnalytics(db, logger)

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		panic("TELEGRAM_BOT_TOKEN is required")
	}
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		panic(err)
	}
	broadcastSender := &telegramBroadcaster{bot: bot}

	nugClient, err := nuglabs.NewClient(ctx, &nuglabs.ClientOptions{
		AutoSync:   true,
		StorageDir: "./.nuglabs-cache",
	})
	if err != nil {
		logger.Error("nuglabs client init failed: %v", err)
		panic(err)
	}
	defer nugClient.Close(ctx)

	if _, err := nugClient.ForceResync(ctx); err != nil {
		logger.Warn("nuglabs force resync failed, continuing with bundled data: %v", err)
	}

	handleUnknownUC := handlemessage.NewHandleUnknownUseCase(db, analytics)
	handleStrainUC := handlemessage.NewHandleStrainUseCase(nugClient, analytics)
	handleURLUC := handlemessage.NewHandleURLUseCase(db, analytics, nugClient)
	handleMessageRootUC := handlemessage.NewRootUseCase(handleURLUC, handleStrainUC, handleUnknownUC, analytics)

	handlePolicyUC := handlecommand.NewHandlePolicyUseCase(analytics)
	handleInlineUC := handleinline.NewHandleInlineUseCase(nugClient, analytics)
	handleBroadcastUC := handlebroadcast.NewRootUseCase(db, analytics, broadcastSender, broadcastSender)

	userMiddleware := middleware.NewHandleUserMiddleware(db)
	messageController := controllers.NewMessageController(handleMessageRootUC)
	commandController := controllers.NewCommandController(handleStrainUC, handlePolicyUC)
	inlineController := controllers.NewInlineController(handleInlineUC, handleStrainUC)

	messageRoute := routes.NewMessageRoute(userMiddleware, messageController)
	commandRoute := routes.NewCommandRoute(userMiddleware, commandController)
	inlineRoute := routes.NewInlineRoute(userMiddleware, inlineController)
	broadcastService := bgservices.NewHandleBroadcastService(handleBroadcastUC, logger)

	go broadcastService.RunEvery(ctx, time.Minute)
	go runTelegramLoop(ctx, logger, bot, messageRoute, commandRoute, inlineRoute)

	logger.Info("telegram-v2 composition root initialized")
	<-ctx.Done()
	logger.Info("telegram-v2 shutting down")
}

func loadEnv() {
	if LIVE {
		_ = godotenv.Load(".env")
		return
	}
	_ = godotenv.Load(".env.test")
}

func runTelegramLoop(
	ctx context.Context,
	logger *utils.Logger,
	bot *tgbotapi.BotAPI,
	messageRoute *routes.MessageRoute,
	commandRoute *routes.CommandRoute,
	inlineRoute *routes.InlineRoute,
) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := handleUpdate(ctx, bot, update, messageRoute, commandRoute, inlineRoute); err != nil {
				logger.Error("update handling failed: %v", err)
			}
		}
	}
}

func handleUpdate(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	update tgbotapi.Update,
	messageRoute *routes.MessageRoute,
	commandRoute *routes.CommandRoute,
	inlineRoute *routes.InlineRoute,
) error {
	if update.Message != nil {
		user := middleware.TelegramUser{
			TelegramID: update.Message.From.ID,
			Username:   update.Message.From.UserName,
			FirstName:  update.Message.From.FirstName,
			LastName:   update.Message.From.LastName,
		}

		if update.Message.IsCommand() {
			reply, err := commandRoute.Handle(ctx, user, "/"+update.Message.Command(), update.Message.CommandArguments())
			if err != nil {
				return err
			}
			_, err = bot.Send(newHTMLMessageIfNeeded(update.Message.Chat.ID, reply))
			return err
		}

		reply, err := messageRoute.Handle(ctx, user, update.Message.Text)
		if err != nil {
			return err
		}
		_, err = bot.Send(newHTMLMessageIfNeeded(update.Message.Chat.ID, reply))
		return err
	}

	if update.InlineQuery != nil {
		user := middleware.TelegramUser{
			TelegramID: update.InlineQuery.From.ID,
			Username:   update.InlineQuery.From.UserName,
			FirstName:  update.InlineQuery.From.FirstName,
			LastName:   update.InlineQuery.From.LastName,
		}
		hits, err := inlineRoute.HandleQuery(ctx, user, update.InlineQuery.Query)
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
		_, err = bot.Request(cfg)
		return err
	}

	return nil
}

func newHTMLMessageIfNeeded(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	if strings.Contains(text, "<b>") {
		m.ParseMode = "HTML"
	}
	return m
}

type telegramBroadcaster struct {
	bot *tgbotapi.BotAPI
}

func (t *telegramBroadcaster) SendMessage(userID int64, text string) error {
	_, err := t.bot.Send(tgbotapi.NewMessage(userID, text))
	return err
}

func (t *telegramBroadcaster) SendQuiz(userID int64, question string, options []string, correctIndex int) error {
	quiz := tgbotapi.NewPoll(userID, question, options...)
	quiz.Type = "quiz"
	quiz.CorrectOptionID = int64(correctIndex)
	quiz.IsAnonymous = false
	_, err := t.bot.Send(quiz)
	return err
}
