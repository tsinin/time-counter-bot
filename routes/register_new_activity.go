package routes

import (
	"log"

	"TimeCounterBot/common"
	"TimeCounterBot/db"
	"TimeCounterBot/tg/bot"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func RegisterNewActivityCommand(message *tgbotapi.Message) {
	tgUser := message.From
	if tgUser == nil {
		return
	}

	user, err := db.GetUserByID(common.UserID(tgUser.ID))
	if err != nil {
		log.Fatal(err)
	}

	registerNewActivity(*user)
}

func registerNewActivity(user db.User) {
	userState := common.UserStates[user.ID]

	if userState.State == common.InCommand || userState.State == common.InFill {
		_, err := bot.Bot.Send(
			tgbotapi.NewMessage(int64(user.ChatID), "You're already executing some command"),
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	userState.State = common.InCommand
	waitChan := make(chan string)
	common.UserStates[user.ID] = common.UserState{State: common.InCommand, WaitingChannel: &waitChan}

	reply := tgbotapi.NewMessage(int64(user.ChatID), "Write new activity")
	forceReply := tgbotapi.ForceReply{ForceReply: true}
	reply.ReplyMarkup = forceReply

	_, err := bot.Bot.Send(reply)
	if err != nil {
		log.Fatal(err)
	}

	ans := <-waitChan
	err = db.ParseAndAddActivity(user.ID, ans)
	if err != nil {
		log.Fatal(err)
	}

	close(waitChan)

	common.UserStates[user.ID] = common.UserState{State: common.Idle, WaitingChannel: nil}

	reply = tgbotapi.NewMessage(int64(user.ChatID), "New activity \""+ans+"\" added!")

	_, err = bot.Bot.Send(reply)
	if err != nil {
		log.Fatal(err)
	}
}
