package telegram

import (
	"fmt"
	"html"

	"wa-tg-bridge/state"
	middlewares "wa-tg-bridge/telegram/middleware"
	"wa-tg-bridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func AddHandlers() {
	dispatcher := state.State.TelegramDispatcher

	dispatcher.AddHandler(handlers.NewCommand("start", StartCommandHandler))
	dispatcher.AddHandler(handlers.NewCommand("getwagroups", GetAllWhatsAppGroupsHandler))
	dispatcher.AddHandler(handlers.NewCommand("findcontact", FindContactHandler))

	state.State.TelegramCommands = append(state.State.TelegramCommands,
		gotgbot.BotCommand{
			Command:     "getwagroups",
			Description: "Get all the WhatsApp groups with their JIDs",
		},
		gotgbot.BotCommand{
			Command:     "findcontact",
			Description: "Find JIDs from contact names in WhatsApp",
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

func FindContactHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : <code>/findcontact name</code>"

	args := c.Args()
	if len(args) <= 1 {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			usageString,
			&gotgbot.SendMessageOpts{},
		)
		return err
	}
	query := args[1]

	results, err := utils.WhatsAppFindContact(query)
	if err != nil {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Encountered error while finding contacts:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{},
		)
		return err
	}

	responseText := "Here are the matching contacts:\n\n"
	for jid, name := range results {
		responseText += fmt.Sprintf(
			"- <i>%s</i> [ <code>%s</code> ]\n",
			html.EscapeString(name),
			html.EscapeString(jid),
		)
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		responseText,
		&gotgbot.SendMessageOpts{},
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
