package routes

import (
	"log"
	"time"

	"TimeCounterBot/db"
)

// UserLocalWallClock переводит момент времени в «локальные часы» пользователя по его смещению от UTC.
func UserLocalWallClock(user db.User, utc time.Time) time.Time {
	sec := int(user.TimezoneOffset) * 3600
	loc := time.FixedZone("user_tz", sec)
	return utc.In(loc)
}

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
	nowUTC := now.UTC()
	localNow := UserLocalWallClock(user, nowUTC)
	if !isTimeInInterval(localNow, startHour, finishHour) {
		return
	}

	if user.LastNotify.Valid && now.Sub(user.LastNotify.Time) < time.Minute*time.Duration(user.TimerMinutes.Int64) {
		return
	}

	go notifyUser(user)
	nextUTC := nowUTC.Add(time.Minute * time.Duration(user.TimerMinutes.Int64))
	nextLocal := UserLocalWallClock(user, nextUTC)
	if !isTimeInInterval(nextLocal, startHour, finishHour) {
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
