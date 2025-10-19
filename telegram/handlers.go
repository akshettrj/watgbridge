package telegram

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type waTgBridgeCommand struct {
	command     handlers.Command
	description string
}

var commands = []waTgBridgeCommand{}

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
		waTgBridgeCommand{
			handlers.NewCommand("start", StartCommandHandler),
			"",
		},
		waTgBridgeCommand{
			handlers.NewCommand("getwagroups", GetWhatsAppGroupsHandler),
			"Get all the WhatsApp groups along with their JIDs",
		},
		waTgBridgeCommand{
			handlers.NewCommand("findcontact", FindContactHandler),
			"Fuzzy find contact JIDs from names in WhatsApp",
		},
		waTgBridgeCommand{
			handlers.NewCommand("revoke", RevokeCommandHandler),
			"Revoke a message from WhatsApp",
		},
		waTgBridgeCommand{
			handlers.NewCommand("synccontacts", SyncContactsHandler),
			"Try to sync the contacts list from WhatsApp",
		},
		waTgBridgeCommand{
			handlers.NewCommand("clearpairhistory", ClearMessageIdPairsHistoryHandler),
			"Delete all the past stored message id pairs",
		},
		waTgBridgeCommand{
			handlers.NewCommand("restartwa", RestartWhatsAppConnectionHandler),
			"Restart the WhatsApp client",
		},
		waTgBridgeCommand{
			handlers.NewCommand("joininvitelink", JoinInviteLinkHandler),
			"Join a WhatsApp chat using invite link",
		},
		waTgBridgeCommand{
			handlers.NewCommand("settargetgroupchat", SetTargetGroupChatHandler),
			"Set the target WhatsApp group chat for current thread",
		},
		waTgBridgeCommand{
			handlers.NewCommand("settargetprivatechat", SetTargetPrivateChatHandler),
			"Set the target WhatsApp private chat for current thread",
		},
		waTgBridgeCommand{
			handlers.NewCommand("unlinkthread", UnlinkThreadHandler),
			"Unlink the current thread from its WhatsApp chat",
		},
		waTgBridgeCommand{
			handlers.NewCommand("getprofilepicture", GetProfilePictureHandler),
			"Get the profile picture of user or group using its ID",
		},
		waTgBridgeCommand{
			handlers.NewCommand("updateandrestart", UpdateAndRestartHandler),
			"Try to fetch updates from GitHub and build and restart the bot",
		},
		waTgBridgeCommand{
			handlers.NewCommand("synctopicnames", SyncTopicNamesHandler),
			"Update the names of the topics created",
		},
		waTgBridgeCommand{
			handlers.NewCommand("send", SendToWhatsAppHandler),
			"Send a message to WhatsApp",
		},
		waTgBridgeCommand{
			handlers.NewCommand("help", HelpCommandHandler),
			"Get all the available commands",
		},
		waTgBridgeCommand{
			handlers.NewCommand("block", BlockCommandHandler),
			"Block a user in WhatsApp",
		},
		waTgBridgeCommand{
			handlers.NewCommand("unblock", UnblockCommandHandler),
			"Unblock a user in WhatsApp",
		},
	)

	for _, command := range commands {
		dispatcher.AddHandler(command.command)
		if command.description != "" {
			state.State.TelegramCommands = append(state.State.TelegramCommands,
				gotgbot.BotCommand{
					Command:     command.command.Command,
					Description: command.description,
				},
			)
		}
	}

	dispatcher.AddHandlerToGroup(handlers.NewCallback(
		func(cq *gotgbot.CallbackQuery) bool {
			return strings.HasPrefix(cq.Data, "revoke")
		}, RevokeCallbackHandler), DispatcherCallbackHandlerGroup)
}

func BridgeTelegramToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	for _, command := range commands {
		if command.command.CheckUpdate(b, c) {
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
		} else if waChatID == "" {
			if c.EffectiveMessage.MessageThreadId != 0 {
				_, err = utils.TgReplyTextByContext(b, c, "No mapping found between current topic and a WhatsApp chat", nil, false)
				return err
			}
			return nil
		}
	}

	// Status Update
	if strings.HasSuffix(waChatID, "@broadcast") {
		waChatID = participantID

		waChatJID, _ := utils.WaParseJID(participantID)
		contactName := utils.WaGetContactName(waChatJID)

		contactThreadID, err := utils.TgGetOrMakeThreadFromWa(participantID, c.EffectiveChat.Id, contactName)
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to get or create a thread for the contact", err)
		}

		forwardedMsg, err := b.ForwardMessage(c.EffectiveChat.Id, c.EffectiveChat.Id, c.EffectiveMessage.MessageId, &gotgbot.ForwardMessageOpts{
			MessageThreadId: contactThreadID,
		})
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to forward the message to the contact's thread", err)
		}

		msgCopy := *msgToForward
		msgCopy.MessageId = forwardedMsg.MessageId
		msgCopy.MessageThreadId = forwardedMsg.MessageThreadId

		finalWaChatJID, _ := utils.WaParseJID(waChatID)
		return utils.TgSendToWhatsApp(b, c, &msgCopy, msgToReplyTo, finalWaChatJID, participantID, stanzaID, true)

	} else if participantID != "" {
		participant, _ := utils.WaParseJID(participantID)
		participantID = participant.ToNonAD().String()
	}

	waChatJID, _ := utils.WaParseJID(waChatID)

	return utils.TgSendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participantID, stanzaID, msgToReplyTo != nil && msgToReplyTo.ForumTopicCreated == nil)
}

func StartCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	var (
		startTime     = state.State.StartTime
		localLocation = state.State.LocalLocation
		timeFormat    = state.State.Config.TimeFormat
		upTime        = time.Now().UTC().Sub(startTime).Round(time.Second)
	)

	startMessage := "Hi! The bot is up and running\n\n"
	startMessage += fmt.Sprintf("• <b>Up Since</b>: %s [ %s ]\n",
		startTime.In(localLocation).Format(timeFormat),
		upTime.String(),
	)
	startMessage += fmt.Sprintf("• <b>Version</b>: <code>%s</code>\n", state.WATGBRIDGE_VERSION)
	if len(state.State.Modules) > 0 {
		startMessage += "• <b>Loaded Modules</b>:\n"
		for _, module := range state.State.Modules {
			startMessage += fmt.Sprintf("  - <i>%s</i>\n", html.EscapeString(module))
		}
	} else {
		startMessage += "• No Modules Loaded\n"
	}

	_, err := utils.TgReplyTextByContext(b, c, startMessage, nil, false)
	return err
}

func GetWhatsAppGroupsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	waClient := state.State.WhatsAppClient

	waGroups, err := waClient.GetJoinedGroups(context.Background())
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to retrieve the groups", err)
	}

	outputString := ""
	for groupNum, group := range waGroups {
		outputString += fmt.Sprintf("%v. %s [ <code>%s</code> ]\n",
			groupNum+1, html.EscapeString(group.Name),
			html.EscapeString(group.JID.String()))

		if len(outputString) >= 1800 {
			utils.TgReplyTextByContext(b, c, outputString, nil, false)
			time.Sleep(500 * time.Millisecond)
			outputString = ""
		}
	}

	if len(outputString) > 0 {
		_, err = utils.TgReplyTextByContext(b, c, outputString, nil, false)
		return err
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
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}
	query := args[1]

	results, resultsCount, err := utils.WaFuzzyFindContacts(query)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Encountered error while finding contacts", err)
	} else if resultsCount == 0 {
		_, err = utils.TgReplyTextByContext(b, c, "No matching results found :(", nil, false)
		return err
	}

	outputString := fmt.Sprintf("Here are the %v matching contacts:\n\n", resultsCount)
	for jid, name := range results {
		outputString += fmt.Sprintf("- <i>%s</i> [ <code>%s</code> ]\n",
			html.EscapeString(name), html.EscapeString(jid))

		if len(outputString) >= 1800 {
			utils.TgReplyTextByContext(b, c, outputString, nil, false)
			time.Sleep(500 * time.Millisecond)
			outputString = ""
		}
	}

	if len(outputString) > 0 {
		_, err = utils.TgReplyTextByContext(b, c, outputString, nil, false)
		return err
	}
	return nil
}

func UpdateAndRestartHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	cfg := state.State.Config

	if cfg.UseGithHubBinaries {
		if cfg.Architecture == "" {
			return utils.TgReplyWithErrorByContext(b, c,
				"Please set an architecture field in config file\nCan be 'amd64' or 'aarch64'",
				nil)
		}

		RELEASE_URL_FORMAT := "https://github.com/akshettrj/watgbridge/releases/latest/download/watgbridge_linux_%s"

		url := fmt.Sprintf(RELEASE_URL_FORMAT, cfg.Architecture)
		err := utils.DownloadFileToLocalByURL("watgbridge_temp", url)
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to download the release", err)
		}

		err = os.Rename("watgbridge_temp", "watgbridge")
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to rename the downloaded file", err)
		}

		err = os.Chmod("watgbridge", 0755)
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to make the file executable", err)
		}

		utils.TgReplyTextByContext(b, c, "Successfully downloaded and prepared the release, now restarting...", nil, false)

	} else {
		gitPullCmd := exec.Command(cfg.GitExecutable, "pull", "--rebase")
		err := gitPullCmd.Run()
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to execute 'git pull --rebase' command", err)
		}

		utils.TgReplyTextByContext(b, c, "Successfully pulled from GitHub", nil, false)

		goBuildCmd := exec.Command(cfg.GoExecutable, "build")
		err = goBuildCmd.Run()
		if err != nil {
			return utils.TgReplyWithErrorByContext(b, c, "Failed to execute 'go build' command", err)
		}

		utils.TgReplyTextByContext(b, c, "Successfully built the binary, now restarting...", nil, false)

	}

	os.Setenv("WATG_IS_RESTARTED", "1")
	os.Setenv("WATG_CHAT_ID", fmt.Sprint(c.EffectiveChat.Id))
	os.Setenv("WATG_MESSAGE_ID", fmt.Sprint(c.EffectiveMessage.MessageId))

	err := syscall.Exec(path.Join(".", "watgbridge"), []string{}, os.Environ())
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to run exec syscall to restart the bot", err)
	}

	return nil
}

func SyncContactsHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	utils.TgReplyTextByContext(b, c, "Starting syncing contacts... may take some time", nil, false)

	waClient := state.State.WhatsAppClient

	err := waClient.FetchAppState(context.Background(), appstate.WAPatchCriticalUnblockLow, false, false)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to sync contacts", err)
	}

	contacts, err := waClient.Store.Contacts.GetAllContacts(context.Background())
	if err == nil {
		database.ContactNameBulkAddOrUpdate(contacts)
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully synced the contact list", nil, false)
	return err
}

func ClearMessageIdPairsHistoryHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	err := database.MsgIdDropAllPairs()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to delete stored pairs", err)
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully deleted all the stored pairs", nil, false)
	return err
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

	_, err = utils.TgReplyTextByContext(b, c, "Successfully restarted the WhatsApp connection", nil, false)
	return err
}

func JoinInviteLinkHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: <code>" + html.EscapeString("/joininvitelink <invite_link>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}
	inviteLink := args[1]

	waClient := state.State.WhatsAppClient

	groupID, err := waClient.JoinGroupWithLink(inviteLink)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to join", err)
	}

	_, err = utils.TgReplyTextByContext(b, c,
		fmt.Sprintf("Joined a new group with ID: <code>%s</code>", groupID.String()), nil, false)
	return err
}

func SetTargetGroupChatHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: (Send in a topic) <code>" + html.EscapeString("/settargetgroupchat <group_id>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}

	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "The command should be sent in a topic", nil, false)
		return err
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
		_, err = utils.TgReplyTextByContext(b, c, "A topic already exists in database for the given WhatsApp chat. Aborting...", nil, false)
		return err
	}

	err = database.ChatThreadAddNewPair(groupJID.String(), cfg.Telegram.TargetChatID, c.EffectiveMessage.MessageThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to add the mapping in database. Unsuccessful", err)
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully mapped", nil, false)
	return err
}

func UnlinkThreadHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "The command should be sent in a topic", nil, false)
		return err
	}

	var (
		tgChatId   = c.EffectiveChat.Id
		tgThreadId = c.EffectiveMessage.MessageThreadId
	)

	waChatId, err := database.ChatThreadGetWaFromTg(tgChatId, tgThreadId)
	if err != nil {
		err = utils.TgReplyWithErrorByContext(b, c, "Failed to get existing chat ID pairing", err)
		return err
	} else if waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "No existing chat pairing found!!", nil, false)
		return err
	}

	err = database.ChatThreadDropPairByTg(tgChatId, tgThreadId)
	if err != nil {
		err = utils.TgReplyWithErrorByContext(b, c, "Failed to delete the thread chat pairing", err)
		return err
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully unlinked", nil, false)
	return err
}

func handleBlockUnblockUser(b *gotgbot.Bot, c *ext.Context, action events.BlocklistChangeAction) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "The command should be sent in a topic", nil, false)
		return err
	}

	var (
		tgChatId   = c.EffectiveChat.Id
		tgThreadId = c.EffectiveMessage.MessageThreadId
	)

	waChatId, err := database.ChatThreadGetWaFromTg(tgChatId, tgThreadId)
	if err != nil {
		err = utils.TgReplyWithErrorByContext(b, c, "Failed to get existing chat ID pairing", err)
		return err
	} else if waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "No existing chat pairing found!!", nil, false)
		return err
	}
	jid, _ := utils.WaParseJID(waChatId)
	_, err = state.State.WhatsAppClient.UpdateBlocklist(jid, action)
	if err != nil {
		err = utils.TgReplyWithErrorByContext(b, c, "Failed to change the blocklist status", err)
		return err
	}
	actionText := "blocked"
	if action == events.BlocklistChangeActionUnblock {
		actionText = "unblocked"
	}

	_, err = utils.TgReplyTextByContext(b, c, fmt.Sprintf("Successfully %s the user", actionText), nil, false)
	return err
}

func BlockCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	return handleBlockUnblockUser(b, c, events.BlocklistChangeActionBlock)
}

func UnblockCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	return handleBlockUnblockUser(b, c, events.BlocklistChangeActionUnblock)
}

func SetTargetPrivateChatHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage (Send in a topic): <code>" + html.EscapeString("/settargetprivatechat <user_id>") + "</code>"

	args := c.Args()
	if len(args) <= 1 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}

	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "The command should be sent in a topic", nil, false)
		return err
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
		_, err = utils.TgReplyTextByContext(b, c, "A topic already exists in database for the given WhatsApp chat. Aborting...", nil, false)
		return err
	}

	err = database.ChatThreadAddNewPair(userJID.String(), cfg.Telegram.TargetChatID, c.EffectiveMessage.MessageThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to add the mapping in database. Unsuccessful", err)
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully mapped", nil, false)
	return err
}

func GetProfilePictureHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage: <code>" + html.EscapeString("/getprofilepicture <user/group_id>") + "</code>"
	usageString += "\n\nYou need to add <code>@g.us</code> at the end for groups"

	args := c.Args()
	if len(args) <= 1 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}

	var (
		waClient = state.State.WhatsAppClient
		userID   = args[1]
	)

	userJID, _ := utils.WaParseJID(userID)

	ppInfo, err := waClient.GetProfilePictureInfo(userJID, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to fetch profile picture info from WhatsApp", err)
	}

	res, err := http.DefaultClient.Get(ppInfo.URL)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to make HTTP GET request to profile picture URL", err)
	}
	defer res.Body.Close()

	imgBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to read HTTP response body", err)
	}

	opts := &gotgbot.SendPhotoOpts{
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: c.EffectiveMessage.MessageId,
		},
	}
	if c.EffectiveMessage.IsTopicMessage {
		opts.MessageThreadId = c.EffectiveMessage.MessageThreadId
	}
	_, err = b.SendPhoto(c.EffectiveChat.Id, &gotgbot.FileReader{Data: bytes.NewReader(imgBytes)}, opts)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to send photo", err)
	}

	return nil
}

func SyncTopicNamesHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	chatThreadPairs, err := database.ChatThreadGetAllPairs(c.EffectiveChat.Id)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "failed to retreive chat thread pairs from database", err)
	}

	for _, pair := range chatThreadPairs {
		var (
			waChatId   = pair.ID
			tgThreadId = pair.TgThreadId
		)

		if waChatId == "status@broadcast" || waChatId == "calls" || waChatId == "mentions" {
			continue
		}
		waChatJid, _ := utils.WaParseJID(waChatId)

		var newName string
		if waChatJid.Server == waTypes.GroupServer {
			newName = utils.WaGetGroupName(waChatJid)
		} else {
			newName = utils.WaGetContactName(waChatJid)
		}

		b.EditForumTopic(c.EffectiveChat.Id, tgThreadId, &gotgbot.EditForumTopicOpts{
			Name:              newName,
			IconCustomEmojiId: nil,
		})
		time.Sleep(5 * time.Second)
	}

	_, err = c.EffectiveMessage.Reply(b, "Successfully synced topic names", nil)
	return err
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

	_, err := utils.TgReplyTextByContext(b, c, helpString, nil, false)
	return err
}

func SendToWhatsAppHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : Reply to a message, <code>" + html.EscapeString("/send <target_id>") + "</code>\n"
	usageString += "Example : <code>/send 911234567890</code>"

	args := c.Args()
	if len(args) <= 1 || c.EffectiveMessage.ReplyToMessage == nil || c.EffectiveMessage.ReplyToMessage.ForumTopicCreated != nil {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
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
		_, err := utils.TgReplyTextByContext(b, c, "Provided JID is not valid", nil, false)
		return err
	}

	return utils.TgSendToWhatsApp(b, c, msgToForward, msgToReplyTo, waChatJID, participantID, stanzaID, false)
}

func RevokeCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	usageString := "Usage : Reply to a message, <code>/revoke</code>"

	if c.EffectiveMessage.ReplyToMessage == nil || c.EffectiveMessage.ReplyToMessage.ForumTopicClosed != nil {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}

	var (
		waClient    = state.State.WhatsAppClient
		msgToRevoke = c.EffectiveMessage.ReplyToMessage
		chatId      = c.EffectiveChat.Id
	)

	waMsgId, _, waChatId, err := database.MsgIdGetWaFromTg(chatId, msgToRevoke.MessageId, msgToRevoke.MessageThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "failed to retrieve WhatsApp side IDs", err)
	}

	chatJid, _ := utils.WaParseJID(waChatId)
	revokeMessage := waClient.BuildRevoke(chatJid, waTypes.EmptyJID, waMsgId)
	_, err = waClient.SendMessage(context.Background(), chatJid, revokeMessage)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "failed to revoke message", err)
	}

	_, err = utils.TgReplyTextByContext(b, c, "<i>Successfully revoked</i>", nil, false)
	return err
}

func RevokeCallbackHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}

	var (
		waClient = state.State.WhatsAppClient
		cq       = c.CallbackQuery
		data     = strings.Split(cq.Data, "_")
	)

	if len(data) == 3 {

		confirmKeyboard := utils.TgMakeRevokeKeyboard(data[1], data[2], true)
		_, _, err := b.EditMessageText("Revoke the message ?", &gotgbot.EditMessageTextOpts{
			ChatId:      c.EffectiveChat.Id,
			MessageId:   c.EffectiveMessage.MessageId,
			ReplyMarkup: *confirmKeyboard,
		})
		cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Are you sure ?",
			ShowAlert: false,
		})
		return err

	} else if len(data) == 4 {

		confirmation := data[3]
		if confirmation == "n" {

			revokeKeyboard := utils.TgMakeRevokeKeyboard(data[1], data[2], false)
			_, _, err := b.EditMessageText("Successfully sent", &gotgbot.EditMessageTextOpts{
				ChatId:      c.EffectiveChat.Id,
				MessageId:   c.EffectiveMessage.MessageId,
				ReplyMarkup: *revokeKeyboard,
			})
			cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "Aborted",
				ShowAlert: true,
			})
			return err

		} else if confirmation == "y" {

			chatJid, _ := utils.WaParseJID(data[2])
			revokeMesssage := waClient.BuildRevoke(chatJid, waTypes.EmptyJID, data[1])
			_, err := waClient.SendMessage(context.Background(), chatJid, revokeMesssage)
			if err != nil {
				_, err = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
					Text:      "Failed to send revoke message : " + err.Error(),
					ShowAlert: true,
					CacheTime: 60,
				})
				return err
			} else {
				_, err = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
					Text:      "Successfully revoked",
					ShowAlert: true,
					CacheTime: 60,
				})
				b.EditMessageText("<i>Revoked</i>", &gotgbot.EditMessageTextOpts{
					ChatId:    c.EffectiveChat.Id,
					MessageId: c.EffectiveMessage.MessageId,
					ReplyMarkup: gotgbot.InlineKeyboardMarkup{
						InlineKeyboard: [][]gotgbot.InlineKeyboardButton{},
					},
				})
				database.MsgIdDeletePair(c.EffectiveChat.Id, c.EffectiveMessage.MessageId)
				return err
			}

		} else {

			_, err := cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text:      "Invalid callback query",
				ShowAlert: true,
				CacheTime: 60,
			})
			return err
		}

	} else {

		_, err := cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Invalid callback query",
			ShowAlert: true,
			CacheTime: 60,
		})
		return err
	}
}
