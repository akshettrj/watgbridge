package state

import (
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.mau.fi/whatsmeow"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const WATGBRIDGE_VERSION = "1.7.1"

type state struct {
	Config   *Config
	Database *gorm.DB
	Logger   *zap.Logger

	TelegramBot        *gotgbot.Bot
	TelegramDispatcher *ext.Dispatcher
	TelegramUpdater    *ext.Updater
	TelegramCommands   []gotgbot.BotCommand

	WhatsAppClient *whatsmeow.Client

	Modules []string

	StartTime     time.Time
	LocalLocation *time.Location
}

var State state

func init() {
	State.Config = &Config{Path: "config.yaml"}
}
