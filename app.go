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
	handleevents "telegram-v2/use-cases/handle-events"
	handleinline "telegram-v2/use-cases/handle-inline"
	handlemessage "telegram-v2/use-cases/handle-message"
	handlesubscribe "telegram-v2/use-cases/handle-subscribe"
	"telegram-v2/utils"
	"telegram-v2/utils/db"
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

	logger := utils.NewAsyncLogger(ctx)

	database, err := db.DatabaseManager.Init(ctx)
	if err != nil {
		logger.Error("database init failed: %v", err)
		panic(err)
	}
	defer database.Close()

	analytics := utils.AnalyticsManager.Init(logger)
	deferredWrites := db.NewDeferredWriteQueue()
	go deferredWrites.Run(ctx, database, logger)

	handleEventsUC := handleevents.NewRootUseCase(database, analytics, logger)
	eventsService := bgservices.NewHandleEventsService(handleEventsUC, logger)
	go eventsService.Run(ctx)

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

	handleUnknownUC := handlemessage.NewHandleUnknownUseCase(database, analytics)
	handleStrainUC := handlemessage.NewHandleStrainUseCase(nugClient, analytics, database, logger)
	handleURLUC := handlemessage.NewHandleURLUseCase(database, analytics, nugClient, logger)
	handleMessageRootUC := handlemessage.NewRootUseCase(handleURLUC, handleStrainUC, handleUnknownUC, analytics)

	handlePolicyUC := handlecommand.NewRootUseCase(handlecommand.NewHandlePolicyUseCase(), analytics)
	handleSubscribeUC := handlesubscribe.NewRootUseCase(database, analytics)
	handleInlineUC := handleinline.NewHandleInlineUseCase(nugClient, analytics)
	handleBroadcastUC := handlebroadcast.NewRootUseCase(database, analytics, broadcastSender, broadcastSender, logger)

	userMiddleware := middleware.NewHandleUserMiddleware(database, analytics, deferredWrites)
	messageController := controllers.NewMessageController(handleMessageRootUC)
	commandController := controllers.NewCommandController(handleStrainUC, handlePolicyUC, handleSubscribeUC, analytics)
	inlineController := controllers.NewInlineController(handleInlineUC, handleStrainUC)

	messageRoute := routes.NewMessageRoute(userMiddleware, messageController, logger)
	commandRoute := routes.NewCommandRoute(userMiddleware, commandController, logger)
	inlineRoute := routes.NewInlineRoute(userMiddleware, inlineController, logger)
	updateRouter := routes.NewUpdateRouter(bot, logger, messageRoute, commandRoute, inlineRoute)
	broadcastService := bgservices.NewHandleBroadcastService(handleBroadcastUC, logger)

	go updateRouter.Run(ctx)

	if utils.Env.BroadcastSchedulerEnabled() {
		go broadcastService.RunEvery(ctx, pollBroadcastInterval())
		logger.Info("background services started (broadcast)")
	} else {
		logger.Info("background services skipped (set APP_ENV=live or APP_ENV=test to enable broadcast scheduler)")
	}
	logger.Info("telegram-v2 composition root initialized")
	<-ctx.Done()
	logger.Info("telegram-v2 shutting down")
}

type telegramBroadcaster struct {
	bot *tgbotapi.BotAPI
}

func (t *telegramBroadcaster) SendMessage(chatID int64, text string) (int64, error) {
	msg, err := t.bot.Send(telegramHTMLMessage(chatID, text))
	if err != nil {
		return 0, err
	}
	return int64(msg.MessageID), nil
}

func telegramHTMLMessage(chatID int64, text string) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(chatID, text)
	if looksLikeTelegramHTML(text) {
		m.ParseMode = "HTML"
	}
	return m
}

// looksLikeTelegramHTML enables HTML parse mode for broadcast-style bodies (see assets/broadcasts).
func looksLikeTelegramHTML(text string) bool {
	if !strings.Contains(text, "<") || !strings.Contains(text, ">") {
		return false
	}
	// https://core.telegram.org/bots/api#html-style
	return strings.Contains(text, "<b>") || strings.Contains(text, "<strong>") ||
		strings.Contains(text, "<i>") || strings.Contains(text, "<em>") ||
		strings.Contains(text, "<u>") || strings.Contains(text, "<s>") || strings.Contains(text, "<strike>") ||
		strings.Contains(text, "<code>") || strings.Contains(text, "<pre>") ||
		strings.Contains(text, "<a ") || strings.Contains(text, "<tg-spoiler>") ||
		strings.Contains(text, "<blockquote>") || strings.Contains(text, "<br")
}

func (t *telegramBroadcaster) SendQuiz(userID int64, question string, options []string, correctIndex int) (int64, error) {
	quiz := tgbotapi.NewPoll(userID, question, options...)
	quiz.Type = "quiz"
	quiz.CorrectOptionID = int64(correctIndex)
	quiz.IsAnonymous = false
	msg, err := t.bot.Send(quiz)
	if err != nil {
		return 0, err
	}
	return int64(msg.MessageID), nil
}

func pollBroadcastInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("POLL_BROADCAST_INTERVAL_SECONDS"))
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return time.Minute
	}
	return time.Duration(secs) * time.Second
}
