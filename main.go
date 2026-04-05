package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/mainbot"
	"watgbridge/modules"
	"watgbridge/state"
	"watgbridge/telegram"
	"watgbridge/utils"
	"watgbridge/crypto/sqlitekey"
	"watgbridge/whatsapp"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/go-co-op/gocron"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

func main() {
	// Flags (override config file and env)
	configPath := pflag.StringP("config", "c", "config.yaml", "config file path")
	pflag.String("mode", "single", "run mode: single or multi")
	pflag.String("main-telegram-bot-token", "", "Telegram main bot token (control plane)")
	pflag.String("telegram-bot-token", "", "Telegram bot token")
	pflag.Int64("telegram-owner-id", 0, "Telegram owner user ID")
	pflag.Int64("telegram-target-chat-id", 0, "Telegram target chat/group ID")
	pflag.Bool("debug", false, "enable debug mode")
	pflag.String("time-zone", "UTC", "time zone")
	pflag.String("time-format", "02 Jan, 2006 - Mon @ 15:04", "time format")
	pflag.String("whatsapp-session-name", "watgbridge", "WhatsApp session name")
	pflag.Bool("telegram-skip-startup-message", false, "skip sending startup message to owner")
	pflag.Bool("telegram-skip-setting-commands", false, "skip setting bot commands list")
	pflag.Bool("telegram-silent-confirmation", true, "send silent confirmation when message is sent")
	pflag.String("telegram-confirmation-type", "emoji", "confirmation type: text, emoji, or none")
	pflag.Parse()

	// Config path: positional arg overrides --config for backward compat
	if pflag.NArg() > 0 {
		*configPath = pflag.Arg(0)
	}

	bindings := []state.FlagBinding{
		{ViperKey: "mode", Flag: pflag.Lookup("mode")},
		{ViperKey: "telegram.main_bot_token", Flag: pflag.Lookup("main-telegram-bot-token")},
		{ViperKey: "telegram.bot_token", Flag: pflag.Lookup("telegram-bot-token")},
		{ViperKey: "telegram.owner_id", Flag: pflag.Lookup("telegram-owner-id")},
		{ViperKey: "telegram.target_chat_id", Flag: pflag.Lookup("telegram-target-chat-id")},
		{ViperKey: "debug_mode", Flag: pflag.Lookup("debug")},
		{ViperKey: "time_zone", Flag: pflag.Lookup("time-zone")},
		{ViperKey: "time_format", Flag: pflag.Lookup("time-format")},
		{ViperKey: "whatsapp.session_name", Flag: pflag.Lookup("whatsapp-session-name")},
		{ViperKey: "telegram.skip_startup_message", Flag: pflag.Lookup("telegram-skip-startup-message")},
		{ViperKey: "telegram.skip_setting_commands", Flag: pflag.Lookup("telegram-skip-setting-commands")},
		{ViperKey: "telegram.silent_confirmation", Flag: pflag.Lookup("telegram-silent-confirmation")},
		{ViperKey: "telegram.confirmation_type", Flag: pflag.Lookup("telegram-confirmation-type")},
	}

	var err error
	if err = state.InitConfig(*configPath, bindings); err != nil {
		panic(fmt.Errorf("failed to load config: %w", err))
	}

	if v := os.Getenv("WATGBRIDGE_VERSION"); v != "" {
		state.WATGBRIDGE_VERSION = v
	}

	cfg := state.State.Config
	if cfg.Mode == "multi" {
		if cfg.Telegram.MainBotToken == "" {
			panic(fmt.Errorf("telegram.main_bot_token is required in multi mode"))
		}
	} else if cfg.Telegram.BotToken == "" || cfg.Telegram.OwnerID == 0 || cfg.Telegram.TargetChatID == 0 {
		panic(fmt.Errorf("telegram.bot_token, telegram.owner_id and telegram.target_chat_id are required (set in config file, env TELEGRAM_BOT_TOKEN/TELEGRAM_OWNER_ID/TELEGRAM_TARGET_CHAT_ID, or flags --telegram-bot-token/--telegram-owner-id/--telegram-target-chat-id)"))
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
	switch cfg.Mode {
	case "single":
		fields := []zap.Field{zap.String("watgbridge_process_role", "bridge_child")}
		if cfg.Telegram.OwnerID != 0 {
			fields = append(fields, zap.Int64("bridge_owner_telegram_user_id", cfg.Telegram.OwnerID))
		}
		if s := os.Getenv("WATG_BRIDGE_ID"); s != "" {
			if n, err := strconv.ParseUint(s, 10, 64); err == nil {
				fields = append(fields, zap.Uint("bridge_id", uint(n)))
			}
		}
		logger = logger.With(fields...)
	case "multi":
		logger = logger.With(zap.String("watgbridge_process_role", "main_bot"))
	}
	state.State.Logger = logger

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

	// Git/go are optional; only used for /updateandrestart (build from source). Leave empty in Docker.
	if cfg.GitExecutable == "" {
		if gitPath, err := exec.LookPath("git"); err == nil {
			cfg.GitExecutable = gitPath
			logger.Info("setting path to git executable", zap.String("path", gitPath))
			_ = logger.Sync()
			_ = cfg.SaveConfig()
		} else {
			logger.Debug("git not found in PATH; /updateandrestart will not be available")
		}
	}

	if cfg.GoExecutable == "" {
		if goPath, err := exec.LookPath("go"); err == nil {
			cfg.GoExecutable = goPath
			logger.Info("setting path to go executable", zap.String("path", goPath))
			_ = logger.Sync()
			_ = cfg.SaveConfig()
		} else {
			logger.Debug("go not found in PATH; /updateandrestart will not be available")
		}
	}

	if cfg.FfmpegExecutable == "" && !cfg.Telegram.SkipVideoStickers {
		ffmpegPath, err := exec.LookPath("ffmpeg")
		if err != nil && !errors.Is(err, exec.ErrDot) {
			logger.Fatal("failed to set ffmpeg executable path",
				zap.Error(err),
			)
		}

		cfg.FfmpegExecutable = ffmpegPath
		logger.Info("setting path to ffmpeg executable",
			zap.String("path", ffmpegPath),
		)
		_ = logger.Sync()

		// Best-effort: try to persist ffmpeg path, but don't crash if config is read-only (e.g. Docker mount).
		if err = cfg.SaveConfig(); err != nil {
			logger.Warn("failed to save config file; continuing with in-memory config only",
				zap.Error(err),
			)
		}
	}

	// SQLCipher: derive WATG_SQLCIPHER_KEY_HEX from WATG_SQLITE_MASTER_KEY for this process (children get their own from the bridge manager).
	if master, hasMaster, err := sqlitekey.MasterKeyBytesFromEnv(); err != nil {
		logger.Fatal("invalid WATG_SQLITE_MASTER_KEY", zap.Error(err))
	} else if hasMaster && os.Getenv(sqlitekey.EnvDerived) == "" {
		dbType := cfg.Database["type"]
		waSQLite := cfg.WhatsApp.LoginDatabase.Type == "sqlite3"
		switch {
		case cfg.Mode == "multi" && dbType == "sqlite":
			k, err := sqlitekey.DeriveKeyHex(master, "watgbridge-v1/registry")
			if err != nil {
				logger.Fatal("derive sqlcipher registry key", zap.Error(err))
			}
			_ = os.Setenv(sqlitekey.EnvDerived, k)
		case cfg.Mode == "single" && (dbType == "sqlite" || waSQLite):
			k, err := sqlitekey.DeriveKeyHex(master, "watgbridge-v1/single")
			if err != nil {
				logger.Fatal("derive sqlcipher key", zap.Error(err))
			}
			_ = os.Setenv(sqlitekey.EnvDerived, k)
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
	seedMappedForumTopics(cfg)

	if cfg.Mode == "multi" {
		bridgeRoot := filepath.Join(filepath.Dir(cfg.Path), "bridges")
		manager := bridge.NewManager(bridgeRoot)
		if err := manager.StartEnabled(); err != nil {
			logger.Error("failed to start enabled bridge runtimes", zap.Error(err))
		}
		telegram.LogLaunchVersion()
		logger.Info("starting main bot in multi mode")
		if err := mainbot.Start(cfg.Telegram.MainBotToken, manager); err != nil {
			logger.Fatal("failed to start main bot", zap.Error(err))
		}
		return
	}

	if cfg.Redis.Addr != "" {
		state.State.RedisClient = state.NewRedisClient(cfg)
		if err := state.State.RedisClient.Ping(context.Background()).Err(); err != nil {
			logger.Fatal("redis ping failed", zap.String("addr", cfg.Redis.Addr), zap.Error(err))
		}
		logger.Info("redis connected", zap.String("addr", cfg.Redis.Addr))
	} else {
		logger.Debug("redis not configured; LID→phone resolution will not be cached")
	}

	err = telegram.NewTelegramClient()
	if err != nil {
		logger.Fatal("failed to initialize telegram client",
			zap.Error(err),
		)
	}
	_ = logger.Sync()

	if err := telegram.EnsureForumMetaTopicsProvisioned(); err != nil {
		logger.Fatal("forum meta topics",
			zap.Error(err),
		)
	}
	seedMappedForumTopics(state.State.Config)

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
	s.StartAsync()

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

	{
		isRestarted, found := os.LookupEnv("WATG_IS_RESTARTED")
		if !found || isRestarted != "1" {
			goto SKIP_RESTART
		}

		chatIdString, chatIdFound := os.LookupEnv("WATG_CHAT_ID")
		msgIdString, msgIdFound := os.LookupEnv("WATG_MESSAGE_ID")
		if !chatIdFound || !msgIdFound {
			goto SKIP_RESTART
		}

		chatId, chatIdSuccess := strconv.ParseInt(chatIdString, 10, 64)
		msgId, msgIdSuccess := strconv.ParseInt(msgIdString, 10, 64)
		if chatIdSuccess != nil || msgIdSuccess != nil {
			goto SKIP_RESTART
		}

		opts := gotgbot.SendMessageOpts{
			ReplyParameters: &gotgbot.ReplyParameters{
				MessageId: msgId,
			},
		}

		state.State.TelegramBot.SendMessage(chatId, "Successfully restarted", &opts)
	}
SKIP_RESTART:

	telegram.LogLaunchVersion()

	state.State.TelegramUpdater.Idle()
}

// seedMappedForumTopics wires forum thread ids from config into local state (meta topics → ChatThreadPair, BotMeta file).
func seedMappedForumTopics(cfg *state.Config) {
	tgChat := cfg.Telegram.TargetChatID
	if cfg.Telegram.CallsThreadID != 0 && tgChat != 0 {
		_, found, err := database.ChatThreadGetTgFromWa("calls", tgChat)
		if err == nil && !found {
			_ = database.ChatThreadAddNewPair("calls", tgChat, cfg.Telegram.CallsThreadID)
		}
	}
	if cfg.Telegram.StatusThreadID != 0 && tgChat != 0 {
		_, found, err := database.ChatThreadGetTgFromWa("status@broadcast", tgChat)
		if err == nil && !found {
			_ = database.ChatThreadAddNewPair("status@broadcast", tgChat, cfg.Telegram.StatusThreadID)
		}
	}
	if cfg.Telegram.BotMetaThreadID != 0 && cfg.Path != "" {
		p := filepath.Join(filepath.Dir(cfg.Path), "bot_meta_topic_id")
		_ = os.WriteFile(p, []byte(strconv.FormatInt(cfg.Telegram.BotMetaThreadID, 10)), 0o644)
	}
}
