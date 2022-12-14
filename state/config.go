package state

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path       string `yaml:"-"`
	TimeZone   string `yaml:"time_zone"`
	TimeFormat string `yaml:"time_format"`

	Telegram struct {
		BotToken      string `yaml:"bot_token"`
		APIURL        string `yaml:"api_url"`
		SelfHostedAPI bool   `yaml:"self_hosted_api"`
		OwnerID       int64  `yaml:"owner_id"`
		TargetChatID  int64  `yaml:"target_chat_id"`
	} `yaml:"telegram"`

	WhatsApp struct {
		TagAllAllowedGroups []string `yaml:"tag_all_allowed_groups"`
		StatusIgnoredChats  []string `yaml:"status_ignored_chats"`
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
