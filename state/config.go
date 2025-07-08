package state

import (
	"fmt"
	"io"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path             string `yaml:"-"`
	TimeZone         string `yaml:"time_zone"`
	TimeFormat       string `yaml:"time_format"`
	GitExecutable    string `yaml:"git_executable"`
	GoExecutable     string `yaml:"go_executable"`
	FfmpegExecutable string `yaml:"ffmpeg_executable"`
	DebugMode        bool   `yaml:"debug_mode"`

	UseGithHubBinaries bool   `yaml:"use_github_binaries"`
	Architecture       string `yaml:"architecture"`

	Telegram struct {
		BotToken            string  `yaml:"bot_token"`
		APIURL              string  `yaml:"api_url"`
		SudoUsersID         []int64 `yaml:"sudo_users_id"`
		OwnerID             int64   `yaml:"owner_id"`
		TargetChatID        int64   `yaml:"target_chat_id"`
		SelfHostedAPI       bool    `yaml:"self_hosted_api"`
		SkipVideoStickers   bool    `yaml:"skip_video_stickers"`
		SkipSettingCommands bool    `yaml:"skip_setting_commands"`
		SendMyPresence      bool    `yaml:"send_my_presence"`
		SendMyReadReceipts  bool    `yaml:"send_my_read_receipts"`
		SilentConfirmation  bool    `yaml:"silent_confirmation"`
		ConfirmationType    string  `yaml:"confirmation_type"`
		EmojiConfirmation   *bool   `yaml:"emoji_confirmation"`
		SkipStartupMessage  bool    `yaml:"skip_startup_message"`
		SpoilerViewOnce     bool    `yaml:"spoiler_as_viewonce"`
		Reactions           bool    `yaml:"reactions"`
	} `yaml:"telegram"`

	WhatsApp struct {
		LoginDatabase struct {
			Type string `yaml:"type"`
			URL  string `yaml:"url"`
		} `yaml:"login_database"`
		StickerMetadata struct {
			PackName   string `yaml:"pack_name"`
			AuthorName string `yaml:"author_name"`
		} `yaml:"sticker_metadata"`
		SessionName                    string   `yaml:"session_name"`
		TagAllAllowedGroups            []string `yaml:"tag_all_allowed_groups"`
		IgnoreChats                    []string `yaml:"ignore_chats"`
		StatusIgnoredChats             []string `yaml:"status_ignored_chats"`
		SkipDocuments                  bool     `yaml:"skip_documents"`
		SkipImages                     bool     `yaml:"skip_images"`
		SkipGIFs                       bool     `yaml:"skip_gifs"`
		SkipVideos                     bool     `yaml:"skip_videos"`
		SkipVoiceNotes                 bool     `yaml:"skip_voice_notes"`
		SkipAudios                     bool     `yaml:"skip_audios"`
		SkipStatus                     bool     `yaml:"skip_status"`
		SkipStickers                   bool     `yaml:"skip_stickers"`
		SkipContacts                   bool     `yaml:"skip_contacts"`
		SkipLocations                  bool     `yaml:"skip_locations"`
		SkipProfilePictureUpdates      bool     `yaml:"skip_profile_picture_updates"`
		SkipGroupSettingsUpdates       bool     `yaml:"skip_group_settings_updates"`
		SkipChatDetails                bool     `yaml:"skip_chat_details"`
		SendRevokedMessageUpdates      bool     `yaml:"send_revoked_message_updates"`
		WhatsmeowDebugMode             bool     `yaml:"whatsmeow_debug_mode"`
		SendMyMessagesFromOtherDevices bool     `yaml:"send_my_messages_from_other_devices"`
		CreateThreadForInfoUpdates     bool     `yaml:"create_thread_for_info_updates"`
	} `yaml:"whatsapp"`

	Database map[string]string `yaml:"database"`
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

	cfg.Telegram.ConfirmationType = "emoji"
}
