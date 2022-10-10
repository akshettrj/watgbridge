package telegram

import (
	"fmt"
	"html"

	"wa-tg-bridge/state"
	middlewares "wa-tg-bridge/telegram/middleware"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func AddHandlers() {
	dispatcher := state.State.TelegramDispatcher

	dispatcher.AddHandler(handlers.NewCommand("start", StartCommandHandler))
	dispatcher.AddHandler(handlers.NewCommand("getwagroups", GetAllWhatsAppGroupsHandler))

	state.State.TelegramCommands = append(state.State.TelegramCommands,
		gotgbot.BotCommand{
			Command:     "getwagroups",
			Description: "Get all the WhatsApp groups with their JIDs",
		},
	)
}

func StartCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	cfg := state.State.Config

	_, err := b.SendMessage(
		c.EffectiveChat.Id,
		fmt.Sprintf(
			"Hoi, the bot has been up since %s",
			html.EscapeString(state.State.StartTime.Local().Format(cfg.TimeFormat)),
		),
		&gotgbot.SendMessageOpts{
			ReplyToMessageId: c.EffectiveMessage.MessageId,
		},
	)
	return err
}

func GetAllWhatsAppGroupsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	waGroups, err := waClient.GetJoinedGroups()
	if err != nil {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to retrieve the groups:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)
		return err
	}

	groupString := ""
	for groupNum, group := range waGroups {
		groupString += fmt.Sprintf(
			"%v. <i>%s</i> [ <code>%s</code> ]\n",
			groupNum+1,
			html.EscapeString(group.Name),
			html.EscapeString(group.JID.String()),
		)
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		groupString,
		&gotgbot.SendMessageOpts{
			ReplyToMessageId: c.EffectiveMessage.MessageId,
		},
	)
	return err
}
