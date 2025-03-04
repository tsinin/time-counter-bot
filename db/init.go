package db

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// GormDB — глобальный объект для работы с базой через GORM.
var GormDB *gorm.DB

// InitDB инициализирует подключение к PostgreSQL и выполняет миграции.
func InitDB() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://bot:secret@localhost:5432/botdb?sslmode=disable"
	}

	var err error
	GormDB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("PostgreSQL connection error:", err)
	}

	fmt.Println("✅ Successfully connected to PostgreSQL via GORM")

	// Автоматически создаем/обновляем таблицы для моделей.
	err = GormDB.AutoMigrate(&Activity{}, &ActivityLog{}, &User{})
	if err != nil {
		log.Fatal("Migration error:", err)
	}
}
