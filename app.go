package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	nuglabs "github.com/nug-labs/go-sdk"
	bgservices "telegram-v2/bg-services"
	"telegram-v2/controllers"
	"telegram-v2/middleware"
	"telegram-v2/routes"
	handlebroadcast "telegram-v2/use-cases/handle-broadcast"
	handlecommand "telegram-v2/use-cases/handle-command"
	handlegroupchat "telegram-v2/use-cases/handle-groupchat"
	handleinline "telegram-v2/use-cases/handle-inline"
	handlemessage "telegram-v2/use-cases/handle-message"
	handlesubscribe "telegram-v2/use-cases/handle-subscribe"
	"telegram-v2/utils"
)

/*
app.go is the composition root for telegram-v2.
It wires utils, middleware, controllers, use-cases, and background services.
It does not hold business rules; route/update dispatch lives in routes package.
*/

func main() {
	utils.Env.Init()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := utils.NewLogger()

	db, err := utils.DatabaseManager.Init(ctx)
	if err != nil {
		logger.Error("database init failed: %v", err)
		panic(err)
	}
	defer db.Close()

	analytics := utils.AnalyticsManager.Init(db, logger)

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		panic("TELEGRAM_BOT_TOKEN is required")
	}
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		panic(err)
	}
	broadcastSender := &telegramBroadcaster{bot: bot}

	// NugLabs SDK: AutoSync refreshes the strain index into StorageDir; the client keeps a workable in-memory view for queries.
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
	handleStrainUC := handlemessage.NewHandleStrainUseCase(nugClient, analytics, db, logger)
	handleURLUC := handlemessage.NewHandleURLUseCase(db, analytics, nugClient, logger)
	handleMessageRootUC := handlemessage.NewRootUseCase(handleURLUC, handleStrainUC, handleUnknownUC, analytics)

	handlePolicyUC := handlecommand.NewRootUseCase(handlecommand.NewHandlePolicyUseCase(), analytics)
	handleSubscribeUC := handlesubscribe.NewRootUseCase(db, analytics)
	handleInlineUC := handleinline.NewHandleInlineUseCase(nugClient, analytics)
	handleBroadcastUC := handlebroadcast.NewRootUseCase(db, analytics, broadcastSender, broadcastSender)
	handleGroupchatUC := handlegroupchat.NewRootUseCase(db, analytics, logger, bot)

	userMiddleware := middleware.NewHandleUserMiddleware(db, analytics)
	messageController := controllers.NewMessageController(handleMessageRootUC)
	commandController := controllers.NewCommandController(handleStrainUC, handlePolicyUC, handleSubscribeUC, analytics)
	inlineController := controllers.NewInlineController(handleInlineUC, handleStrainUC)

	messageRoute := routes.NewMessageRoute(userMiddleware, messageController, logger)
	commandRoute := routes.NewCommandRoute(userMiddleware, commandController, logger)
	inlineRoute := routes.NewInlineRoute(userMiddleware, inlineController, logger)
	updateRouter := routes.NewUpdateRouter(bot, logger, messageRoute, commandRoute, inlineRoute)
	broadcastService := bgservices.NewHandleBroadcastService(handleBroadcastUC, logger)
	groupchatService := bgservices.NewHandleGroupchatService(handleGroupchatUC, logger)

	go updateRouter.Run(ctx)
	if utils.Env.IsLive() {
		go broadcastService.RunEvery(ctx, pollBroadcastInterval())
		go groupchatService.RunEvery(ctx, pollGroupchatInterval())
		logger.Info("background services started (broadcast, groupchat)")
	} else {
		logger.Info("background services skipped (APP_ENV is not live; set APP_ENV=live to enable broadcast + groupchat schedulers)")
	}

	logger.Info("telegram-v2 composition root initialized")
	<-ctx.Done()
	logger.Info("telegram-v2 shutting down")
}

type telegramBroadcaster struct {
	bot *tgbotapi.BotAPI
}

func (t *telegramBroadcaster) SendMessage(chatID int64, text string) error {
	_, err := t.bot.Send(telegramHTMLMessage(chatID, text))
	return err
}

func telegramHTMLMessage(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	if strings.Contains(text, "<b>") || strings.Contains(text, "<a ") {
		m.ParseMode = "HTML"
	}
	return m
}

func (t *telegramBroadcaster) SendQuiz(userID int64, question string, options []string, correctIndex int) error {
	quiz := tgbotapi.NewPoll(userID, question, options...)
	quiz.Type = "quiz"
	quiz.CorrectOptionID = int64(correctIndex)
	quiz.IsAnonymous = false
	_, err := t.bot.Send(quiz)
	return err
}

func pollBroadcastInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("POLL_BROADCAST_INTERVAL_SECONDS"))
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return time.Minute
	}
	return time.Duration(secs) * time.Second
}

// pollGroupchatInterval matches GROUPCHAT_FREQUENCY_MINUTES (same env as handlegroupchat cooldown semantics for tick rate).
func pollGroupchatInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GROUPCHAT_FREQUENCY_MINUTES"))
	if raw == "" {
		return 60 * time.Minute
	}
	mins, err := strconv.Atoi(raw)
	if err != nil || mins <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(mins) * time.Minute
}
