package whatsapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"os"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	waWa6 "go.mau.fi/whatsmeow/proto/waWa6"
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

// WhatsApp client identities the session can register as.
const (
	clientModeAndroid         = "android"
	clientModeAndroidBusiness = "android_business"
)

// sendQRCodeToOwner sends the WhatsApp pairing QR code to the owner's Telegram
// chat as a photo. If the code cannot be rendered as PNG, a text message is sent
// instead so the operator still learns to look at the terminal.
//
// The QR code is always printed locally as well, so a failed Telegram request
// must not abort the login flow: it is returned for the caller to log.
func sendQRCodeToOwner(bot *gotgbot.Bot, ownerID int64, qrCode string) error {
	qrCodePNG, err := qrcode.Encode(qrCode, qrcode.Highest, 512)
	if err != nil {
		_, sendErr := bot.SendMessage(
			ownerID,
			fmt.Sprintf(
				"Please check your terminal and scan the QR code to login to WhatsApp. Failed to encode to PNG and send here:\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{},
		)
		if sendErr != nil {
			return fmt.Errorf("could not encode the QR code as PNG (%v) and failed to send the fallback message : %w", err, sendErr)
		}
		return nil
	}

	_, err = bot.SendPhoto(
		ownerID,
		gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrCodePNG)),
		&gotgbot.SendPhotoOpts{
			Caption: "Scan the above QR code to login to WhatsApp.",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to send the QR code photo : %w", err)
	}

	return nil
}

// maxQRPairingAttempts is how many full QR code rounds are offered before the
// bridge gives up. One round lasts about 160 seconds (a 60 second code plus
// five 20 second codes), so the operator gets about 8 minutes to scan.
const maxQRPairingAttempts = 3

// notifyOwner sends an HTML message to the owner's Telegram chat, if the bot
// is available. Failures are only logged, because the caller is already on an
// error or recovery path.
func notifyOwner(logger *zap.Logger, text string) {
	bot := state.State.TelegramBot
	if bot == nil {
		return
	}
	ownerID := state.State.Config.Telegram.OwnerID
	if _, err := bot.SendMessage(ownerID, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"}); err != nil {
		logger.Error("failed to send a message to the owner on telegram",
			zap.Int64("owner_id", ownerID),
			zap.Error(err),
		)
	}
}

// pairWithQRCode links the client as a new device. When all the QR codes of a
// round expire, it starts a new round, up to maxQRPairingAttempts rounds, so
// the operator does not have to restart the bridge after one missed scan. Any
// other failure (rejected pairing, outdated client, ...) is returned at once.
func pairWithQRCode(client *whatsmeow.Client, logger *zap.Logger) error {
	for attempt := 1; attempt <= maxQRPairingAttempts; attempt++ {
		qrChan, err := client.GetQRChannel(context.Background())
		if err != nil {
			return fmt.Errorf("could not get the QR channel for Whatsapp login : %w", err)
		}

		markBacklogCutoff()
		if err := client.Connect(); err != nil {
			return fmt.Errorf("could not connect to Whatsapp for login : %w", err)
		}

		var lastEvt whatsmeow.QRChannelItem
		for evt := range qrChan {
			lastEvt = evt
			if evt.Event == whatsmeow.QRChannelEventCode {
				if state.State.TelegramBot != nil {
					ownerID := state.State.Config.Telegram.OwnerID
					if err := sendQRCodeToOwner(state.State.TelegramBot, ownerID, evt.Code); err != nil {
						logger.Error("failed to send the QR code on telegram, scan the one printed in the terminal instead",
							zap.Int64("owner_id", ownerID),
							zap.Error(err),
						)
					}
				}
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				logger.Info("received WhatsApp login event",
					zap.Any("event", evt.Event),
				)
			}
		}

		switch {
		case lastEvt.Event == whatsmeow.QRChannelSuccess.Event && client.Store.ID != nil:
			return nil
		case lastEvt.Event == whatsmeow.QRChannelTimeout.Event:
			// whatsmeow already disconnected the client; it is safe to ask
			// for a new QR channel in the next round.
			client.Disconnect()
			if attempt < maxQRPairingAttempts {
				logger.Warn("the WhatsApp QR codes expired, sending new ones",
					zap.Int("attempt", attempt+1),
					zap.Int("max_attempts", maxQRPairingAttempts),
				)
				notifyOwner(logger, fmt.Sprintf(
					"The QR codes expired. New QR codes follow (round %d of %d).",
					attempt+1, maxQRPairingAttempts,
				))
			}
		case lastEvt.Error != nil:
			client.Disconnect()
			return fmt.Errorf("whatsapp pairing failed (%s) : %w", lastEvt.Event, lastEvt.Error)
		default:
			client.Disconnect()
			return fmt.Errorf("whatsapp pairing failed with event %q", lastEvt.Event)
		}
	}

	return fmt.Errorf("could not complete whatsapp login : the QR codes expired %d times without a scan", maxQRPairingAttempts)
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

	// Configure device as Android if client mode is set to android/android_business
	isAndroidEmulation := cfg.WhatsApp.ClientMode == clientModeAndroid ||
		cfg.WhatsApp.ClientMode == clientModeAndroidBusiness

	if isAndroidEmulation {
		store.DeviceProps.Os = proto.String("Android")
		store.DeviceProps.RequireFullSync = proto.Bool(false)
		store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_ANDROID_PHONE.Enum()
		store.DeviceProps.Version = &waCompanionReg.DeviceProps_AppVersion{
			Primary:    proto.Uint32(2),
			Secondary:  proto.Uint32(23),
			Tertiary:   proto.Uint32(9),
			Quaternary: proto.Uint32(0),
		}
		store.DeviceProps.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{
			FullSyncDaysLimit:              proto.Uint32(0),
			FullSyncSizeMbLimit:            proto.Uint32(0),
			StorageQuotaMb:                 proto.Uint32(0),
			RecentSyncDaysLimit:            proto.Uint32(0),
			SupportCallLogHistory:          proto.Bool(false),
			SupportBotUserAgentChatHistory: proto.Bool(false),
			SupportCagReactionsAndPolls:    proto.Bool(false),
		}

		// Configure client payload as Android
		var platform waWa6.ClientPayload_UserAgent_Platform
		if cfg.WhatsApp.ClientMode == clientModeAndroidBusiness {
			platform = waWa6.ClientPayload_UserAgent_SMB_ANDROID
		} else {
			platform = waWa6.ClientPayload_UserAgent_ANDROID
		}
		store.BaseClientPayload.UserAgent.Platform = platform.Enum()
		store.BaseClientPayload.UserAgent.Device = proto.String("SM-G900F")
		store.BaseClientPayload.UserAgent.Manufacturer = proto.String("Samsung")
		store.BaseClientPayload.UserAgent.OsVersion = proto.String("14")
		waVersion := store.GetWAVersion()
		store.BaseClientPayload.UserAgent.AppVersion = &waWa6.ClientPayload_UserAgent_AppVersion{
			Primary:   proto.Uint32(uint32(waVersion[0])),
			Secondary: proto.Uint32(uint32(waVersion[1])),
			Tertiary:  proto.Uint32(uint32(waVersion[2])),
		}
		store.BaseClientPayload.WebInfo = nil // Remove WebInfo as it's only for web

		if cfg.WhatsApp.ClientMode == clientModeAndroidBusiness {
			logger.Info("WhatsApp client configured to emulate Android phone (WhatsApp Business)")
		} else {
			logger.Info("WhatsApp client configured to emulate Android phone")
		}
	} else {
		// Keep default Web configuration
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

		logger.Info("WhatsApp client configured as Web client")
	}

	container, err := sqlstore.New(context.Background(), state.State.Config.WhatsApp.LoginDatabase.Type,
		state.State.Config.WhatsApp.LoginDatabase.URL, waDatabaseLogger)
	if err != nil {
		return fmt.Errorf("could not initialize sqlstore for Whatsapp : %s", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("could not initialize device store for Whatsapp : %s", err)
	}

	client := whatsmeow.NewClient(deviceStore, waClientLogger)
	state.State.WhatsAppClient = client

	if client.Store.ID == nil {
		if err := pairWithQRCode(client, logger); err != nil {
			notifyOwner(logger, "WhatsApp pairing did not complete, so the bridge stops now.\n"+
				"Restart the bridge to get a new QR code.\n\n<code>"+html.EscapeString(err.Error())+"</code>")
			return err
		}
	} else {
		markBacklogCutoff()
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp : %s", err)
		}
	}

	// Defensive: pairWithQRCode only returns nil after a successful pairing,
	// but never dereference a nil device ID in the log line below.
	if client.Store.ID == nil {
		return errors.New("could not complete whatsapp login : no device ID after pairing")
	}

	logger.Info("successfully logged into WhatsApp",
		zap.String("push_name", client.Store.PushName),
		zap.String("jid", client.Store.ID.String()),
	)

	return nil
}
