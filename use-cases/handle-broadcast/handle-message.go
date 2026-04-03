package handlebroadcast

type MessageBroadcaster interface {
	SendMessage(userID int64, text string) (telegramMessageID int64, err error)
}
