package whatsapp

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html"

	"watgbridge/crypto/sqlitekey"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/mutecomm/go-sqlcipher/v4"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type whatsmeowLogger struct {
	logger *zap.SugaredLogger
}

func (wl whatsmeowLogger) Warnf(msg string, args ...interface{}) {
	wl.logger.Warnf(msg, args...)
	_ = wl.logger.Sync()
}
func (wl whatsmeowLogger) Errorf(msg string, args ...interface{}) {
	wl.logger.Errorf(msg, args...)
	_ = wl.logger.Sync()
}
func (wl whatsmeowLogger) Infof(msg string, args ...interface{}) {
	wl.logger.Infof(msg, args...)
	_ = wl.logger.Sync()
}
func (wl whatsmeowLogger) Debugf(msg string, args ...interface{}) {
	wl.logger.Debugf(msg, args...)
	_ = wl.logger.Sync()
}
func (wl whatsmeowLogger) Sub(module string) waLog.Logger {
	return whatsmeowLogger{logger: wl.logger.Named(module)}
}

func NewWhatsAppClient() error {

	var (
		cfg    = state.State.Config
		err    error
		logger *zap.Logger
	)

	if cfg.WhatsApp.WhatsmeowDebugMode {
		developmentConfig := zap.NewDevelopmentConfig()
		developmentConfig.OutputPaths = append(developmentConfig.OutputPaths, "whatsmeow_debug.log")
		logger, err = developmentConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize development loggers for WhatsMeow client: %s", err))
		}
	} else {
		productionConfig := zap.NewProductionConfig()
		logger, err = productionConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize production loggers for WhatsMeow client: %s", err))
		}
	}
	logger = logger.Named("WaTgBridge")
	defer logger.Sync()

	waDatabaseLogger := &whatsmeowLogger{logger: logger.Sugar().Named("WhatsMeow_Database")}
	waClientLogger := &whatsmeowLogger{logger: logger.Sugar().Named("WhatsMeow_Client")}

	store.DeviceProps.Os = proto.String(state.State.Config.WhatsApp.SessionName)
	store.DeviceProps.RequireFullSync = proto.Bool(false)
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{
		FullSyncDaysLimit:              proto.Uint32(0),
		FullSyncSizeMbLimit:            proto.Uint32(0),
		StorageQuotaMb:                 proto.Uint32(0),
		RecentSyncDaysLimit:            proto.Uint32(0),
		SupportCallLogHistory:          proto.Bool(false),
		SupportBotUserAgentChatHistory: proto.Bool(false),
		SupportCagReactionsAndPolls:    proto.Bool(false),
	}

	var container *sqlstore.Container
	if cfg.WhatsApp.LoginDatabase.Type == "sqlite3" {
		sqlDB, err := sql.Open("sqlite3", cfg.WhatsApp.LoginDatabase.URL)
		if err != nil {
			return fmt.Errorf("could not open whatsapp sqlite: %w", err)
		}
		if hexKey, ok := sqlitekey.DerivedHexFromEnv(); ok {
			if err := sqlitekey.ApplyToDB(sqlDB, hexKey); err != nil {
				_ = sqlDB.Close()
				return fmt.Errorf("whatsapp sqlcipher: %w", err)
			}
		}
		container = sqlstore.NewWithDB(sqlDB, "sqlite3", waDatabaseLogger)
		if err := container.Upgrade(context.Background()); err != nil {
			return fmt.Errorf("could not upgrade whatsapp sqlstore: %w", err)
		}
	} else {
		container, err = sqlstore.New(context.Background(), cfg.WhatsApp.LoginDatabase.Type,
			cfg.WhatsApp.LoginDatabase.URL, waDatabaseLogger)
		if err != nil {
			return fmt.Errorf("could not initialize sqlstore for Whatsapp : %s", err)
		}
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("could not initialize device store for Whatsapp : %s", err)
	}

	client := whatsmeow.NewClient(deviceStore, waClientLogger)
	state.State.WhatsAppClient = client

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp for login : %s", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// var png []byte
				// png, _err := qrcode.Encode("aklsdfjasdfaklsdfjlasdfjaskldfjasldfjaklsdfjals", qrcode.Highest, 256)
				// if _err != nil {
				// 	panic(_err)
				// }

				if state.State.TelegramBot != nil {
					qrCodePNG, err := qrcode.Encode(evt.Code, qrcode.Highest, 512)
					if err != nil {
						_, _ = state.State.TelegramBot.SendMessage(
							state.State.Config.Telegram.OwnerID,
							fmt.Sprintf(
								"WhatsApp login QR could not be encoded as PNG. Fix the issue and restart; QR is not printed to logs or terminal.\n<code>%s</code>",
								html.EscapeString(err.Error()),
							),
							&gotgbot.SendMessageOpts{},
						)
						logger.Warn("whatsapp qr png encode failed", zap.Error(err))
					} else {
						_, _ = state.State.TelegramBot.SendPhoto(
							state.State.Config.Telegram.OwnerID,
							gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrCodePNG)),
							&gotgbot.SendPhotoOpts{
								Caption: "Scan the above QR code to login to WhatsApp.",
							},
						)
					}
				} else {
					logger.Warn("whatsapp qr login: telegram bot not initialized; qr not sent to terminal or logs — ensure Telegram starts before WhatsApp")
				}
			} else {
				logger.Info("received WhatsApp login event",
					zap.Any("event", evt.Event),
				)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp : %s", err)
		}
	}

	logger.Info("successfully logged into WhatsApp",
		zap.String("push_name", client.Store.PushName),
		zap.String("jid", client.Store.ID.String()),
	)

	return nil
}
