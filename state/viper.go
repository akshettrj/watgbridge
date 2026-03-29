package state

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// FlagBinding maps a viper key (e.g. "telegram.bot_token") to a pflag.Flag.
// Used so flag names can use dashes (--telegram-bot-token) while viper keys use dots.
type FlagBinding struct {
	ViperKey string
	Flag     *pflag.Flag
}

// InitConfig loads config from file (if present), then applies env vars and flags.
// Precedence: defaults < config file < env < flags.
// configPath is the config file path (e.g. from --config or default "config.yaml").
// bindings map flags to viper keys so --telegram-bot-token overrides telegram.bot_token.
func InitConfig(configPath string, bindings []FlagBinding) error {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Defaults (match SetDefaults)
	v.SetDefault("mode", "single")
	v.SetDefault("time_zone", "UTC")
	v.SetDefault("whatsapp.session_name", "watgbridge")
	v.SetDefault("whatsapp.login_database.type", "sqlite3")
	v.SetDefault("whatsapp.login_database.url", "file:wawebstore.db?foreign_keys=on")
	v.SetDefault("whatsapp.sticker_metadata.pack_name", "WaTgBridge")
	v.SetDefault("whatsapp.sticker_metadata.author_name", "WaTgBridge")
	v.SetDefault("whatsapp.skip_status", true)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config file: %w", err)
		}
		// Config file not found is OK when using env/flags only
	}

	v.AutomaticEnv() // TELEGRAM_BOT_TOKEN, etc. (key upper snake from path)

	for _, b := range bindings {
		if b.Flag != nil {
			_ = v.BindPFlag(b.ViperKey, b.Flag)
		}
	}

	if err := v.Unmarshal(&State.Config); err != nil {
		return fmt.Errorf("unmarshaling config: %w", err)
	}

	State.Config.Path = configPath

	// Redis from env (e.g. Docker: REDIS_ADDR=redis:6379) so template doesn't need it
	if State.Config.Redis.Addr == "" && os.Getenv("REDIS_ADDR") != "" {
		State.Config.Redis.Addr = os.Getenv("REDIS_ADDR")
		State.Config.Redis.Password = os.Getenv("REDIS_PASSWORD")
		if n, err := strconv.Atoi(os.Getenv("REDIS_DB")); err == nil {
			State.Config.Redis.DB = n
		}
	}

	// WhatsApp login DB URL fix (same as LoadConfig)
	if State.Config.WhatsApp.LoginDatabase.Type == "sqlite3" {
		parsedURL, err := url.Parse(State.Config.WhatsApp.LoginDatabase.URL)
		if err != nil {
			return fmt.Errorf("whatsapp login database URL is not valid: %w", err)
		}
		if _, found := parsedURL.Query()["_foreign_keys"]; !found {
			q := parsedURL.Query()
			q.Set("_foreign_keys", "on")
			if q.Has("foreign_keys") {
				q.Del("foreign_keys")
			}
			parsedURL.RawQuery = q.Encode()
			State.Config.WhatsApp.LoginDatabase.URL = parsedURL.String()
			_ = State.Config.SaveConfig()
		}
	}

	return nil
}

// ConfigPathFromArgs returns config path: first non-flag arg, or default.
// Call after flag parse so args don't include flags.
func ConfigPathFromArgs(defaultPath string) string {
	for _, a := range os.Args[1:] {
		if len(a) > 0 && a[0] != '-' {
			return a
		}
	}
	return defaultPath
}
