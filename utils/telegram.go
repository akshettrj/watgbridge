package utils

import (
	"github.com/PaulSonOfLars/gotgbot/v2"
)

func RegisterBotCommand(bot *gotgbot.Bot, commands ...gotgbot.BotCommand) error {
	oldCommands, err := bot.GetMyCommands(&gotgbot.GetMyCommandsOpts{
		Scope:        gotgbot.BotCommandScopeAllPrivateChats{},
		LanguageCode: "en",
	})
	if err != nil {
		return err
	}

	hasChanges := false
	for _, command := range commands {
		commandPresent := false
		for num, oldCommand := range oldCommands {
			if command.Command == oldCommand.Command {
				commandPresent = true
				if oldCommand.Description != command.Description {
					oldCommands[num].Description = command.Description
					hasChanges = true
					break
				}
			}
		}
		if !commandPresent {
			hasChanges = true
			oldCommands = append(oldCommands, gotgbot.BotCommand{
				Command:     command.Command,
				Description: command.Description,
			})
		}
	}

	if hasChanges {
		_, err := bot.SetMyCommands(oldCommands, &gotgbot.SetMyCommandsOpts{
			Scope:        gotgbot.BotCommandScopeAllPrivateChats{},
			LanguageCode: "en",
		})
		return err
	}
	return nil
}
