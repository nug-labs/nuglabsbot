package handlebroadcast

import handlemessage "nuglabsbot-v2/use-cases/handle-message"

type MessageBroadcaster interface {
	SendOutbound(userID int64, msg handlemessage.OutboundMessage) (telegramMessageID int64, err error)
}
