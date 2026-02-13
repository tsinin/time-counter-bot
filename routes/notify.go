package routes

import (
	"database/sql"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"TimeCounterBot/common"
	"TimeCounterBot/db"
	tg "TimeCounterBot/tg/bot"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// notifyUser -> sends message M and creates keybord Ki with first-level activities
// user sends callback on K1 [activity_log node_id timestamp]
// LogUserActivityCallback gets callback, switch:
//  node_id is a leaf -> logs leaf-activity, deletes Ki
//  node_id is not a leaf -> load all children of node_id, creates new Keyboard Ki+1

func notifyUser(user db.User) {
	user.LastNotify = sql.NullTime{Time: time.Now(), Valid: true}
	err := db.UpdateUser(user)
	if err != nil {
		log.Fatal(err)
	}

	msgconf := tgbotapi.NewMessage(int64(user.ChatID), "Чё делаеш?))0)")
	isMuted := false
	msgconf.ReplyMarkup = buildActivitiesKeyboardMarkupForUser(
		user, -1, &isMuted, nil, "activity_log", getStandardActivitiesLastRow())

	_, err = tg.Bot.Send(msgconf)
	if err != nil {
		if tg.IsBotBlockedError(err) {
			log.Printf("Bot was blocked by user %d, disabling notifications", user.ID)
			user.TimerEnabled = false
			if updateErr := db.UpdateUser(user); updateErr != nil {
				log.Printf("Failed to disable timer for blocked user %d: %v", user.ID, updateErr)
			}
			return
		}
		log.Fatal(err)
	}
}

func LogUserActivityCallback(callback *tgbotapi.CallbackQuery) {
	var nodeID int64

	var timerMinutes int64

	_, err := fmt.Sscanf(callback.Data, "activity_log %d %d", &nodeID, &timerMinutes)
	if err != nil {
		log.Fatal(err)
	}

	isMuted := false
	activities, err := db.GetSimpleActivities(common.UserID(callback.From.ID), &isMuted, nil)
	if err != nil {
		log.Fatal(err)
	}

	idx := slices.IndexFunc(activities, func(a db.Activity) bool { return a.ID == nodeID })
	if idx == -1 {
		log.Fatalf("activity with id %d was not found in user activities.", nodeID)
	}

	if activities[idx].IsLeaf {
		err = db.AddActivityLog(
			db.ActivityLog{
				MessageID:       int64(callback.Message.MessageID),
				UserID:          callback.From.ID,
				ActivityID:      nodeID,
				Timestamp:       callback.Message.Time(),
				IntervalMinutes: timerMinutes,
			},
		)
		if err != nil {
			log.Fatal(err)
		}

		activityName, err := db.BuildFullActivityName(activities, nodeID)
		if err != nil {
			log.Fatal(err)
		}

		_, err = tg.Bot.Send(
			tgbotapi.NewEditMessageTextAndMarkup(
				callback.Message.Chat.ID, callback.Message.MessageID,
				"Saved activity \""+activityName+"\"",
				tgbotapi.InlineKeyboardMarkup{InlineKeyboard: make([][]tgbotapi.InlineKeyboardButton, 0)},
			),
		)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		keyboard := buildActivitiesKeyboardMarkup(
			activities, timerMinutes, nodeID, "activity_log", getStandardActivitiesLastRow())

		_, err = tg.Bot.Send(
			tgbotapi.NewEditMessageTextAndMarkup(
				callback.Message.Chat.ID, callback.Message.MessageID, callback.Message.Text, keyboard,
			),
		)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func RefreshActivitiesCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}

	isMuted := false
	keyboard := buildActivitiesKeyboardMarkupForUser(
		*user, -1, &isMuted, nil, "activity_log", getStandardActivitiesLastRow())

	_, err = tg.Bot.Send(
		tgbotapi.NewEditMessageTextAndMarkup(
			callback.Message.Chat.ID, callback.Message.MessageID, callback.Message.Text, keyboard,
		),
	)
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		log.Fatal(err)
	}
}

func AddNewActivityCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}

	registerNewActivity(*user)
}

func buildActivitiesKeyboardMarkup(
	activities []db.Activity, timerMinutes int64, parentActivityID int64,
	callbackCommand string, lastRow []tgbotapi.InlineKeyboardButton) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)

	for _, activity := range activities {
		if activity.ParentActivityID != parentActivityID {
			continue
		}

		leafIDStr := fmt.Sprintf(
			"%s %d %d", callbackCommand, activity.ID, timerMinutes,
		)
		buttons := make([]tgbotapi.InlineKeyboardButton, 0)
		buttons = append(
			buttons,
			tgbotapi.InlineKeyboardButton{
				Text:         activity.Name,
				CallbackData: &leafIDStr,
			},
		)
		rows = append(rows, buttons)
	}

	rows = append(rows, lastRow)

	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buildActivitiesKeyboardMarkupForUser(
	user db.User, parentActivityID int64, isMuted *bool, hasMutedLeaves *bool,
	callbackCommand string, lastRow []tgbotapi.InlineKeyboardButton) tgbotapi.InlineKeyboardMarkup {
	activities, err := db.GetSimpleActivities(user.ID, isMuted, hasMutedLeaves)
	if err != nil {
		log.Fatal(err)
	}

	return buildActivitiesKeyboardMarkup(activities, user.TimerMinutes.Int64, parentActivityID, callbackCommand, lastRow)
}

func getStandardActivitiesLastRow() []tgbotapi.InlineKeyboardButton {
	newActivityCallbackText := "register_new_activity"
	refreshActivitiesCallbackText := "refresh_activities"
	return append(
		make([]tgbotapi.InlineKeyboardButton, 0),
		tgbotapi.InlineKeyboardButton{
			Text:         "Add new activity",
			CallbackData: &newActivityCallbackText,
		},
		tgbotapi.InlineKeyboardButton{
			Text:         "\U0001F504 Refresh activities",
			CallbackData: &refreshActivitiesCallbackText,
		},
	)
}
