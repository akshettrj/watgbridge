package telegram

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"watgbridge/state"
	"watgbridge/telegram/middlewares"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func NewTelegramClient() error {
	cfg := state.State.Config

	bot, err := gotgbot.NewBot(cfg.Telegram.BotToken, &gotgbot.BotOpts{
		Client: http.Client{},
		DefaultRequestOpts: &gotgbot.RequestOpts{
			APIURL: cfg.Telegram.APIURL,
		},
	})
	if err != nil {
		return fmt.Errorf("Could not initialize telegram bot : %s", err)
	}
	state.State.TelegramBot = bot

	bot.UseMiddleware(middlewares.ParseAsHTML)
	bot.UseMiddleware(middlewares.SendWithoutReply)

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		ErrorLog:    log.Default(),
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	updater := ext.NewUpdater(&ext.UpdaterOpts{
		ErrorLog:   log.Default(),
		Dispatcher: dispatcher,
	})

	state.State.TelegramUpdater = updater
	state.State.TelegramDispatcher = dispatcher

	err = updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Second,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("[telegram] failed to start polling : %s", err)
	}

	return nil
}
