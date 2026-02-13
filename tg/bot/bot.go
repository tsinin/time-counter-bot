package bot

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var Bot *tgbotapi.BotAPI

// IsBotBlockedError checks if the error is a "Forbidden: bot was blocked by the user" error
// from the Telegram API.
func IsBotBlockedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Forbidden")
}
