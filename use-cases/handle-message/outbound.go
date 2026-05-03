package handlemessage

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

// OutboundMessage is a chat-bound Telegram message body plus optional inline keyboard markup.
type OutboundMessage struct {
	Text        string
	ReplyMarkup *tgbotapi.InlineKeyboardMarkup
}

// SubscriptionPlainSender pushes plain strain HTML to subscription chats (DM or group telegram_id).
type SubscriptionPlainSender interface {
	SendOutbound(chatID int64, msg OutboundMessage) (int64, error)
}
