package db

import (
	"log"
)

func AddActivityLog(activityLog ActivityLog) {
	database := getPostgreSQLDatabase()

	insertActivitySQL := `INSERT INTO activity_log (message_id, user_id, activity_id, timestamp, interval_minutes) 
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(message_id, user_id)
		DO UPDATE SET activity_id = excluded.activity_id;
	`

	_, err := database.Exec(
		insertActivitySQL, activityLog.MessageID, activityLog.UserID,
		activityLog.ActivityID, activityLog.Timestamp, activityLog.IntervalMinutes,
	)
	if err != nil {
		log.Fatal(err)
	}
}
