package mainbot

import (
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// MainBotTelegramCommands is the full list for Bot API setMyCommands (slash menu + autocomplete).
func MainBotTelegramCommands() []gotgbot.BotCommand {
	return []gotgbot.BotCommand{
		{Command: "start", Description: "Help, privacy summary, and setup overview"},
		{Command: "bridge_list", Description: "List your bridges (id, name, enabled, target chat)"},
		{Command: "bridge_create_bot", Description: "Create a managed bridge bot (Bot Management Mode)"},
		{Command: "bridge_bind", Description: "Finish managed setup: pick forum group or paste group id"},
		{Command: "bridge_cancel_managed", Description: "Clear pending managed-bridge setup"},
		{Command: "bridge_add", Description: "Add bridge: bot token, group id, optional label"},
		{Command: "bridge_enable", Description: "Start a bridge by id"},
		{Command: "bridge_disable", Description: "Stop a bridge by id"},
		{Command: "bridge_delete", Description: "Remove a bridge by id"},
		{Command: "import_history", Description: "Archive Telegram Desktop export for a bridge id"},
	}
}

func registerMainBotMyCommands(bot *gotgbot.Bot) error {
	if state.State.Config.Telegram.SkipSettingCommands {
		return nil
	}
	return utils.TgRegisterBotCommands(bot, MainBotTelegramCommands()...)
}
