package main

import (
	"log"
	"os"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/telegram"
	"watgbridge/utils"
	"watgbridge/whatsapp"
)

func main() {
	// Load configuration file
	cfg := state.State.Config
	if len(os.Args) > 1 {
		cfg.Path = os.Args[1]
	} else {
		cfg.Path = "config.yaml"
	}
	err := cfg.LoadConfig()
	if err != nil {
		log.Fatalln(err)
	}

	// Create local location for time
	if cfg.TimeZone == "" {
		cfg.TimeZone = "UTC"
	}
	locLoc, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		log.Fatalln("could not set timezone to '" + cfg.TimeZone + "': " + err.Error())
	}
	state.State.LocalLocation = locLoc

	// Setup database
	db, err := database.Connect()
	if err != nil {
		log.Fatalln("could not connect to database : " + err.Error())
	}
	state.State.Database = db
	err = database.AutoMigrate()
	if err != nil {
		log.Fatalln("could not migrate database tables : " + err.Error())
	}

	err = telegram.NewTelegramClient()
	if err != nil {
		panic(err)
	}
	log.Printf("[telegram] logged in as : %s [ @%s ]\n",
		state.State.TelegramBot.FirstName, state.State.TelegramBot.Username)
	telegram.AddTelegramHandlers()
	utils.TgRegisterBotCommands(state.State.TelegramBot, state.State.TelegramCommands...)

	err = whatsapp.NewWhatsAppClient()
	if err != nil {
		panic(err)
	}
	log.Printf("[whatsapp] logged in as : %s [ @%s ]\n",
		state.State.WhatsAppClient.Store.PushName, state.State.WhatsAppClient.Store.ID.User)
	state.State.WhatsAppClient.AddEventHandler(whatsapp.WhatsAppEventHandler)

	state.State.StartTime = time.Now().UTC()

	state.State.TelegramUpdater.Idle()
}
