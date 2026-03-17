package state

import (
	"fmt"
	"io"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path             string `yaml:"-" mapstructure:"-"`
	TimeZone         string `yaml:"time_zone" mapstructure:"time_zone"`
	TimeFormat       string `yaml:"time_format" mapstructure:"time_format"`
	GitExecutable    string `yaml:"git_executable" mapstructure:"git_executable"`
	GoExecutable     string `yaml:"go_executable" mapstructure:"go_executable"`
	FfmpegExecutable string `yaml:"ffmpeg_executable" mapstructure:"ffmpeg_executable"`
	DebugMode        bool   `yaml:"debug_mode" mapstructure:"debug_mode"`

	UseGithHubBinaries bool   `yaml:"use_github_binaries" mapstructure:"use_github_binaries"`
	Architecture       string `yaml:"architecture" mapstructure:"architecture"`

	Telegram struct {
		BotToken            string  `yaml:"bot_token" mapstructure:"bot_token"`
		APIURL              string  `yaml:"api_url" mapstructure:"api_url"`
		SudoUsersID         []int64 `yaml:"sudo_users_id" mapstructure:"sudo_users_id"`
		OwnerID             int64   `yaml:"owner_id" mapstructure:"owner_id"`
		TargetChatID        int64   `yaml:"target_chat_id" mapstructure:"target_chat_id"`
		SelfHostedAPI       bool    `yaml:"self_hosted_api" mapstructure:"self_hosted_api"`
		SkipVideoStickers   bool    `yaml:"skip_video_stickers" mapstructure:"skip_video_stickers"`
		SkipSettingCommands bool    `yaml:"skip_setting_commands" mapstructure:"skip_setting_commands"`
		SendMyPresence      bool    `yaml:"send_my_presence" mapstructure:"send_my_presence"`
		SendMyReadReceipts  bool    `yaml:"send_my_read_receipts" mapstructure:"send_my_read_receipts"`
		SilentConfirmation  bool    `yaml:"silent_confirmation" mapstructure:"silent_confirmation"`
		ConfirmationType    string  `yaml:"confirmation_type" mapstructure:"confirmation_type"`
		ConfirmationEmoji   string  `yaml:"confirmation_emoji" mapstructure:"confirmation_emoji"`
		EmojiConfirmation   *bool   `yaml:"emoji_confirmation" mapstructure:"emoji_confirmation"`
		SkipStartupMessage  bool    `yaml:"skip_startup_message" mapstructure:"skip_startup_message"`
		SpoilerViewOnce     bool    `yaml:"spoiler_as_viewonce" mapstructure:"spoiler_as_viewonce"`
		Reactions           bool    `yaml:"reactions" mapstructure:"reactions"`
	} `yaml:"telegram" mapstructure:"telegram"`

	WhatsApp struct {
		LoginDatabase struct {
			Type string `yaml:"type" mapstructure:"type"`
			URL  string `yaml:"url" mapstructure:"url"`
		} `yaml:"login_database" mapstructure:"login_database"`
		StickerMetadata struct {
			PackName   string `yaml:"pack_name" mapstructure:"pack_name"`
			AuthorName string `yaml:"author_name" mapstructure:"author_name"`
		} `yaml:"sticker_metadata" mapstructure:"sticker_metadata"`
		SessionName                    string   `yaml:"session_name" mapstructure:"session_name"`
		TagAllAllowedGroups            []string `yaml:"tag_all_allowed_groups" mapstructure:"tag_all_allowed_groups"`
		IgnoreChats                    []string `yaml:"ignore_chats" mapstructure:"ignore_chats"`
		StatusIgnoredChats             []string `yaml:"status_ignored_chats" mapstructure:"status_ignored_chats"`
		SkipDocuments                  bool     `yaml:"skip_documents" mapstructure:"skip_documents"`
		SkipImages                     bool     `yaml:"skip_images" mapstructure:"skip_images"`
		SkipGIFs                       bool     `yaml:"skip_gifs" mapstructure:"skip_gifs"`
		SkipVideos                     bool     `yaml:"skip_videos" mapstructure:"skip_videos"`
		SkipVoiceNotes                 bool     `yaml:"skip_voice_notes" mapstructure:"skip_voice_notes"`
		SkipAudios                     bool     `yaml:"skip_audios" mapstructure:"skip_audios"`
		SkipStatus                     bool     `yaml:"skip_status" mapstructure:"skip_status"`
		SkipStickers                   bool     `yaml:"skip_stickers" mapstructure:"skip_stickers"`
		SkipContacts                   bool     `yaml:"skip_contacts" mapstructure:"skip_contacts"`
		SkipLocations                  bool     `yaml:"skip_locations" mapstructure:"skip_locations"`
		SkipProfilePictureUpdates      bool     `yaml:"skip_profile_picture_updates" mapstructure:"skip_profile_picture_updates"`
		SkipGroupSettingsUpdates       bool     `yaml:"skip_group_settings_updates" mapstructure:"skip_group_settings_updates"`
		SkipChatDetails                bool     `yaml:"skip_chat_details" mapstructure:"skip_chat_details"`
		SendRevokedMessageUpdates      bool     `yaml:"send_revoked_message_updates" mapstructure:"send_revoked_message_updates"`
		WhatsmeowDebugMode             bool     `yaml:"whatsmeow_debug_mode" mapstructure:"whatsmeow_debug_mode"`
		SendMyMessagesFromOtherDevices bool     `yaml:"send_my_messages_from_other_devices" mapstructure:"send_my_messages_from_other_devices"`
		CreateThreadForInfoUpdates     bool     `yaml:"create_thread_for_info_updates" mapstructure:"create_thread_for_info_updates"`
	} `yaml:"whatsapp" mapstructure:"whatsapp"`

	Database map[string]string `yaml:"database" mapstructure:"database"`

	// Redis is optional; used to cache LID→phone resolution to avoid spamming WhatsApp.
	Redis struct {
		Addr     string `yaml:"addr" mapstructure:"addr"`         // e.g. "localhost:6379"
		Password string `yaml:"password" mapstructure:"password"`
		DB       int    `yaml:"db" mapstructure:"db"`
	} `yaml:"redis" mapstructure:"redis"`
}

func (cfg *Config) LoadConfig() error {
	configFilePath := cfg.Path

	if _, err := os.Stat(configFilePath); err != nil {
		return fmt.Errorf("error with config file path : %s", err)
	}

	configFile, err := os.Open(configFilePath)
	if err != nil {
		return fmt.Errorf("could not open config file : %s", err)
	}
	defer configFile.Close()

	configBody, err := io.ReadAll(configFile)
	if err != nil {
		return fmt.Errorf("could not read config file : %s", err)
	}

	err = yaml.Unmarshal(configBody, cfg)
	if err != nil {
		return fmt.Errorf("could not parse config file : %s", err)
	}

	whatsappLoginDB := cfg.WhatsApp.LoginDatabase
	if whatsappLoginDB.Type == "sqlite3" {
		parsedUrl, err := url.Parse(whatsappLoginDB.URL)
		if err != nil {
			return fmt.Errorf("whatsapp Login Database URL is not a valid URL")
		}
		if _, found := parsedUrl.Query()["_foreign_keys"]; !found {
			q := parsedUrl.Query()
			q.Set("_foreign_keys", "on")
			if q.Has("foreign_keys") {
				q.Del("foreign_keys")
			}
			parsedUrl.RawQuery = q.Encode()
			cfg.WhatsApp.LoginDatabase.URL = parsedUrl.String()
			cfg.SaveConfig()
		}
	}

	return nil
}

func (cfg *Config) SaveConfig() error {
	configFilePath := cfg.Path

	configFile, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("could not open config file : %s", err)
	}
	defer configFile.Close()

	newConfigBody, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config into string : %s", err)
	}

	_, err = configFile.Write(newConfigBody)
	if err != nil {
		return fmt.Errorf("failed to write config file : %s", err)
	}

	return nil
}

func (cfg *Config) SetDefaults() {
	cfg.TimeZone = "UTC"

	cfg.WhatsApp.SessionName = "watgbridge"
	cfg.WhatsApp.LoginDatabase.Type = "sqlite3"
	cfg.WhatsApp.LoginDatabase.URL = "file:wawebstore.db?foreign_keys=on"
	cfg.WhatsApp.StickerMetadata.PackName = "WaTgBridge"
	cfg.WhatsApp.StickerMetadata.AuthorName = "WaTgBridge"
	cfg.WhatsApp.SkipStatus = true
}
