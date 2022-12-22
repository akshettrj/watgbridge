package telegram

import (
	"fmt"
	"html"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	middlewares "watgbridge/telegram/middleware"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"go.mau.fi/whatsmeow/appstate"
)

var commands = []handlers.Command{}

func AddHandlers() {
	dispatcher := state.State.TelegramDispatcher
	cfg := state.State.Config

	dispatcher.AddHandlerToGroup(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			if msg.Chat.Id != cfg.Telegram.TargetChatID {
				return false
			}
			if msg.ReplyToMessage == nil {
				return false
			}
			return true
		}, BridgeTelegramToWhatsAppHandler,
	), 1)

	commands = append(commands,
		handlers.NewCommand("start", StartCommandHandler),
		handlers.NewCommand("getwagroups", GetAllWhatsAppGroupsHandler),
		handlers.NewCommand("findcontact", FindContactHandler),
		handlers.NewCommand("synccontacts", SyncContactsHandler),
		handlers.NewCommand("clearpairhistory", ClearPairHistoryHandler),
		handlers.NewCommand("restartwa", RestartWhatsAppHandler),
		handlers.NewCommand("joininvitelink", JoinInviteLinkHandler),
		handlers.NewCommand("send", SendToWhatsAppHandler))

	for _, command := range commands {
		dispatcher.AddHandler(command)
	}

	state.State.TelegramCommands = append(state.State.TelegramCommands,
		gotgbot.BotCommand{
			Command:     "getwagroups",
			Description: "Get all the WhatsApp groups with their JIDs",
		},
		gotgbot.BotCommand{
			Command:     "findcontact",
			Description: "Find JIDs from contact names in WhatsApp",
		},
		gotgbot.BotCommand{
			Command:     "synccontacts",
			Description: "Force sync the WhatsApp contact lists",
		},
		gotgbot.BotCommand{
			Command:     "clearpairhistory",
			Description: "Delete all the past stored msg id pairs",
		},
		gotgbot.BotCommand{
			Command:     "restartwa",
			Description: "Restart the WhatsApp client",
		},
		gotgbot.BotCommand{
			Command:     "joininvitelink",
			Description: "Join a WhatsApp chat using invite link",
		},
		gotgbot.BotCommand{
			Command:     "send",
			Description: "Send a message to WhatsApp",
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
			html.EscapeString(state.State.StartTime.In(state.State.LocalLocation).Format(cfg.TimeFormat)),
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

	usageString := "Usage : <code>" + html.EscapeString("/findcontact <name>") + "</code>"

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

	results, resultsCount, err := utils.WhatsAppFindContact(query)
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
	loopNum := 0
	for jid, name := range results {
		responseText += fmt.Sprintf(
			"- <i>%s</i> [ <code>%s</code> ]\n",
			html.EscapeString(name),
			html.EscapeString(jid),
		)

		if len(responseText) >= 1500 && loopNum < resultsCount-1 {
			b.SendMessage(
				c.EffectiveChat.Id,
				responseText,
				&gotgbot.SendMessageOpts{},
			)
			time.Sleep(500 * time.Millisecond)
			responseText = ""
		}

		loopNum += 1
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

		if len(groupString) >= 1500 && groupNum < len(waGroups)-1 {
			b.SendMessage(
				c.EffectiveChat.Id,
				groupString,
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			time.Sleep(500 * time.Millisecond)
			groupString = ""
		}
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

func SyncContactsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	err := waClient.FetchAppState(appstate.WAPatchCriticalUnblockLow, true, false)
	if err != nil {
		_, err = b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to sync contacts:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)
		return err
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		"Successfully synced the contacts list",
		&gotgbot.SendMessageOpts{
			ReplyToMessageId: c.EffectiveMessage.MessageId,
		},
	)
	return err
}

func ClearPairHistoryHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	err := database.DropAllPairs()
	if err != nil {
		_, err = b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to delete stored pairs:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)
		return err
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		"Successfully deleted all the stored pairs",
		&gotgbot.SendMessageOpts{
			ReplyToMessageId: c.EffectiveMessage.MessageId,
		},
	)
	return err
}

func SendToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : Reply to a message to forward\n\n  <code>" + html.EscapeString("/send <target_jid>") + "</code>"

	args := c.Args()
	if len(args) <= 1 || c.EffectiveMessage.ReplyToMessage == nil {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			usageString,
			&gotgbot.SendMessageOpts{},
		)
		return err
	}
	waChat := args[1]

	msgToForward := c.EffectiveMessage.ReplyToMessage
	var msgToReplyTo *gotgbot.Message = nil

	stanzaId, participant := "", ""

	waChatJID, ok := utils.WhatsAppParseJID(waChat)
	if !ok {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			"The provided JID is not valid",
			&gotgbot.SendMessageOpts{},
		)
		return err
	}

	return sendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participant, stanzaId, false)
}

func BridgeTelegramToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	for _, command := range commands {
		if command.CheckUpdate(b, c.Update) {
			return nil
		}
	}

	waClient := state.State.WhatsAppClient

	msgToForward := c.EffectiveMessage
	msgToReplyTo := c.EffectiveMessage.ReplyToMessage

	stanzaId, participant, waChat, err := database.GetWaFromTg(c.EffectiveChat.Id, msgToReplyTo.MessageId)
	if err != nil {
		_, err = b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to retreive a pair from database:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)
		return err
	}

	if stanzaId == "" {
		return nil
	}

	if waChat == waClient.Store.ID.String() || waChat == "status@broadcast" {
		// private chat or status
		waChat = participant
	}
	waChatJID, _ := utils.WhatsAppParseJID(waChat)

	return sendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participant, stanzaId, true)
}

func RestartWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	waClient.Disconnect()
	err := waClient.Connect()
	if err != nil {
		_, err = b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to connect to WA servers:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)
		return err
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		"Successfully restarted WhatsApp connection",
		&gotgbot.SendMessageOpts{
			ReplyToMessageId: c.EffectiveMessage.MessageId,
		},
	)
	return err
}

func JoinInviteLinkHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !middlewares.CheckAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : <code>" + html.EscapeString("/joininvitelink <invitelink>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			usageString,
			&gotgbot.SendMessageOpts{},
		)
		return err
	}
	inviteLink := args[1]

	waClient := state.State.WhatsAppClient
	groupID, err := waClient.JoinGroupWithLink(inviteLink)
	if err != nil {
		_, err := b.SendMessage(
			c.EffectiveChat.Id,
			fmt.Sprintf(
				"Failed to join:\n\n<code>%s</code>",
				html.EscapeString(err.Error()),
			),
			&gotgbot.SendMessageOpts{},
		)
		return err
	}

	_, err = b.SendMessage(
		c.EffectiveChat.Id,
		fmt.Sprintf(
			"Joined a new group with ID: <code>%s</code>",
			groupID.String(),
		),
		&gotgbot.SendMessageOpts{},
	)
	return err
}
