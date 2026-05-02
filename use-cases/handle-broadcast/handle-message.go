package handlebroadcast

import "nuglabsbot-v2/utils"

type MessageBroadcaster interface {
	SendOutbound(userID int64, msg utils.OutboundMessage) (telegramMessageID int64, err error)
}
