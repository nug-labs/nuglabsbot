package handlebroadcast

type MessageBroadcaster interface {
	SendMessage(userID int64, text string) error
}
