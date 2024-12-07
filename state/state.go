package state

import (
	_ "embed"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.mau.fi/whatsmeow"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

//go:embed version.txt
var WATGBRIDGE_VERSION string

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
	WATGBRIDGE_VERSION = strings.TrimSpace(WATGBRIDGE_VERSION)
	State.Config = &Config{Path: "config.yaml"}
}
