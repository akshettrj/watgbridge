package telegram

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"watgbridge/state"
	mw "watgbridge/telegram/middleware"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func NewClient() error {
	cfg := state.State.Config

	bot, err := gotgbot.NewBot(
		cfg.Telegram.BotToken,
		&gotgbot.BotOpts{
			Client: http.Client{},
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: gotgbot.DefaultTimeout,
				APIURL:  cfg.Telegram.ApiURL,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("Could not initialize bot : %s", err)
	}
	state.State.TelegramBot = bot

	bot.UseMiddleware(mw.ParseAsHTML)
	bot.UseMiddleware(mw.SendWithoutReply)

	updater := ext.NewUpdater(&ext.UpdaterOpts{
		ErrorLog: nil,
		DispatcherOpts: ext.DispatcherOpts{
			Error: func(_ *gotgbot.Bot, _ *ext.Context, err error) ext.DispatcherAction {
				log.Println("[telegram] An error occurred while handling update : " + err.Error())
				return ext.DispatcherActionNoop
			},
			MaxRoutines: ext.DefaultMaxRoutines,
		},
	})
	state.State.TelegramUpdater = &updater

	dispatcher := updater.Dispatcher
	state.State.TelegramDispatcher = dispatcher

	err = updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start polling : %s", err)
	}

	return nil
}
