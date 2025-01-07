package state

type DeprecatedOption struct {
	Name        string
	Description string
}

func GetDeprecatedConfigOptions(cfg *Config) []DeprecatedOption {

	returnValue := []DeprecatedOption{}

	if cfg.Telegram.EmojiConfirmation != nil {
		returnValue = append(returnValue, DeprecatedOption{
			Name:        "[telegram.emoji_confirmation]",
			Description: "It has been replaced with [telegram.confirmation_type]",
		})

		if *cfg.Telegram.EmojiConfirmation {
			cfg.Telegram.ConfirmationType = "emoji"
		} else {
			cfg.Telegram.ConfirmationType = "text"
		}
		cfg.Telegram.EmojiConfirmation = nil

		cfg.SaveConfig()
	}

	if len(returnValue) > 0 {
		return returnValue
	} else {
		return nil
	}
}
