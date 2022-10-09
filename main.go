package main

import (
	"log"
	"os"

	"wa-tg-bridge/database"
	"wa-tg-bridge/state"
	"wa-tg-bridge/telegram"
	"wa-tg-bridge/whatsapp"
)

func main() {
	// Load config file
	cfg := state.State.Config
	if len(os.Args) > 1 {
		cfg.Path = os.Args[1]
	} else {
		cfg.Path = "config.yaml"
	}
	cfg.LoadConfig()

	if cfg.TimeZone == "" {
		cfg.TimeZone = "Asia/Kolkata"
	}

	db, err := database.Connect()
	if err != nil {
		log.Fatalln("Couldn't connect to the database : " + err.Error())
	}
	state.State.Database = db

	err = telegram.NewClient()
	if err != nil {
		panic(err)
	}
	log.Printf(
		"[telegram] logged in as : %s [ @%s ]\n",
		state.State.TelegramBot.FirstName,
		state.State.TelegramBot.Username,
	)

	err = whatsapp.NewClient()
	if err != nil {
		panic(err)
	}
	log.Printf(
		"[whatsapp] logged in as : %s [ %s ]\n",
		state.State.WhatsAppClient.Store.PushName,
		state.State.WhatsAppClient.Store.ID.User,
	)

	waClient := state.State.WhatsAppClient
	waClient.AddEventHandler(whatsapp.WhatsAppEventHandler)

	state.State.TelegramUpdater.Idle()
}
