package routes

import (
	"TimeCounterBot/common"
	"TimeCounterBot/db"
	"TimeCounterBot/tg/bot"
	"database/sql"
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func StartCommand(message *tgbotapi.Message) {
	user, err := db.GetUserByID(common.UserID(message.From.ID))
	if err != nil {
		log.Fatal(err)
	}

	msg := tgbotapi.NewMessage(
		int64(user.ChatID),
		"Hi! You are using Andrew's time management bot.\n"+
			"First, choose your timezone as offset from UTC (hours). "+
			"Reminder hours below will be in this local time.",
	)
	msg.ReplyMarkup = getTimezoneOffsetKeyboardMarkup()

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func SetTimezoneOffsetCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}
	var offset int64
	_, err = fmt.Sscanf(callback.Data, "start__set_timezone_offset %d", &offset)
	if err != nil {
		log.Fatal(err)
	}
	if offset < -12 || offset > 12 {
		log.Fatalf("invalid timezone offset %d", offset)
	}
	user.TimezoneOffset = offset
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}

	msg := tgbotapi.NewEditMessageTextAndMarkup(
		int64(user.ChatID),
		callback.Message.MessageID,
		fmt.Sprintf(
			"Timezone: %s.\nNow choose how often you want to be asked what you're doing (interval between reminders).",
			formatUTCOffsetLabel(offset),
		),
		getStartCommandTimerIntervalsKeyboardMarkup(),
	)

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func SetTimerMinutesCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}
	var timerMinutes int64
	_, err = fmt.Sscanf(callback.Data, "start__set_timer_minutes %d", &timerMinutes)
	if err != nil {
		log.Fatal(err)
	}
	user.TimerMinutes = sql.NullInt64{Int64: timerMinutes, Valid: true}
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}

	msg := tgbotapi.NewEditMessageTextAndMarkup(
		int64(user.ChatID),
		callback.Message.MessageID,
		fmt.Sprintf(
			"Nice, your interval is %d minutes!\n"+
				"Now choose the hour (%s) when reminders should start each day.",
			timerMinutes,
			formatUTCOffsetLabel(user.TimezoneOffset),
		),
		getScheduleMorningStartHourKeyboardMarkup(),
	)

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func SetScheduleMorningStartHourCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}
	var scheduleMorningStartHour int64
	_, err = fmt.Sscanf(callback.Data, "start__schedule_morning_start_hour %d", &scheduleMorningStartHour)
	if err != nil {
		log.Fatal(err)
	}
	user.ScheduleMorningStartHour = sql.NullInt64{Int64: scheduleMorningStartHour, Valid: true}
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}

	msg := tgbotapi.NewEditMessageTextAndMarkup(
		int64(user.ChatID),
		callback.Message.MessageID,
		fmt.Sprintf(
			"Start of day: %02d:00 (%s).\n"+
				"Now choose the hour (%s) when reminders stop and day stats are offered.",
			scheduleMorningStartHour,
			formatUTCOffsetLabel(user.TimezoneOffset),
			formatUTCOffsetLabel(user.TimezoneOffset),
		),
		getScheduleEveningFinishHourKeyboardMarkup(),
	)

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func SetScheduleEveningFinishHourCallback(callback *tgbotapi.CallbackQuery) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}
	var scheduleEveningFinishHour int64
	_, err = fmt.Sscanf(callback.Data, "start__schedule_evening_finish_hour %d", &scheduleEveningFinishHour)
	if err != nil {
		log.Fatal(err)
	}
	user.ScheduleEveningFinishHour = sql.NullInt64{Int64: scheduleEveningFinishHour, Valid: true}
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}

	var text string
	var keyboardMarkup tgbotapi.InlineKeyboardMarkup
	if user.TimerEnabled {
		text = "You will get notifications every %d minutes, from %02d:00 to %02d:00 (%s).\n" +
			"Notifications enabled! You can disable them by pressing button below."
		keyboardMarkup = getDisableNotificationsKeyboardMarkup()
	} else {
		text = "Cool. You will get notifications every %d minutes, from %02d:00 to %02d:00 (%s).\n" +
			"Now click the button to enable notifications."
		keyboardMarkup = getEnableNotificationsKeyboardMarkup()
	}

	msg := tgbotapi.NewEditMessageTextAndMarkup(
		int64(user.ChatID),
		callback.Message.MessageID,
		fmt.Sprintf(
			text,
			user.TimerMinutes.Int64,
			user.ScheduleMorningStartHour.Int64,
			user.ScheduleEveningFinishHour.Int64,
			formatUTCOffsetLabel(user.TimezoneOffset),
		),
		keyboardMarkup,
	)

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func formatUTCOffsetLabel(offset int64) string {
	if offset >= 0 {
		return fmt.Sprintf("UTC+%d", offset)
	}
	return fmt.Sprintf("UTC%d", offset)
}

func EnableNotificationsCallback(callback *tgbotapi.CallbackQuery, enable bool) {
	user, err := db.GetUserByID(common.UserID(callback.From.ID))
	if err != nil {
		log.Fatal(err)
	}
	user.TimerEnabled = enable
	err = db.UpdateUser(*user)
	if err != nil {
		log.Fatal(err)
	}

	message := fmt.Sprintf(
		"You will get notifications every %d minutes, from %02d:00 to %02d:00 (%s).\n",
		user.TimerMinutes.Int64,
		user.ScheduleMorningStartHour.Int64,
		user.ScheduleEveningFinishHour.Int64,
		formatUTCOffsetLabel(user.TimezoneOffset),
	)
	if enable {
		message += "Notifications enabled!"
	} else {
		message += "Notifications disabled!"
	}

	var keyboardMarkup tgbotapi.InlineKeyboardMarkup
	if enable {
		keyboardMarkup = getDisableNotificationsKeyboardMarkup()
	} else {
		keyboardMarkup = getEnableNotificationsKeyboardMarkup()
	}

	msg := tgbotapi.NewEditMessageTextAndMarkup(
		int64(user.ChatID),
		callback.Message.MessageID,
		message,
		keyboardMarkup,
	)

	_, err = bot.Bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}
}

func getTimezoneOffsetKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for o := int64(-8); o <= 8; o++ {
		label := formatUTCOffsetLabel(o)
		cb := fmt.Sprintf("start__set_timezone_offset %d", o)
		row = append(row, tgbotapi.InlineKeyboardButton{Text: label, CallbackData: StringPtr(cb)})
		if len(row) >= 4 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func getStartCommandTimerIntervalsKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		append(
			make([]tgbotapi.InlineKeyboardButton, 0),
			tgbotapi.InlineKeyboardButton{Text: "10 minutes", CallbackData: StringPtr("start__set_timer_minutes 10")},
			tgbotapi.InlineKeyboardButton{Text: "20 minutes", CallbackData: StringPtr("start__set_timer_minutes 20")},
		),
		append(
			make([]tgbotapi.InlineKeyboardButton, 0),
			tgbotapi.InlineKeyboardButton{Text: "30 minutes", CallbackData: StringPtr("start__set_timer_minutes 30")},
			tgbotapi.InlineKeyboardButton{Text: "1 hour", CallbackData: StringPtr("start__set_timer_minutes 60")},
		),
	)
}

func createTimeKeyboardButtons(startHour, endHour int, callbackPrefix string) [][]tgbotapi.InlineKeyboardButton {
	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton

	for hour := startHour; hour <= endHour; hour++ {
		button := tgbotapi.InlineKeyboardButton{
			Text:         fmt.Sprintf("%02d:00", hour),
			CallbackData: StringPtr(fmt.Sprintf("%s %d", callbackPrefix, hour)),
		}
		currentRow = append(currentRow, button)

		if len(currentRow) == 4 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}

	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	return rows
}

func getScheduleMorningStartHourKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		createTimeKeyboardButtons(6, 13, "start__schedule_morning_start_hour")...)
}

func getScheduleEveningFinishHourKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	rows := createTimeKeyboardButtons(21, 23, "start__schedule_evening_finish_hour")
	rows = append(rows, createTimeKeyboardButtons(0, 3, "start__schedule_evening_finish_hour")...)
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func getEnableNotificationsKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		append(
			make([]tgbotapi.InlineKeyboardButton, 0),
			tgbotapi.InlineKeyboardButton{Text: "Enable notifications!", CallbackData: StringPtr("start__enable_notifications")},
		),
	)
}

func getDisableNotificationsKeyboardMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		append(
			make([]tgbotapi.InlineKeyboardButton, 0),
			tgbotapi.InlineKeyboardButton{
				Text:         "Disable notifications!",
				CallbackData: StringPtr("start__disable_notifications"),
			},
		),
	)
}

func StringPtr(value string) *string {
	return &value
}
