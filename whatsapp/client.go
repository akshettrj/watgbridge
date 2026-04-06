package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"sync/atomic"

	"watgbridge/crypto/sqlitekey"
	"watgbridge/state"

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

var qrReconnectRunning int32

// ErrReconnectInProgress is returned when a Reconnect is already running.
var ErrReconnectInProgress = errors.New("reconnect already in progress")

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

	didFreshQRLogin := false
	if client.Store.ID == nil {
		didFreshQRLogin = true
		resetWhatsAppQRLoginMessageState()
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp for login : %s", err)
		}
		runQRCodeLoop(context.Background(), client, logger)
		if client.Store.ID == nil {
			OnWhatsAppQRSessionClosed(logger)
		}
	} else {
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp : %s", err)
		}
	}

	if didFreshQRLogin && client.Store.ID != nil {
		notifyWhatsAppLinked(client, logger)
	}

	if client.Store.ID != nil {
		logger.Info("successfully logged into WhatsApp",
			zap.String("push_name", client.Store.PushName),
			zap.String("jid", client.Store.ID.String()),
		)
	} else {
		logger.Debug("WhatsApp session not linked after QR channel closed",
			zap.String("push_name", client.Store.PushName),
		)
	}

	return nil
}

// runQRCodeLoop consumes QR pairing codes until the channel closes (timeout / completion).
func runQRCodeLoop(ctx context.Context, client *whatsmeow.Client, logger *zap.Logger) {
	qrChan, _ := client.GetQRChannel(ctx)
	for evt := range qrChan {
		if evt.Event != "code" {
			continue
		}
		if state.State.TelegramBot == nil {
			logger.Warn("whatsapp qr login: telegram bot not initialized; qr not sent to terminal or logs — ensure Telegram starts before WhatsApp")
			continue
		}
		qrCodePNG, err := qrcode.Encode(evt.Code, qrcode.Highest, 512)
		if err != nil {
			msg := fmt.Sprintf(
				"WhatsApp login QR could not be encoded as PNG. Fix the issue and restart; QR is not printed to logs or terminal.\n<code>%s</code>",
				html.EscapeString(err.Error()),
			)
			if sendErr := sendWhatsAppQRTextToTelegram(msg); sendErr != nil {
				logger.Warn("whatsapp qr error text send failed", zap.Error(sendErr))
			}
			logger.Warn("whatsapp qr png encode failed", zap.Error(err))
			continue
		}
		OnWhatsAppQRCodeReceived(qrCodePNG)
	}
}

// StartWhatsAppQRReconnect disconnects and starts a new pairing session (Reconnect button).
// Applies reconnect rate limit only after Connect succeeds (new pairing window is open).
func StartWhatsAppQRReconnect(logger *zap.Logger) error {
	if !atomic.CompareAndSwapInt32(&qrReconnectRunning, 0, 1) {
		return ErrReconnectInProgress
	}
	defer atomic.StoreInt32(&qrReconnectRunning, 0)

	client := state.State.WhatsAppClient
	if client == nil {
		return fmt.Errorf("WhatsApp client not initialized")
	}
	ctx := context.Background()
	client.Disconnect()
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if client.Store.ID != nil {
		return fmt.Errorf("already logged in")
	}
	resetWhatsAppQRLoginMessageState()
	deleteSessionClosedMessageIfAny()
	applyReconnectCooldown()
	runQRCodeLoop(ctx, client, logger)
	if client.Store.ID != nil {
		notifyWhatsAppLinked(client, logger)
		return nil
	}
	OnWhatsAppQRSessionClosed(logger)
	return nil
}
