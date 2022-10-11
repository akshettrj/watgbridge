package utils

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

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

func TelegramDownloadFileByPath(bot *gotgbot.Bot, filePath string) ([]byte, error) {

	if bot.GetAPIURL() == gotgbot.DefaultAPIURL {
		req, err := http.NewRequest(
			"GET",
			fmt.Sprintf(
				"%s/file/bot%s/%s",
				bot.GetAPIURL(),
				bot.GetToken(),
				filePath,
			),
			nil,
		)
		if err != nil {
			return nil, err
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()

		if res.StatusCode != 200 {
			return nil, fmt.Errorf("Received non-200 status code : " + res.Status)
		}

		bodyBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}

		return bodyBytes, nil
	} else {
		return os.ReadFile(filePath)
	}
}
