package telegram

import (
	"fmt"
	"html"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"go.mau.fi/whatsmeow/appstate"
)

var commands = []handlers.Command{}

func AddTelegramHandlers() {
	var (
		cfg        = state.State.Config
		dispatcher = state.State.TelegramDispatcher
	)

	dispatcher.AddHandlerToGroup(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			return msg.Chat.Id == cfg.Telegram.TargetChatID
		}, BridgeTelegramToWhatsAppHandler,
	), DispatcherForwardHandlerGroup)

	commands = append(commands,
		handlers.NewCommand("start", StartCommandHandler),
		handlers.NewCommand("getwagroups", GetWhatsAppGroupsHandler),
		handlers.NewCommand("findcontact", FindContactHandler),
		handlers.NewCommand("synccontacts", SyncContactsHandler),
		handlers.NewCommand("clearpairhistory", ClearMessageIdPairsHistoryHandler),
		handlers.NewCommand("restartwa", RestartWhatsAppConnectionHandler),
		handlers.NewCommand("joininvitelink", JoinInviteLinkHandler),
		handlers.NewCommand("settargetgroupchat", SetTargetGroupChatHandler),
		handlers.NewCommand("settargetprivatechat", SetTargetPrivateChatHandler),
		handlers.NewCommand("send", SendToWhatsAppHandler),
		handlers.NewCommand("help", HelpCommandHandler),
	)

	for _, command := range commands {
		dispatcher.AddHandler(command)
	}

	state.State.TelegramCommands = append(state.State.TelegramCommands,
		gotgbot.BotCommand{
			Command:     "getwagroups",
			Description: "Get all the WhatsApp groups along with their JIDs",
		},
		gotgbot.BotCommand{
			Command:     "findcontact",
			Description: "Fuzzy find contact JIDs from names in WhatsApp",
		},
		gotgbot.BotCommand{
			Command:     "synccontacts",
			Description: "Try to sync the contacts list from WhatsApp",
		},
		gotgbot.BotCommand{
			Command:     "clearpairhistory",
			Description: "Delete all the past stored message id pairs",
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
			Command:     "settargetgroupchat",
			Description: "Set the target WhatsApp group chat for current thread",
		},
		gotgbot.BotCommand{
			Command:     "settargetprivatechat",
			Description: "Set the target WhatsApp private chat for current thread",
		},
		gotgbot.BotCommand{
			Command:     "send",
			Description: "Send a message to WhatsApp",
		},
		gotgbot.BotCommand{
			Command:     "help",
			Description: "Get all the available commands",
		},
	)
}

func BridgeTelegramToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	for _, command := range commands {
		if command.CheckUpdate(b, c) {
			return nil
		}
	}

	var (
		waClient     = state.State.WhatsAppClient
		msgToForward = c.EffectiveMessage
		msgToReplyTo = c.EffectiveMessage.ReplyToMessage
	)

	var stanzaID, participantID, waChatID string
	var err error

	if msgToReplyTo != nil && msgToReplyTo.ForumTopicCreated == nil {
		stanzaID, participantID, waChatID, err = database.MsgIdGetWaFromTg(c.EffectiveChat.Id, msgToReplyTo.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to retreive a pair from database", err)
		} else if stanzaID == "" {
			return utils.TgReplyWithErrorByContext(b, c, "Cannot send to WhatsApp", fmt.Errorf("corresponding stanza Id to replied to message not found"))
		}

		if waChatID == waClient.Store.ID.String() {
			waChatID = participantID
		}
	} else {
		waChatID, err = database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to find the chat pairing between this topic and a WhatsApp chat", err)
		}
	}

	// Status Update
	if strings.HasSuffix(waChatID, "@broadcast") {
		waChatID = participantID
	}

	waChatJID, _ := utils.WaParseJID(waChatID)

	return utils.TgSendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participantID, stanzaID, msgToReplyTo != nil)
}

func StartCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	return utils.TgReplyTextByContext(b, c, fmt.Sprintf("Hi! The bot has been up since %s",
		html.EscapeString(state.State.StartTime.In(state.State.LocalLocation).Format(state.State.Config.TimeFormat))))
}

func GetWhatsAppGroupsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	waGroups, err := waClient.GetJoinedGroups()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to retrieve the groups", err)
	}

	outputString := ""
	for groupNum, group := range waGroups {
		outputString += fmt.Sprintf("%v. %s [ <code>%s</code> ]\n",
			groupNum+1, html.EscapeString(group.Name),
			html.EscapeString(group.JID.String()))

		if len(outputString) >= 1800 {
			utils.TgReplyTextByContext(b, c, outputString)
			time.Sleep(500 * time.Millisecond)
			outputString = ""
		}
	}

	if len(outputString) > 0 {
		return utils.TgReplyTextByContext(b, c, outputString)
	}
	return nil
}

func FindContactHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : <code>" + html.EscapeString("/findcontact <search_string>") + "</code>\n"
	usageString += "Example : <code>/findcontact propheci</code>"

	args := c.Args()
	if len(args) <= 1 {
		return utils.TgReplyTextByContext(b, c, usageString)
	}
	query := args[1]

	results, resultsCount, err := utils.WaFuzzyFindContacts(query)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Encountered error while finding contacts", err)
	} else if resultsCount == 0 {
		return utils.TgReplyTextByContext(b, c, "No matching results found :(")
	}

	outputString := fmt.Sprintf("Here are the %v matching contacts:\n\n", resultsCount)
	for jid, name := range results {
		outputString += fmt.Sprintf("- <i>%s</i> [ <code>%s</code> ]\n",
			html.EscapeString(name), html.EscapeString(jid))

		if len(outputString) >= 1800 {
			utils.TgReplyTextByContext(b, c, outputString)
			time.Sleep(500 * time.Millisecond)
			outputString = ""
		}
	}

	if len(outputString) > 0 {
		return utils.TgReplyTextByContext(b, c, outputString)
	}
	return nil
}

func SyncContactsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	utils.TgReplyTextByContext(b, c, "Starting syncing contacts... may take some time")

	waClient := state.State.WhatsAppClient

	err := waClient.FetchAppState(appstate.WAPatchCriticalUnblockLow, false, false)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to sync contacts", err)
	}

	contacts, err := waClient.Store.Contacts.GetAllContacts()
	if err == nil {
		database.ContactNameBulkAddOrUpdate(contacts)
	}

	return utils.TgReplyTextByContext(b, c, "Successfully synced the contact list")
}

func ClearMessageIdPairsHistoryHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	err := database.MsgIdDropAllPairs()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to delete stored pairs", err)
	}

	return utils.TgReplyTextByContext(b, c, "Successfully deleted all the stored pairs")
}

func RestartWhatsAppConnectionHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	waClient.Disconnect()
	err := waClient.Connect()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to reconnect to WA servers", err)
	}

	return utils.TgReplyTextByContext(b, c, "Successfully restarted the WhatsApp connection")
}

func JoinInviteLinkHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: <code>" + html.EscapeString("/joininvitelink <invite_link>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		return utils.TgReplyTextByContext(b, c, usageString)
	}
	inviteLink := args[1]

	waClient := state.State.WhatsAppClient

	groupID, err := waClient.JoinGroupWithLink(inviteLink)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to join", err)
	}

	return utils.TgReplyTextByContext(b, c,
		fmt.Sprintf("Joined a new group with ID: <code>%s</code>", groupID.String()))
}

func SetTargetGroupChatHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: (Send in a topic) <code>" + html.EscapeString("/settargetgroupchat <group_id>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		return utils.TgReplyTextByContext(b, c, usageString)
	}

	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		return utils.TgReplyTextByContext(b, c, "The command should be sent in a topic")
	}

	var (
		cfg      = state.State.Config
		groupID  = args[1]
		waClient = state.State.WhatsAppClient
	)

	groupJID, _ := utils.WaParseJID(groupID)
	groupInfo, err := waClient.GetGroupInfo(groupJID)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to get group info", err)
	}
	groupJID = groupInfo.JID

	_, threadFound, err := database.ChatThreadGetTgFromWa(groupJID.String(), cfg.Telegram.TargetChatID)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to check database for existing mapping", err)
	} else if threadFound {
		return utils.TgReplyTextByContext(b, c, "A topic already exists in database for the given WhatsApp chat. Aborting...")
	}

	err = database.ChatThreadAddNewPair(groupJID.String(), cfg.Telegram.TargetChatID, c.EffectiveMessage.MessageThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to add the mapping in database. Unsuccessful", err)
	}

	return utils.TgReplyTextByContext(b, c, "Successfully mapped")
}

func SetTargetPrivateChatHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: (Send in a topic) <code>" + html.EscapeString("/settargetprivatechat <user_id>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		return utils.TgReplyTextByContext(b, c, usageString)
	}

	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		return utils.TgReplyTextByContext(b, c, "The command should be sent in a topic")
	}

	var (
		cfg     = state.State.Config
		groupID = args[1]
	)

	userJID, _ := utils.WaParseJID(groupID)

	_, threadFound, err := database.ChatThreadGetTgFromWa(userJID.String(), cfg.Telegram.TargetChatID)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to check database for existing mapping", err)
	} else if threadFound {
		return utils.TgReplyTextByContext(b, c, "A topic already exists in database for the given WhatsApp chat. Aborting...")
	}

	err = database.ChatThreadAddNewPair(userJID.String(), cfg.Telegram.TargetChatID, c.EffectiveMessage.MessageThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to add the mapping in database. Unsuccessful", err)
	}

	return utils.TgReplyTextByContext(b, c, "Successfully mapped")
}

func HelpCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	helpString := "Here are the available commands:\n\n"

	for _, command := range state.State.TelegramCommands {
		helpString += fmt.Sprintf("- <code>/%s</code> : %s\n",
			command.Command, html.EscapeString(command.Description))
	}

	return utils.TgReplyTextByContext(b, c, helpString)
}

func SendToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : Reply to a message, <code>" + html.EscapeString("/send <target_id>") + "</code>\n"
	usageString += "Example : <code>/send 911234567890</code>"

	args := c.Args()
	if len(args) <= 1 || c.EffectiveMessage.ReplyToMessage == nil || c.EffectiveMessage.ReplyToMessage.ForumTopicCreated != nil {
		return utils.TgReplyTextByContext(b, c, usageString)
	}
	waChatID := args[1]

	var (
		msgToForward                   = c.EffectiveMessage.ReplyToMessage
		msgToReplyTo  *gotgbot.Message = nil
		stanzaID                       = ""
		participantID                  = ""
	)

	waChatJID, ok := utils.WaParseJID(waChatID)
	if !ok {
		return utils.TgReplyTextByContext(b, c, "Provided JID is not valid")
	}

	return utils.TgSendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participantID, stanzaID, false)
}
