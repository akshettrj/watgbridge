package state

import (
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"
)

const WATGBRIDGE_VERSION = "1.0.0"

type state struct {
	Config   *Config
	Database *gorm.DB

	TelegramBot        *gotgbot.Bot
	TelegramDispatcher *ext.Dispatcher
	TelegramUpdater    *ext.Updater
	TelegramCommands   []gotgbot.BotCommand

	WhatsAppClient *whatsmeow.Client

	StartTime     time.Time
	LocalLocation *time.Location
}

var State state

func init() {
	State.Config = &Config{}
}
