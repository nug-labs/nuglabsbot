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
	bgservices "nuglabsbot-v2/bg-services"
	"nuglabsbot-v2/controllers"
	"nuglabsbot-v2/middleware"
	"nuglabsbot-v2/routes"
	handlebroadcast "nuglabsbot-v2/use-cases/handle-broadcast"
	handlechatmember "nuglabsbot-v2/use-cases/handle-chat-member"
	handlecommand "nuglabsbot-v2/use-cases/handle-command"
	handleempty "nuglabsbot-v2/use-cases/handle-empty"
	handleevents "nuglabsbot-v2/use-cases/handle-events"
	handleinline "nuglabsbot-v2/use-cases/handle-inline"
	handlemessage "nuglabsbot-v2/use-cases/handle-message"
	handlestrainpress "nuglabsbot-v2/use-cases/handle-strain-press"
	handlesubscribe "nuglabsbot-v2/use-cases/handle-subscribe"
	"nuglabsbot-v2/utils"
	"nuglabsbot-v2/utils/db"
)

/*
app.go is the composition root for nuglabsbot-v2.
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
	handleStrainUC := handlemessage.NewHandleStrainUseCase(nugClient, analytics, database, deferredWrites, logger, broadcastSender)
	handleURLUC := handlemessage.NewHandleURLUseCase(database, analytics, nugClient, logger)
	handleMessageRootUC := handlemessage.NewRootUseCase(handleURLUC, handleStrainUC, handleUnknownUC, analytics)

	handlePolicyUC := handlecommand.NewRootUseCase(handlecommand.NewHandlePolicyUseCase(), analytics)
	handleSubscribeUC := handlesubscribe.NewRootUseCase(database, analytics)
	handleInlineUC := handleinline.NewHandleInlineUseCase(nugClient, analytics)
	handleEmptyUC := handleempty.NewRootUseCase(analytics)
	handleChatMemberUC := handlechatmember.NewRootUseCase(bot, analytics)
	handleBroadcastUC := handlebroadcast.NewRootUseCase(database, analytics, broadcastSender, broadcastSender, handleStrainUC, logger)

	userMiddleware := middleware.NewHandleUserMiddleware(database, analytics, deferredWrites)
	messageController := controllers.NewMessageController(handleMessageRootUC)
	commandController := controllers.NewCommandController(handleStrainUC, handlePolicyUC, handleSubscribeUC, analytics)
	inlineController := controllers.NewInlineController(handleInlineUC, handleStrainUC)
	emptyController := controllers.NewEmptyController(handleEmptyUC)
	chatMemberController := controllers.NewChatMemberController(handleChatMemberUC)

	messageRoute := routes.NewMessageRoute(userMiddleware, messageController, logger)
	commandRoute := routes.NewCommandRoute(userMiddleware, commandController, logger)
	inlineRoute := routes.NewInlineRoute(userMiddleware, inlineController, logger)
	emptyRoute := routes.NewEmptyRoute(emptyController, logger)
	chatMemberRoute := routes.NewChatMemberRoute(chatMemberController, logger)
	strainPressUC := handlestrainpress.NewRootUseCase(database, analytics, logger)
	updateRouter := routes.NewUpdateRouter(bot, logger, userMiddleware, strainPressUC, handleStrainUC, messageRoute, commandRoute, inlineRoute, emptyRoute, chatMemberRoute)
	broadcastService := bgservices.NewHandleBroadcastService(handleBroadcastUC, logger)

	go updateRouter.Run(ctx)

	if utils.Env.BroadcastSchedulerEnabled() {
		go broadcastService.RunEvery(ctx, pollBroadcastInterval())
		logger.Info("background services started (broadcast)")
	} else {
		logger.Info("background services skipped (set APP_ENV=live or APP_ENV=test to enable broadcast scheduler)")
	}
	logger.Info("nuglabsbot-v2 composition root initialized")
	<-ctx.Done()
	logger.Info("nuglabsbot-v2 shutting down")
}

type telegramBroadcaster struct {
	bot *tgbotapi.BotAPI
}

func (t *telegramBroadcaster) SendOutbound(chatID int64, msg handlemessage.OutboundMessage) (int64, error) {
	cfg := tgbotapi.NewMessage(chatID, msg.Text)
	if utils.LooksLikeTelegramHTML(msg.Text) {
		cfg.ParseMode = "HTML"
	}
	if msg.ReplyMarkup != nil {
		cfg.ReplyMarkup = msg.ReplyMarkup
	}
	sent, err := t.bot.Send(cfg)
	if err != nil {
		return 0, err
	}
	return int64(sent.MessageID), nil
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
