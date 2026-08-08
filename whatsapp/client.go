package whatsapp

import (
	"bytes"
	"context"
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
	"go.mau.fi/whatsmeow/types/events"
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

func isAndroidClientMode(mode string) bool {
	return mode == clientModeAndroid || mode == clientModeAndroidBusiness
}

// applyAndroidDeviceProps marks the companion as an Android phone in DeviceProps
// (used only during registration / pairing). This is what Linked Devices shows
// and is safe for the web multidevice protocol.
func applyAndroidDeviceProps(mode string) {
	if !isAndroidClientMode(mode) {
		return
	}
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_ANDROID_PHONE.Enum()
}

// applyAndroidLoginUserAgent switches the login UserAgent to Android so WhatsApp
// delivers view-once media (withheld from pure web companions). Mirrors
// WhiskeySockets/Baileys#2201, but must NOT be applied during registration:
// UserAgent ANDROID/SMB_ANDROID is a primary-phone identity. Pairing with it on
// the web protocol yields stream 516 then 401 (session deleted). Call this only
// once the device is already paired (existing session, or after PairSuccess).
func applyAndroidLoginUserAgent(mode string) {
	var platform waWa6.ClientPayload_UserAgent_Platform
	switch mode {
	case clientModeAndroid:
		platform = waWa6.ClientPayload_UserAgent_ANDROID
	case clientModeAndroidBusiness:
		platform = waWa6.ClientPayload_UserAgent_SMB_ANDROID
	default:
		return
	}
	store.BaseClientPayload.UserAgent.Platform = platform.Enum()
	store.BaseClientPayload.WebInfo = nil
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
	// DeviceProps only — keep web UserAgent for the registration handshake.
	applyAndroidDeviceProps(cfg.WhatsApp.ClientMode)

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

	// ANDROID_PHONE DeviceProps would make the QR advertise PairClientAndroid.
	// That companion type is for the native phone protocol; on the web multidevice
	// path it pairs then gets killed (516 → 401). Force a web companion QR type.
	if isAndroidClientMode(cfg.WhatsApp.ClientMode) {
		client.QRClientType = whatsmeow.PairClientChrome
	}

	// After pairing, WhatsApp asks the client to restart the stream. Desktop
	// companions usually get code 515 (handled inside whatsmeow). Some identities
	// get 516 instead, which whatsmeow only logs as an unknown StreamError.
	// Mirror the 515 path so pairing can complete.
	//
	// Also, once pairing succeeds, switch the login UserAgent to Android so the
	// post-pair reconnect (and all later logins) can receive view-once media.
	client.AddEventHandler(func(evt interface{}) {
		switch e := evt.(type) {
		case *events.PairSuccess:
			applyAndroidLoginUserAgent(cfg.WhatsApp.ClientMode)
			waClientLogger.Infof("Pair success; applying Android login identity for view-once (jid=%s)", e.ID)
		case *events.StreamError:
			if e.Code != "516" {
				return
			}
			if client.DisableLoginAutoReconnect {
				waClientLogger.Infof("Got 516 code, but login autoreconnect is disabled, not reconnecting")
				return
			}
			waClientLogger.Infof("Got 516 code, reconnecting...")
			go func() {
				client.Disconnect()
				if err := client.Connect(); err != nil {
					waClientLogger.Errorf("Failed to reconnect after 516 code: %v", err)
				}
			}()
		}
	})

	if client.Store.ID == nil {
		// Fresh pair: register with web UserAgent (set above), then PairSuccess
		// handler switches to Android UA before the server-driven stream restart.
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
						state.State.TelegramBot.SendMessage(
							state.State.Config.Telegram.OwnerID,
							fmt.Sprintf(
								"Please check your terminal and scan the QR code to login to WhatsApp. Failed to encode to PNG and send here:\n<code>%s</code>",
								html.EscapeString(err.Error()),
							),
							&gotgbot.SendMessageOpts{},
						)
					} else {
						state.State.TelegramBot.SendPhoto(
							state.State.Config.Telegram.OwnerID,
							gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrCodePNG)),
							&gotgbot.SendPhotoOpts{
								Caption: "Scan the above QR code to login to WhatsApp.",
							},
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
	} else {
		// Existing session: log in as Android for view-once delivery.
		applyAndroidLoginUserAgent(cfg.WhatsApp.ClientMode)
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
