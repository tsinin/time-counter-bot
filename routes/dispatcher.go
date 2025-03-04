package routes

import (
	"log"
	"time"

	"TimeCounterBot/db"
)

const DispatchInterval = time.Second * 5

func isTimeInInterval(ts time.Time, startHour, finishHour int64) bool {
	if startHour < finishHour {
		return ts.Hour() >= int(startHour) && ts.Hour() < int(finishHour)
	}
	return ts.Hour() >= int(startHour) || ts.Hour() < int(finishHour)
}

func processUser(user db.User, now time.Time) {
	if !user.TimerEnabled {
		return
	}

	if !user.ScheduleMorningStartHour.Valid || !user.ScheduleEveningFinishHour.Valid || !user.TimerMinutes.Valid {
		log.Fatalf("something is invalid for user %d", user.ID)
	}

	startHour := user.ScheduleMorningStartHour.Int64
	finishHour := user.ScheduleEveningFinishHour.Int64
	if !isTimeInInterval(now, startHour, finishHour) {
		return
	}

	if user.LastNotify.Valid && now.Sub(user.LastNotify.Time) < time.Minute*time.Duration(user.TimerMinutes.Int64) {
		return
	}

	go notifyUser(user)
	if !isTimeInInterval(now.Add(time.Minute*time.Duration(user.TimerMinutes.Int64)), startHour, finishHour) {
		go startDayStatsRoutine(user)
	}
}

func DispatchNotifications() {
	now := time.Now()

	users, err := db.GetUsers()
	if err != nil {
		log.Fatal(err)
	}
	for _, user := range users {
		processUser(user, now)
	}

	time.Sleep(DispatchInterval)

	go DispatchNotifications()
}
