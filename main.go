package main

import (
	"context"
	"log"
	"os"

	"TimeCounterBot/db"
	"TimeCounterBot/routes"
	"TimeCounterBot/tg/bot"
	"TimeCounterBot/tg/router"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	db.InitDB()

	var err error

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("Telegram token was not found")
	}

	bot.Bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		// Abort if something is wrong
		log.Panic(err)
	}

	// Set this to true to log all interactions with telegram servers
	bot.Bot.Debug = false

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	// Create a new cancellable background context. Calling `cancel()` leads to the cancellation of the context
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// `updates` is a golang channel which receives telegram updates
	updates := bot.Bot.GetUpdatesChan(updateConfig)

	// Pass cancellable context to goroutine
	go router.ReceiveUpdates(ctx, updates)
	go routes.DispatchNotifications()

	// Tell the user the bot is online
	log.Println("Start listening for updates. Press enter to stop")

	select {}

	cancel()
}
