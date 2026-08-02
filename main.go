package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"watgbridge/database"
	"watgbridge/modules"
	"watgbridge/state"
	"watgbridge/telegram"
	"watgbridge/utils"
	"watgbridge/whatsapp"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
)

func findExecutablePath(logger *zap.Logger, cfg *state.Config, name string, fatalOnErr bool) string {
	path, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		if fatalOnErr {
			logger.Fatal(fmt.Sprintf("failed to set %s executable path", name), zap.Error(err))
		}
	}
	return path
}

func sendRestartNotification(logger *zap.Logger) bool {
	isRestarted, found := os.LookupEnv("WATG_IS_RESTARTED")
	if !found || isRestarted != "1" {
		return false
	}

	chatIdString, chatIdFound := os.LookupEnv("WATG_CHAT_ID")
	msgIdString, msgIdFound := os.LookupEnv("WATG_MESSAGE_ID")
	if !chatIdFound || !msgIdFound {
		return false
	}

	chatId, err1 := strconv.ParseInt(chatIdString, 10, 64)
	msgId, err2 := strconv.ParseInt(msgIdString, 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}

	opts := gotgbot.SendMessageOpts{
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: msgId,
		},
	}

	_, err := state.State.TelegramBot.SendMessage(chatId, "Successfully restarted", &opts)
	if err != nil {
		logger.Error("failed to send restart notification", zap.Error(err))
		return false
	}
	return true
}

func main() {
	// Load configuration file
	cfg := state.State.Config
	cfg.SetDefaults()

	if len(os.Args) > 1 {
		cfg.Path = os.Args[1]
	}

	err := cfg.LoadConfig()
	if err != nil {
		panic(fmt.Errorf("failed to load config file: %s", err))
	}

	deprecatedOptions := state.GetDeprecatedConfigOptions(cfg)
	if deprecatedOptions != nil {
		fmt.Println("The following options have been deprecated/removed:")
		for num, opt := range deprecatedOptions {
			fmt.Printf("%d. %s: %s\n", num+1, opt.Name, opt.Description)
		}
	}

	if cfg.Telegram.APIURL == "" {
		cfg.Telegram.APIURL = gotgbot.DefaultAPIURL
	}

	if cfg.DebugMode {
		developmentConfig := zap.NewDevelopmentConfig()
		developmentConfig.OutputPaths = append(developmentConfig.OutputPaths, "debug.log")
		state.State.Logger, err = developmentConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize development logger: %s", err))
		}
		state.State.Logger = state.State.Logger.Named("WaTgBridge_Dev")
	} else {
		productionConfig := zap.NewProductionConfig()
		state.State.Logger, err = productionConfig.Build()
		if err != nil {
			panic(fmt.Errorf("failed to initialize production logger: %s", err))
		}
		state.State.Logger = state.State.Logger.Named("WaTgBridge")
	}
	logger := state.State.Logger

	logger.Debug("loaded config file and started logger",
		zap.String("config_path", cfg.Path),
		zap.Bool("development_mode", cfg.DebugMode),
	)
	_ = logger.Sync()

	// Create local location for time
	if cfg.TimeZone == "" {
		cfg.TimeZone = "UTC"
	}
	locLoc, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		logger.Fatal("failed to set time zone",
			zap.String("time_zone", cfg.TimeZone),
			zap.Error(err),
		)
	}
	state.State.LocalLocation = locLoc

	if cfg.WhatsApp.SessionName == "" {
		cfg.WhatsApp.SessionName = "watgbridge"
	}

	if cfg.WhatsApp.LoginDatabase.Type == "" || cfg.WhatsApp.LoginDatabase.URL == "" {
		cfg.WhatsApp.LoginDatabase.Type = "sqlite3"
		cfg.WhatsApp.LoginDatabase.URL = "file:wawebstore.db?foreign_keys=on"
		logger.Debug("using sqlite3 as WhatsApp login database")
		_ = logger.Sync()
	}

	configChanged := false
	if cfg.GitExecutable == "" {
		cfg.GitExecutable = findExecutablePath(logger, cfg, "git", true)
		logger.Info("setting path to git executable", zap.String("path", cfg.GitExecutable))
		configChanged = true
	}

	if cfg.GoExecutable == "" {
		cfg.GoExecutable = findExecutablePath(logger, cfg, "go", true)
		logger.Info("setting path to go executable", zap.String("path", cfg.GoExecutable))
		configChanged = true
	}

	if cfg.FfmpegExecutable == "" && !cfg.Telegram.SkipVideoStickers {
		cfg.FfmpegExecutable = findExecutablePath(logger, cfg, "ffmpeg", true)
		logger.Info("setting path to ffmpeg executable", zap.String("path", cfg.FfmpegExecutable))
		configChanged = true
	}

	if configChanged {
		_ = logger.Sync()
		if err = cfg.SaveConfig(); err != nil {
			logger.Fatal("failed to save config file", zap.Error(err))
		}
	}

	// Setup database
	db, err := database.Connect()
	if err != nil {
		logger.Fatal("could not connect to database",
			zap.Error(err),
		)
	}
	state.State.Database = db
	err = database.AutoMigrate()
	if err != nil {
		logger.Fatal("could not migrate database tabels",
			zap.Error(err),
		)
	}

	err = telegram.NewTelegramClient()
	if err != nil {
		logger.Fatal("failed to initialize telegram client",
			zap.Error(err),
		)
	}
	_ = logger.Sync()

	err = whatsapp.NewWhatsAppClient()
	if err != nil {
		panic(err)
	}
	_ = logger.Sync()

	state.State.StartTime = time.Now().UTC()

	s := gocron.NewScheduler(time.UTC)
	s.TagsUnique()
	_, _ = s.Every(1).Hour().Tag("foo").Do(func() {
		contacts, err := state.State.WhatsAppClient.Store.Contacts.GetAllContacts(context.Background())
		if err == nil {
			_ = database.ContactNameBulkAddOrUpdate(contacts)
		}
	})

	state.State.WhatsAppClient.AddEventHandler(whatsapp.WhatsAppEventHandler)
	telegram.AddTelegramHandlers()
	modules.LoadModuleHandlers()

	if !cfg.Telegram.SkipSettingCommands {
		err = utils.TgRegisterBotCommands(state.State.TelegramBot, state.State.TelegramCommands...)
		if err != nil {
			logger.Error("failed to set my commands",
				zap.Error(err),
			)
		}
	} else {
		err = utils.TgRegisterBotCommands(state.State.TelegramBot)
		if err != nil {
			logger.Error("failed to set my commands to empty",
				zap.Error(err),
			)
		}
	}
	_ = logger.Sync()

	startMessageSuccessful := sendRestartNotification(logger)

	if !startMessageSuccessful && !cfg.Telegram.SkipStartupMessage {
		state.State.TelegramBot.SendMessage(cfg.Telegram.OwnerID, "Successfully started WaTgBridge", &gotgbot.SendMessageOpts{})
	}

	utils.StartAutomaticDatabaseBackups()

	state.State.TelegramUpdater.Idle()
}
