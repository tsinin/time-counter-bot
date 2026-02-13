package db

import (
	"database/sql"
	"time"

	"TimeCounterBot/common"
)

// Activity — модель для таблицы activities.
type Activity struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	UserID           int64  `gorm:"not null;index"`
	Name             string `gorm:"not null"`
	ParentActivityID int64  `gorm:"not null;index"`
	IsLeaf           bool   `gorm:"not null"`
	IsMuted          bool   `gorm:"default:false;not null"`
	HasMutedLeaves   bool   `gorm:"default:false;not null"`
}

// ActivityLog — модель для таблицы activity_logs.
// Обратите внимание, что первичный ключ составной: (message_id, user_id).
type ActivityLog struct {
	MessageID       int64     `gorm:"primaryKey;autoIncrement:false"`
	UserID          int64     `gorm:"primaryKey;autoIncrement:false"`
	ActivityID      int64     `gorm:"not null"`
	Timestamp       time.Time `gorm:"not null"`
	IntervalMinutes int64     `gorm:"not null"`
}

// User — модель для таблицы users.
type User struct {
	ID                        common.UserID `gorm:"primaryKey"`
	ChatID                    common.ChatID
	TimerEnabled              bool `gorm:"not null"`
	TimerMinutes              sql.NullInt64
	ScheduleMorningStartHour  sql.NullInt64
	ScheduleEveningFinishHour sql.NullInt64
	LastNotify                sql.NullTime
}

// ActivityRoute — вспомогательная структура для формирования полного пути к листовой активности.
type ActivityRoute struct {
	Name   string
	LeafID int64
}
