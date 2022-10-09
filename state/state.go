package state

import (
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"
)

type state struct {
	Config   *Config
	Database *gorm.DB

	TelegramBot        *gotgbot.Bot
	TelegramDispatcher *ext.Dispatcher
	TelegramUpdater    *ext.Updater
	TelegramCommands   []gotgbot.BotCommand

	WhatsAppClient *whatsmeow.Client

	StartTime time.Time
}

var State state

func init() {
	State.Config = &Config{}
}
