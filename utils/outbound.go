package utils

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

// OutboundMessage is a chat-bound Telegram message body plus optional inline keyboard markup.
type OutboundMessage struct {
	Text          string
	ReplyMarkup   *tgbotapi.InlineKeyboardMarkup
}
