package router

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"TimeCounterBot/common"
	"TimeCounterBot/db"
	"TimeCounterBot/routes"
	"TimeCounterBot/tg/bot"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func SetCommands() {
	commands := []tgbotapi.BotCommand{
		{
			Command:     "start",
			Description: "Начать работу с ботом",
		},
		{
			Command:     "register_new_activity",
			Description: "Зарегистрировать новую активность",
		},
		{
			Command:     "start_notify",
			Description: "Начать присылать уведомления",
		},
		{
			Command:     "stop_notify",
			Description: "Закончить присылать уведомления",
		},
		{
			Command:     "mute_activity",
			Description: "Замьютить активность (чтобы не отображалась в регулярных опросах)",
		},
		{
			Command:     "unmute_activity",
			Description: "Размьютить активность (чтобы снова появилась в регулярных опросах)",
		},
		{
			Command:     "get_day_statistics",
			Description: "Получить статистику за определённый период времени",
		},
		{
			Command:     "fill",
			Description: "Заполнить пропущенные слоты текстом или голосом (OpenAI)",
		},
		{
			Command:     "fill_cancel",
			Description: "Отменить режим /fill",
		},
	}

	setCmd := tgbotapi.NewSetMyCommands(commands...)
	_, err := bot.Bot.Request(setCmd)
	if err != nil {
		log.Printf("Ошибка установки команд: %v", err)
	}
}

func ReceiveUpdates(ctx context.Context, updates tgbotapi.UpdatesChannel) {
	for {
		select {
		// stop looping if ctx is cancelled
		case <-ctx.Done():
			return
		// receive update from channel and then handle it
		case update := <-updates:
			go handleUpdate(update)
		}
	}
}

func handleUpdate(update tgbotapi.Update) {
	switch {
	// Handle messages
	case update.Message != nil:
		handleMessage(update.Message)

	case update.CallbackQuery != nil:
		handleCallbackQuery(update.CallbackQuery)
	}
}

func handleMessage(message *tgbotapi.Message) {
	user := message.From
	if user == nil {
		return
	}

	userID := common.UserID(user.ID)

	maybeAddNewUser(userID, common.ChatID(message.Chat.ID))

	// Print to console
	log.Printf("%s wrote %s", user.UserName, message.Text)

	if message.Voice != nil {
		if common.UserStates[userID].State == common.InFill {
			routes.HandleFillVoice(message)
		}
		return
	}

	if strings.HasPrefix(message.Text, "/") {
		handleCommand(message)
	} else if len(message.Text) > 0 {
		if common.UserStates[userID].State == common.InFill {
			routes.HandleFillText(message)
			return
		}
		// chech user state and send info to waiting channel
		if common.UserStates[userID].WaitingChannel != nil {
			*common.UserStates[userID].WaitingChannel <- message.Text
		}
	}
}

type CallbackHandler func(*tgbotapi.CallbackQuery)

var callbackHandlers = map[string]CallbackHandler{
	"activity_log":          routes.LogUserActivityCallback,
	"register_new_activity": routes.AddNewActivityCallback,
	"refresh_activities":    routes.RefreshActivitiesCallback,

	"day_stats__send_chart":    routes.SendDayStatsRoutineCallback,
	"day_stats__refresh_chart": routes.RefreshDayStatsChartCallback,

	"start__set_timer_minutes":            routes.SetTimerMinutesCallback,
	"start__set_timezone_offset":          routes.SetTimezoneOffsetCallback,
	"start__schedule_morning_start_hour":  routes.SetScheduleMorningStartHourCallback,
	"start__schedule_evening_finish_hour": routes.SetScheduleEveningFinishHourCallback,
	"start__enable_notifications": func(c *tgbotapi.CallbackQuery) {
		routes.EnableNotificationsCallback(c, true)
	},
	"start__disable_notifications": func(c *tgbotapi.CallbackQuery) {
		routes.EnableNotificationsCallback(c, false)
	},

	"mute_activity__mute":    func(c *tgbotapi.CallbackQuery) { routes.MuteActivityCallback(c, true) },
	"mute_activity__cancel":  routes.MuteActivityCancelCallback,
	"mute_activity__refresh": func(c *tgbotapi.CallbackQuery) { routes.MuteActivityRefreshCallback(c, true) },

	"unmute_activity__unmute":  func(c *tgbotapi.CallbackQuery) { routes.MuteActivityCallback(c, false) },
	"unmute_activity__cancel":  routes.MuteActivityCancelCallback,
	"unmute_activity__refresh": func(c *tgbotapi.CallbackQuery) { routes.MuteActivityRefreshCallback(c, false) },
}

func handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	dataPath := strings.Split(callback.Data, " ")[0]
	if strings.HasPrefix(dataPath, "fill__") {
		routes.HandleFillCallback(callback)
		return
	}
	if handler, ok := callbackHandlers[dataPath]; ok {
		handler(callback)
	} else {
		log.Printf("Unknown callback: %q", dataPath)
	}
}

func commandRoot(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.Split(fields[0], "@")[0]
}

// When we get a command, we react accordingly.
func handleCommand(message *tgbotapi.Message) {
	switch commandRoot(message.Text) {
	case "/start":
		routes.StartCommand(message)

	case "/start_notify":
		routes.NotifyCommand(message, true)

	case "/stop_notify":
		routes.NotifyCommand(message, false)

	case "/test_notify":
		routes.TestNotifyCommand(message)

	case "/register_new_activity":
		routes.RegisterNewActivityCommand(message)

	case "/get_day_statistics":
		routes.GetDayStatisticsCommand(message)

	case "/test_day_stats_routine":
		routes.TestDayStatsRoutine(message)

	case "/mute_activity":
		routes.MuteActivityCommand(message, true)

	case "/unmute_activity":
		routes.MuteActivityCommand(message, false)

	case "/fill":
		routes.FillCommand(message)

	case "/fill_cancel":
		routes.FillCancelCommand(message)
	}
}

func maybeAddNewUser(userID common.UserID, chatID common.ChatID) {
	_, err := db.GetUserByID(userID)
	if err != nil && !strings.Contains(err.Error(), "record not found") {
		log.Fatal(err)
	}

	if err != nil && strings.Contains(err.Error(), "record not found") {
		err = db.AddUser(
			db.User{
				ID:                        userID,
				ChatID:                    chatID,
				TimerEnabled:              false,
				TimerMinutes:              sql.NullInt64{},
				ScheduleMorningStartHour:  sql.NullInt64{},
				ScheduleEveningFinishHour: sql.NullInt64{},
				LastNotify:                sql.NullTime{},
			},
		)
		if err != nil {
			log.Fatal(err)
		}
	}
}
