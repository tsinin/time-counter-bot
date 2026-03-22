package routes

import (
	"log"

	"TimeCounterBot/common"
	"TimeCounterBot/db"
	"TimeCounterBot/tg/bot"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func NotifyCommand(message *tgbotapi.Message, start bool) {
	tgUser := message.From
	if tgUser == nil {
		return
	}

	userID := common.UserID(tgUser.ID)

	user, err := db.GetUserByID(userID)
	if err != nil {
		log.Fatal(err)
	}

	userState := common.UserStates[userID]

	if userState.State == common.InCommand || userState.State == common.InFill {
		_, err = bot.Bot.Send(
			tgbotapi.NewMessage(message.Chat.ID, "You're already executing some command"),
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	user.TimerEnabled = start
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}
}

func TestNotifyCommand(message *tgbotapi.Message) {
	tgUser := message.From
	if tgUser == nil {
		return
	}

	userID := common.UserID(tgUser.ID)

	user, err := db.GetUserByID(userID)
	if err != nil {
		log.Fatal(err)
	}

	userState := common.UserStates[userID]

	if userState.State == common.InCommand || userState.State == common.InFill {
		_, err = bot.Bot.Send(
			tgbotapi.NewMessage(message.Chat.ID, "You're already executing some command"),
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	notifyUser(*user)
}
