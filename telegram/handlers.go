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
	"sort"
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
			"Update group topic titles from WhatsApp (not private threads; use /synccontactname there)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("synccontactname", SyncContactNameHandler),
			"Rename this topic to match WA contact name (run inside a private contact thread)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("synccontactphoto", SyncContactPhotoHandler),
			"Post this contact's WhatsApp profile photo in the topic and pin it (private contact thread only)",
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
		waTgBridgeCommand{
			handlers.NewCommand("statusforward", StatusForwardHandler),
			"Toggle forwarding of WhatsApp statuses to Telegram (global)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("statusignore", StatusIgnoreHandler),
			"Stop forwarding statuses from a contact (in topic or /statusignore <jid>)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("statusunignore", StatusUnignoreHandler),
			"Resume forwarding statuses from a contact",
		},
		waTgBridgeCommand{
			handlers.NewCommand("statusignorelist", StatusIgnoreListHandler),
			"List contacts whose statuses are not forwarded",
		},
		waTgBridgeCommand{
			handlers.NewCommand("check", CheckCommandHandler),
			"Check if a phone number is on WhatsApp; opens Chat to create topic",
		},
		waTgBridgeCommand{
			handlers.NewCommand("add", AddContactCommandHandler),
			"Add/update contact name (in WA topic): /add FirstName [LastName] [Company]",
		},
		waTgBridgeCommand{
			handlers.NewCommand("remove_contact", RemoveContactCommandHandler),
			"Remove contact from contact list (in topic or /remove_contact <jid>)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("remove", RemoveTopicCommandHandler),
			"Unlink and close current topic",
		},
		waTgBridgeCommand{
			handlers.NewCommand("list_contacts", ListContactsCommandHandler),
			"List all WA contacts (General topic only)",
		},
		waTgBridgeCommand{
			handlers.NewCommand("tag", TagCommandHandler),
			"Set tags for this contact (in contact thread only): /tag a,b,c",
		},
		waTgBridgeCommand{
			handlers.NewCommand("list_tags", ListTagsCommandHandler),
			"List all tags",
		},
		waTgBridgeCommand{
			handlers.NewCommand("list_contacts_by_tags", ListContactsByTagsCommandHandler),
			"List contacts that have all given tags: /list_contacts_by_tags a,b,c",
		},
		waTgBridgeCommand{
			handlers.NewCommand("archive", ArchiveChatCommandHandler),
			"Archive the WhatsApp chat linked to this topic",
		},
		waTgBridgeCommand{
			handlers.NewCommand("cache_clear", CacheClearCommandHandler),
			"Clear Redis LID→phone cache (for /list_contacts)",
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
	dispatcher.AddHandlerToGroup(handlers.NewCallback(
		func(cq *gotgbot.CallbackQuery) bool {
			return strings.HasPrefix(cq.Data, "check_chat_")
		}, CheckChatCallbackHandler), DispatcherCallbackHandlerGroup)
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

		contactThreadID, err := utils.TgGetOrMakeThreadFromWa(waChatJID, c.EffectiveChat.Id, "")
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

func ListContactsCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if c.EffectiveMessage.IsTopicMessage && c.EffectiveMessage.MessageThreadId != 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command only in the General topic (no specific topic).", nil, false)
		return err
	}
	// List from WhatsApp store (same source as "Contacts on WhatsApp"); DB is only for name overrides
	waClient := state.State.WhatsAppClient
	storeContacts, err := waClient.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to load contacts from WhatsApp", err)
	}
	if len(storeContacts) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "No contacts in the list. Run /synccontacts to sync from WhatsApp.", nil, false)
		return err
	}
	// Sort by JID string for stable order
	jids := make([]waTypes.JID, 0, len(storeContacts))
	for jid := range storeContacts {
		jids = append(jids, jid)
	}
	sort.Slice(jids, func(i, j int) bool { return jids[i].String() < jids[j].String() })
	var bld strings.Builder
	bld.WriteString(fmt.Sprintf("WA contacts (%d):\n\n", len(jids)))
	for _, jid := range jids {
		info := storeContacts[jid]
		name := info.FullName
		if name == "" {
			name = info.FirstName
		}
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			name = info.BusinessName
		}
		if name == "" {
			name = jid.User
		}
		phone := utils.WaGetPhoneForDisplay(jid.User, jid.Server)
		bld.WriteString(fmt.Sprintf("- %s — <code>%s</code>\n", html.EscapeString(name), html.EscapeString(phone)))
		if bld.Len() >= 3500 {
			_, _ = utils.TgReplyTextByContext(b, c, bld.String(), nil, false)
			time.Sleep(500 * time.Millisecond)
			bld.Reset()
		}
	}
	if bld.Len() > 0 {
		_, err = utils.TgReplyTextByContext(b, c, bld.String(), nil, false)
		return err
	}
	return nil
}

func TagCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command inside a contact topic (a thread linked to a WA contact).", nil, false)
		return err
	}
	waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
	if err != nil || waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "This topic is not linked to a WhatsApp contact.", nil, false)
		return err
	}
	if strings.Contains(waChatId, "g.us") || waChatId == "status@broadcast" || waChatId == "calls" || waChatId == "mentions" {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command in a private contact thread only (not groups or status).", nil, false)
		return err
	}
	args := c.Args()
	if len(args) < 2 {
		_, err := utils.TgReplyTextByContext(b, c, "Usage: <code>/tag a,b,c</code> — comma-separated tags (spaces allowed around commas).", nil, false)
		return err
	}
	raw := strings.Join(args[1:], " ")
	var tagNames []string
	for _, s := range strings.Split(raw, ",") {
		t := strings.TrimSpace(s)
		if t != "" {
			tagNames = append(tagNames, t)
		}
	}
	if len(tagNames) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Provide at least one tag.", nil, false)
		return err
	}
	err = database.ContactTagsSet(waChatId, tagNames)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to set tags", err)
	}
	normalized := make([]string, 0, len(tagNames))
	for _, n := range tagNames {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(n)))
	}
	_, err = utils.TgReplyTextByContext(b, c, "Tags set: "+strings.Join(normalized, ", "), nil, false)
	return err
}

func ListTagsCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	tags, err := database.TagsGetAll()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to load tags", err)
	}
	if len(tags) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "No tags yet. Use /tag in a contact topic to add tags.", nil, false)
		return err
	}
	_, err = utils.TgReplyTextByContext(b, c, fmt.Sprintf("Tags (%d):\n\n%s", len(tags), strings.Join(tags, ", ")), nil, false)
	return err
}

func ListContactsByTagsCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	args := c.Args()
	if len(args) < 2 {
		_, err := utils.TgReplyTextByContext(b, c, "Usage: <code>/list_contacts_by_tags a,b,c</code> — list contacts that have all these tags.", nil, false)
		return err
	}
	raw := strings.Join(args[1:], " ")
	var tagNames []string
	for _, s := range strings.Split(raw, ",") {
		t := strings.TrimSpace(s)
		if t != "" {
			tagNames = append(tagNames, t)
		}
	}
	if len(tagNames) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Provide at least one tag.", nil, false)
		return err
	}
	waIds, err := database.ContactTagsGetWaIdsByTags(tagNames)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to list contacts by tags", err)
	}
	if len(waIds) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "No contacts have all of these tags.", nil, false)
		return err
	}
	sort.Strings(waIds)
	var bld strings.Builder
	bld.WriteString(fmt.Sprintf("Contacts with tags [%s] (%d):\n\n", strings.Join(tagNames, ", "), len(waIds)))
	for _, waId := range waIds {
		jid, err := waTypes.ParseJID(waId)
		if err != nil {
			bld.WriteString(fmt.Sprintf("- %s\n", html.EscapeString(waId)))
			continue
		}
		name := utils.WaGetContactName(jid)
		phone := utils.WaGetPhoneForDisplay(jid.User, jid.Server)
		bld.WriteString(fmt.Sprintf("- %s — <code>%s</code>\n", html.EscapeString(name), html.EscapeString(phone)))
		if bld.Len() >= 3500 {
			_, _ = utils.TgReplyTextByContext(b, c, bld.String(), nil, false)
			time.Sleep(500 * time.Millisecond)
			bld.Reset()
		}
	}
	if bld.Len() > 0 {
		_, err = utils.TgReplyTextByContext(b, c, bld.String(), nil, false)
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
		if cfg.GitExecutable == "" || cfg.GoExecutable == "" {
			_, err := utils.TgReplyTextByContext(b, c, "Update and restart (build from source) is not available: git and/or go are not installed in this environment. Use Docker image rebuild or set use_github_binaries in config.", nil, false)
			return err
		}
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

	// fullSync=true to fetch full "Contacts on WhatsApp" list from server (not just delta)
	err := waClient.FetchAppState(context.Background(), appstate.WAPatchCriticalUnblockLow, true, false)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to sync contacts", err)
	}

	contacts, err := waClient.Store.Contacts.GetAllContacts(context.Background())
	if err == nil {
		database.ContactNameBulkAddOrUpdate(contacts)
		if rdb := state.State.RedisClient; rdb != nil {
			_ = rdb.Set(context.Background(), state.ContactsSyncKey, time.Now().UTC().Format(time.RFC3339), 0).Err()
		}
	}

	_, err = utils.TgReplyTextByContext(b, c, "Successfully synced the contact list.", nil, false)
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

	groupID, err := waClient.JoinGroupWithLink(context.Background(), inviteLink)
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
	groupInfo, err := waClient.GetGroupInfo(context.Background(), groupJID)
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
	_, err = state.State.WhatsAppClient.UpdateBlocklist(context.Background(), jid, action)
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

func StatusForwardHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	cfg := state.State.Config
	cfg.WhatsApp.SkipStatus = !cfg.WhatsApp.SkipStatus
	if err := cfg.SaveConfig(); err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to save config", err)
	}
	status := "on"
	if cfg.WhatsApp.SkipStatus {
		status = "off"
	}
	_, err := utils.TgReplyTextByContext(b, c, fmt.Sprintf("WhatsApp status forwarding is now <b>%s</b>.", status), nil, false)
	return err
}

func StatusIgnoreHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	cfg := state.State.Config
	var user string
	args := c.Args()
	if len(args) > 1 {
		jid, ok := utils.WaParseJID(args[1])
		if !ok {
			_, err := utils.TgReplyTextByContext(b, c, "Invalid JID. Usage: <code>/statusignore</code> (in a contact topic) or <code>/statusignore &lt;jid&gt;</code>", nil, false)
			return err
		}
		user = jid.User
	} else if c.EffectiveMessage.IsTopicMessage && c.EffectiveMessage.MessageThreadId != 0 {
		waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
		if err != nil || waChatId == "" {
			_, err := utils.TgReplyTextByContext(b, c, "Could not get WhatsApp chat for this topic. Use <code>/statusignore &lt;jid&gt;</code> with the contact's JID (e.g. from /findcontact).", nil, false)
			return err
		}
		jid, _ := utils.WaParseJID(waChatId)
		user = jid.User
	} else {
		_, err := utils.TgReplyTextByContext(b, c, "Send this command in a contact's topic, or use <code>/statusignore &lt;jid&gt;</code>", nil, false)
		return err
	}
	for _, u := range cfg.WhatsApp.StatusIgnoredChats {
		if u == user {
			_, err := utils.TgReplyTextByContext(b, c, "That contact is already in the status-ignore list.", nil, false)
			return err
		}
	}
	if cfg.WhatsApp.StatusIgnoredChats == nil {
		cfg.WhatsApp.StatusIgnoredChats = []string{}
	}
	cfg.WhatsApp.StatusIgnoredChats = append(cfg.WhatsApp.StatusIgnoredChats, user)
	if err := cfg.SaveConfig(); err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to save config", err)
	}
	name := utils.WaGetContactName(waTypes.NewJID(user, waTypes.DefaultUserServer))
	_, err := utils.TgReplyTextByContext(b, c, fmt.Sprintf("Statuses from <b>%s</b> will no longer be forwarded.", html.EscapeString(name)), nil, false)
	return err
}

func StatusUnignoreHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	cfg := state.State.Config
	var user string
	args := c.Args()
	if len(args) > 1 {
		jid, ok := utils.WaParseJID(args[1])
		if !ok {
			_, err := utils.TgReplyTextByContext(b, c, "Invalid JID. Usage: <code>/statusunignore</code> (in a contact topic) or <code>/statusunignore &lt;jid&gt;</code>", nil, false)
			return err
		}
		user = jid.User
	} else if c.EffectiveMessage.IsTopicMessage && c.EffectiveMessage.MessageThreadId != 0 {
		waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
		if err != nil || waChatId == "" {
			_, err := utils.TgReplyTextByContext(b, c, "Could not get WhatsApp chat for this topic. Use <code>/statusunignore &lt;jid&gt;</code>.", nil, false)
			return err
		}
		jid, _ := utils.WaParseJID(waChatId)
		user = jid.User
	} else {
		_, err := utils.TgReplyTextByContext(b, c, "Send this command in a contact's topic, or use <code>/statusunignore &lt;jid&gt;</code>", nil, false)
		return err
	}
	newList := make([]string, 0, len(cfg.WhatsApp.StatusIgnoredChats))
	found := false
	for _, u := range cfg.WhatsApp.StatusIgnoredChats {
		if u != user {
			newList = append(newList, u)
		} else {
			found = true
		}
	}
	if !found {
		_, err := utils.TgReplyTextByContext(b, c, "That contact was not in the status-ignore list.", nil, false)
		return err
	}
	cfg.WhatsApp.StatusIgnoredChats = newList
	if err := cfg.SaveConfig(); err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to save config", err)
	}
	name := utils.WaGetContactName(waTypes.NewJID(user, waTypes.DefaultUserServer))
	_, err := utils.TgReplyTextByContext(b, c, fmt.Sprintf("Statuses from <b>%s</b> will be forwarded again.", html.EscapeString(name)), nil, false)
	return err
}

func StatusIgnoreListHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	cfg := state.State.Config
	if len(cfg.WhatsApp.StatusIgnoredChats) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "No contacts in the status-ignore list. Use /statusignore in a contact topic or with a JID to add.", nil, false)
		return err
	}
	var bld strings.Builder
	bld.WriteString("Contacts whose statuses are <b>not</b> forwarded:\n\n")
	for _, user := range cfg.WhatsApp.StatusIgnoredChats {
		name := utils.WaGetContactName(waTypes.NewJID(user, waTypes.DefaultUserServer))
		bld.WriteString(fmt.Sprintf("• %s\n", html.EscapeString(name)))
	}
	_, err := utils.TgReplyTextByContext(b, c, bld.String(), nil, false)
	return err
}

func AddContactCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	usageString := "Usage (in a contact topic): <code>" + html.EscapeString("/add FirstName [LastName] [Company]") + "</code>\nNo spaces inside arguments."
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command inside a WhatsApp contact topic.", nil, false)
		return err
	}
	waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
	if err != nil || waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "This topic is not linked to a WhatsApp contact.", nil, false)
		return err
	}
	jid, ok := utils.WaParseJID(waChatId)
	if !ok {
		_, err := utils.TgReplyTextByContext(b, c, "Invalid WhatsApp chat ID for this topic.", nil, false)
		return err
	}
	args := c.Args()
	if len(args) < 2 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}
	firstName := args[1]
	var fullName, company string
	if len(args) >= 3 {
		fullName = args[1] + " " + args[2]
	} else {
		fullName = args[1]
	}
	if len(args) >= 4 {
		company = args[3]
	}
	_, _, pushName, _, found, _ := database.ContactNameGet(jid.User, jid.Server)
	if !found {
		pushName = ""
	}
	err = database.ContactNameAddNew(jid.User, jid.Server, firstName, fullName, pushName, company)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to save contact", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, "Contact updated.", nil, false)
	return err
}

func RemoveContactCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	usageString := "Usage: in a contact topic, or <code>/remove_contact &lt;jid&gt;</code>"
	var jid waTypes.JID
	var ok bool
	args := c.Args()
	if len(args) > 1 {
		jid, ok = utils.WaParseJID(args[1])
		if !ok {
			_, err := utils.TgReplyTextByContext(b, c, "Invalid JID. "+usageString, nil, false)
			return err
		}
	} else if c.EffectiveMessage.IsTopicMessage && c.EffectiveMessage.MessageThreadId != 0 {
		waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
		if err != nil || waChatId == "" {
			_, err := utils.TgReplyTextByContext(b, c, "This topic is not linked to a WhatsApp contact. "+usageString, nil, false)
			return err
		}
		jid, ok = utils.WaParseJID(waChatId)
		if !ok {
			_, err := utils.TgReplyTextByContext(b, c, "Invalid WhatsApp chat for this topic.", nil, false)
			return err
		}
	} else {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}
	err := database.ContactNameDelete(jid.User, jid.Server)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to remove contact", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, "Contact removed from list.", nil, false)
	return err
}

func CacheClearCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	rdb := state.State.RedisClient
	if rdb == nil {
		_, err := utils.TgReplyTextByContext(b, c, "Redis is not configured.", nil, false)
		return err
	}
	ctx := context.Background()
	keys, err := rdb.Keys(ctx, state.LIDToPhoneKeyPrefix+"*").Result()
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to list Redis keys", err)
	}
	if len(keys) == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "LID→phone cache is already empty.", nil, false)
		return err
	}
	if err := rdb.Del(ctx, keys...).Err(); err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to delete cache keys", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, fmt.Sprintf("Invalidated %d LID→phone cache entries.", len(keys)), nil, false)
	return err
}

func ArchiveChatCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command inside a topic linked to a WhatsApp chat.", nil, false)
		return err
	}
	waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
	if err != nil || waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "No linked WhatsApp chat for this topic.", nil, false)
		return err
	}
	jid, ok := utils.WaParseJID(waChatId)
	if !ok {
		_, err := utils.TgReplyTextByContext(b, c, "Invalid WhatsApp chat for this topic.", nil, false)
		return err
	}
	waClient := state.State.WhatsAppClient
	patch := appstate.BuildArchive(jid.ToNonAD(), true, time.Time{}, nil)
	if err := waClient.SendAppState(context.Background(), patch); err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to archive chat", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, "Chat archived.", nil, false)
	return err
}

func RemoveTopicCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use this command inside a topic.", nil, false)
		return err
	}
	tgChatId := c.EffectiveChat.Id
	tgThreadId := c.EffectiveMessage.MessageThreadId
	waChatId, err := database.ChatThreadGetWaFromTg(tgChatId, tgThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to get thread pairing", err)
	}
	if waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "No linked WhatsApp chat for this topic.", nil, false)
		return err
	}
	err = database.ChatThreadDropPairByTg(tgChatId, tgThreadId)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to unlink topic", err)
	}
	_, err = b.CloseForumTopic(tgChatId, tgThreadId, nil)
	if err != nil {
		_, _ = utils.TgReplyTextByContext(b, c, "Unlinked but failed to close topic: "+err.Error(), nil, false)
		return err
	}
	_, err = utils.TgReplyTextByContext(b, c, "Topic removed.", nil, false)
	return err
}

func CheckCommandHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	usageString := "Usage: <code>" + html.EscapeString("/check <phone number>") + "</code>"
	args := c.Args()
	if len(args) <= 1 {
		_, err := utils.TgReplyTextByContext(b, c, usageString, nil, false)
		return err
	}
	phoneInput := strings.TrimSpace(strings.Join(args[1:], " "))
	jid, ok := utils.WaParseJID(phoneInput)
	if !ok || jid.User == "" {
		_, err := utils.TgReplyTextByContext(b, c, "Invalid phone number. "+usageString, nil, false)
		return err
	}
	waClient := state.State.WhatsAppClient
	responses, err := waClient.IsOnWhatsApp(context.Background(), []string{jid.User})
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to check WhatsApp", err)
	}
	if len(responses) == 0 || !responses[0].IsIn {
		_, err := utils.TgReplyTextByContext(b, c, "Phone number is not registered at WhatsApp.", nil, false)
		return err
	}
	canonicalJID := responses[0].JID.ToNonAD().String()
	chatKeyboard := &gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{{
			Text:         "Chat",
			CallbackData: "check_chat_" + canonicalJID,
		}}},
	}
	_, err = utils.TgReplyTextByContext(b, c, "Phone number exists, click on `Chat` button to start messaging", chatKeyboard, false)
	return err
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

	ppInfo, err := waClient.GetProfilePictureInfo(context.Background(), userJID, &whatsmeow.GetProfilePictureParams{})
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

func SyncContactNameHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use <code>/synccontactname</code> inside a private contact topic (not General).", nil, false)
		return err
	}
	waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
	if err != nil || waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "This topic is not linked to a WhatsApp chat.", nil, false)
		return err
	}
	if strings.Contains(waChatId, "g.us") || waChatId == "status@broadcast" || waChatId == "calls" || waChatId == "mentions" {
		_, err := utils.TgReplyTextByContext(b, c, "Use this in a private contact thread only (not groups or system topics).", nil, false)
		return err
	}
	waChatJid, ok := utils.WaParseJID(waChatId)
	if !ok {
		_, err := utils.TgReplyTextByContext(b, c, "Could not parse WhatsApp id for this topic.", nil, false)
		return err
	}
	if waChatJid.Server == waTypes.GroupServer {
		_, err := utils.TgReplyTextByContext(b, c, "Group topics: use <code>/synctopicnames</code> to refresh group titles.", nil, false)
		return err
	}
	err = utils.TgSyncForumTopicTitleFromWa(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId, waChatJid.ToNonAD())
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to rename topic", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, "Topic title updated to match WhatsApp contact name.", nil, false)
	return err
}

func SyncContactPhotoHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	if !c.EffectiveMessage.IsTopicMessage || c.EffectiveMessage.MessageThreadId == 0 {
		_, err := utils.TgReplyTextByContext(b, c, "Use <code>/synccontactphoto</code> inside a private contact topic (not General).", nil, false)
		return err
	}
	waChatId, err := database.ChatThreadGetWaFromTg(c.EffectiveChat.Id, c.EffectiveMessage.MessageThreadId)
	if err != nil || waChatId == "" {
		_, err := utils.TgReplyTextByContext(b, c, "This topic is not linked to a WhatsApp chat.", nil, false)
		return err
	}
	if strings.Contains(waChatId, "g.us") || waChatId == "status@broadcast" || waChatId == "calls" || waChatId == "mentions" {
		_, err := utils.TgReplyTextByContext(b, c, "Use this in a private contact thread only (not groups or system topics).", nil, false)
		return err
	}
	waChatJid, ok := utils.WaParseJID(waChatId)
	if !ok {
		_, err := utils.TgReplyTextByContext(b, c, "Could not parse WhatsApp id for this topic.", nil, false)
		return err
	}
	if waChatJid.Server == waTypes.GroupServer {
		_, err := utils.TgReplyTextByContext(b, c, "Use <code>/getprofilepicture</code> with a group id for groups.", nil, false)
		return err
	}

	waClient := state.State.WhatsAppClient
	targetJID := waChatJid.ToNonAD()
	if targetJID.Server == waTypes.HiddenUserServer {
		if pn, errPN := waClient.Store.LIDs.GetPNForLID(context.Background(), targetJID); errPN == nil {
			targetJID = pn.ToNonAD()
		}
	}

	ppInfo, err := waClient.GetProfilePictureInfo(context.Background(), targetJID, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to fetch profile picture from WhatsApp", err)
	}
	if ppInfo == nil || ppInfo.URL == "" {
		_, err := utils.TgReplyTextByContext(b, c, "This contact has no WhatsApp profile photo (or visibility is restricted).", nil, false)
		return err
	}

	imgBytes, err := utils.DownloadFileBytesByURL(ppInfo.URL)
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to download profile picture", err)
	}

	threadID := c.EffectiveMessage.MessageThreadId
	sentMsg, err := b.SendPhoto(c.EffectiveChat.Id, &gotgbot.FileReader{Data: bytes.NewReader(imgBytes)}, &gotgbot.SendPhotoOpts{
		MessageThreadId:     threadID,
		Caption:             "<i>WhatsApp profile photo</i>",
		ParseMode:           "HTML",
		DisableNotification: true,
	})
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Failed to send photo", err)
	}

	_, err = b.PinChatMessage(c.EffectiveChat.Id, sentMsg.MessageId, &gotgbot.PinChatMessageOpts{
		DisableNotification: true,
	})
	if err != nil {
		return utils.TgReplyWithErrorByContext(b, c, "Photo sent, but failed to pin (bot needs can_pin_messages in this group)", err)
	}
	_, err = utils.TgReplyTextByContext(b, c, "Profile photo posted and pinned.", nil, false)
	return err
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
		waChatJid, ok := utils.WaParseJID(waChatId)
		if !ok {
			continue
		}
		if waChatJid.Server != waTypes.GroupServer {
			continue
		}

		newName := utils.WaGetGroupName(waChatJid)

		b.EditForumTopic(c.EffectiveChat.Id, tgThreadId, &gotgbot.EditForumTopicOpts{
			Name:              newName,
			IconCustomEmojiId: nil,
		})
		time.Sleep(5 * time.Second)
	}

	_, err = c.EffectiveMessage.Reply(b, "Successfully synced group topic names from WhatsApp", nil)
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

func CheckChatCallbackHandler(b *gotgbot.Bot, c *ext.Context) error {
	if !utils.TgUpdateIsAuthorized(b, c) {
		return nil
	}
	cfg := state.State.Config
	cq := c.CallbackQuery
	jidString := strings.TrimPrefix(cq.Data, "check_chat_")
	if jidString == "" {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Invalid callback", ShowAlert: true})
		return nil
	}
	jid, ok := utils.WaParseJID(jidString)
	if !ok {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Invalid JID", ShowAlert: true})
		return nil
	}
	waChatIdString := jid.ToNonAD().String()
	threadName := utils.WaGetContactName(jid)
	_, err := utils.TgGetOrMakeThreadFromWa_String(waChatIdString, cfg.Telegram.TargetChatID, threadName)
	if err != nil {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Failed to create topic: " + err.Error(),
			ShowAlert: true,
			CacheTime: 60,
		})
		return err
	}
	_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Topic created. You can chat in the new topic."})
	_, _, _ = b.EditMessageText("Phone number exists. Use the new topic to chat.", &gotgbot.EditMessageTextOpts{
		ChatId:      c.EffectiveChat.Id,
		MessageId:   c.EffectiveMessage.MessageId,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{}},
	})
	return nil
}
