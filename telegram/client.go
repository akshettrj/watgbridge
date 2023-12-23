package telegram

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"watgbridge/state"
	"watgbridge/telegram/middlewares"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.uber.org/zap"
)

func NewTelegramClient() error {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
	)
	defer logger.Sync()

	bot, err := gotgbot.NewBot(cfg.Telegram.BotToken, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
			DefaultRequestOpts: &gotgbot.RequestOpts{
				APIURL:  cfg.Telegram.APIURL,
				Timeout: time.Duration(math.MaxInt64),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Could not initialize telegram bot : %s", err)
	}
	state.State.TelegramBot = bot

	bot.UseMiddleware(middlewares.AutoHandleRateLimit)
	bot.UseMiddleware(middlewares.ParseAsHTML)
	bot.UseMiddleware(middlewares.DisableWebPagePreview)
	bot.UseMiddleware(middlewares.SendWithoutReply)

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		UnhandledErrFunc: func(err error) {
			logger.Error("telegram dispatcher received error",
				zap.Error(err),
			)
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	updater := ext.NewUpdater(dispatcher, &ext.UpdaterOpts{
		UnhandledErrFunc: func(err error) {
			logger.Error("telegram updater received error",
				zap.Error(err),
			)
		},
	})

	state.State.TelegramUpdater = updater
	state.State.TelegramDispatcher = dispatcher

	err = updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Second,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("telegram failed to start polling : %s", err)
	}

	logger.Info("successfully logged into telegram",
		zap.Int64("id", bot.Id),
		zap.String("name", bot.FirstName),
		zap.String("username", "@"+bot.Username),
		zap.String("api_url", cfg.Telegram.APIURL),
	)

	return nil
}
