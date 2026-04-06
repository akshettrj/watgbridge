package mainbot

import (
	"strconv"
	"strings"
	"sync"

	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// Reply-menu button labels (exact match on user tap).
const (
	btnNewWABridge       = "🟢 New WhatsApp bridge"
	btnListBridges       = "List my bridges"
	btnChooseExistingBot = "Choose existing bot"
	btnCreateBridgeBot   = "Create new bridge bot"
	btnChooseGroup       = "Choose group (with topics)"
	btnMainMenu          = "⬅️ Main menu"
	btnBackSubmenu       = "⬅️ Back"
	btnCancel            = "Cancel"
)

type mainBotFlowKind byte

const (
	flowNone mainBotFlowKind = iota
	flowAwaitManagedBotPick
	flowAwaitManualManagedBotUsername
)

// mainBotReplyFlow scopes which plain-text messages are treated as menu actions (avoids eating random chat).
var mainBotReplyFlow sync.Map // int64 userID -> mainBotFlowKind

func mainBotMainReplyKeyboard() gotgbot.ReplyKeyboardMarkup {
	return gotgbot.ReplyKeyboardMarkup{
		Keyboard: [][]gotgbot.KeyboardButton{
			{{Text: btnNewWABridge, Style: "success"}},
			{{Text: btnListBridges}},
		},
		ResizeKeyboard:  true,
		IsPersistent:    true,
		OneTimeKeyboard: false,
	}
}

func mainBotNewBridgeSubmenuReplyKeyboard() gotgbot.ReplyKeyboardMarkup {
	return gotgbot.ReplyKeyboardMarkup{
		Keyboard: [][]gotgbot.KeyboardButton{
			{{Text: btnChooseExistingBot}},
			{{Text: btnCreateBridgeBot}},
			{{Text: btnMainMenu}},
		},
		ResizeKeyboard:  true,
		IsPersistent:    true,
		OneTimeKeyboard: false,
	}
}

// mainBotManualBotUsernamePromptReplyKeyboard is used while waiting for manual @username (no idle bots); includes Cancel to leave the state.
func mainBotManualBotUsernamePromptReplyKeyboard() gotgbot.ReplyKeyboardMarkup {
	return gotgbot.ReplyKeyboardMarkup{
		Keyboard: [][]gotgbot.KeyboardButton{
			{{Text: btnChooseExistingBot}},
			{{Text: btnCreateBridgeBot}},
			{{Text: btnCancel}},
			{{Text: btnMainMenu}},
		},
		ResizeKeyboard:  true,
		IsPersistent:    true,
		OneTimeKeyboard: false,
	}
}

func chatAdministratorRightsForRequestChatUser() *gotgbot.ChatAdministratorRights {
	return &gotgbot.ChatAdministratorRights{
		IsAnonymous:         false,
		CanManageChat:       true,
		CanDeleteMessages:   false,
		CanManageVideoChats: false,
		CanRestrictMembers:  false,
		CanPromoteMembers:   true,
		CanChangeInfo:       true,
		CanInviteUsers:      true,
		CanPostStories:      false,
		CanEditStories:      false,
		CanDeleteStories:    false,
		CanPinMessages:      true,
		CanManageTopics:     true,
	}
}

func chatAdministratorRightsForRequestChatBot() *gotgbot.ChatAdministratorRights {
	return &gotgbot.ChatAdministratorRights{
		IsAnonymous:         false,
		CanManageChat:       true,
		CanDeleteMessages:   false,
		CanManageVideoChats: false,
		CanRestrictMembers:  false,
		CanPromoteMembers:   true,
		CanChangeInfo:       false,
		CanInviteUsers:      true,
		CanPostStories:      false,
		CanEditStories:      false,
		CanDeleteStories:    false,
		CanPinMessages:      false,
		CanManageTopics:     true,
	}
}

// ManagedBridgeChooseGroupReplyKeyboard puts "Choose group (with topics)" on the first row (required).
func ManagedBridgeChooseGroupReplyKeyboard(requestID int64) gotgbot.ReplyKeyboardMarkup {
	forumTrue := true
	req := &gotgbot.KeyboardButtonRequestChat{
		RequestId:                requestID,
		ChatIsChannel:            false,
		ChatIsForum:              &forumTrue,
		RequestTitle:             true,
		UserAdministratorRights:  chatAdministratorRightsForRequestChatUser(),
		BotAdministratorRights:   chatAdministratorRightsForRequestChatBot(),
	}
	return gotgbot.ReplyKeyboardMarkup{
		Keyboard: [][]gotgbot.KeyboardButton{
			{{Text: btnChooseGroup, RequestChat: req}},
			{{Text: btnMainMenu}},
		},
		ResizeKeyboard:  true,
		IsPersistent:    true,
		OneTimeKeyboard: false,
	}
}

func mainBotReplyMenuMessageFilter(m *gotgbot.Message) bool {
	if m == nil || m.From == nil {
		return false
	}
	if m.Chat.Type != gotgbot.ChatTypePrivate || m.Text == "" {
		return false
	}
	t := strings.TrimSpace(m.Text)
	if strings.HasPrefix(t, "/") {
		return false
	}
	if isMainBotStaticMenuLabel(t) {
		return true
	}
	if v, ok := mainBotReplyFlow.Load(m.From.Id); ok {
		if fk, ok := v.(mainBotFlowKind); ok {
			switch fk {
			case flowAwaitManualManagedBotUsername:
				return true
			case flowAwaitManagedBotPick:
				if t == btnBackSubmenu || t == btnMainMenu {
					return true
				}
				return matchesUnlinkedManagedBotButton(m.From.Id, t)
			}
		}
	}
	return false
}

func isMainBotStaticMenuLabel(t string) bool {
	switch t {
	case btnNewWABridge, btnListBridges, btnChooseExistingBot, btnCreateBridgeBot, btnMainMenu, btnBackSubmenu, btnCancel:
		return true
	default:
		return false
	}
}

func matchesUnlinkedManagedBotButton(ownerUserID int64, text string) bool {
	rows, err := database.BridgeManagedBotListUnlinked(ownerUserID)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if managedBotShortLabel(r.BridgeBotToken, r.ManagedBotUserID) == text {
			return true
		}
	}
	return false
}

func mainBotReplyMenuHandler(_ *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		m := c.Message
		if m == nil || m.From == nil {
			return nil
		}
		uid := m.From.Id
		t := strings.TrimSpace(m.Text)

		switch t {
		case btnNewWABridge:
			mainBotReplyFlow.Delete(uid)
			_, err := b.SendMessage(uid,
				"<b>New WhatsApp bridge</b>\n————————————————————\nPick how to supply the bridge bot:",
				&gotgbot.SendMessageOpts{
					ParseMode:   gotgbot.ParseModeHTML,
					ReplyMarkup: mainBotNewBridgeSubmenuReplyKeyboard(),
				})
			return err
		case btnListBridges:
			mainBotReplyFlow.Delete(uid)
			textOut, err := bridgeListTextForOwner(uid)
			if err != nil {
				_, e := b.SendMessage(uid, "Failed to list bridges", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
				return e
			}
			_, err = b.SendMessage(uid, textOut, &gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
			return err
		case btnChooseExistingBot:
			return sendUnlinkedManagedBotPicker(b, uid)
		case btnCreateBridgeBot:
			mainBotReplyFlow.Delete(uid)
			return SendManagedBotCreationKeyboard(b, uid, "")
		case btnMainMenu:
			mainBotReplyFlow.Delete(uid)
			_, err := b.SendMessage(uid, "Main menu.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
			return err
		case btnCancel:
			mainBotReplyFlow.Delete(uid)
			_, err := b.SendMessage(uid, "Cancelled.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
			return err
		case btnBackSubmenu:
			mainBotReplyFlow.Delete(uid)
			_, err := b.SendMessage(uid, "Pick how to supply the bridge bot:",
				&gotgbot.SendMessageOpts{ReplyMarkup: mainBotNewBridgeSubmenuReplyKeyboard()})
			return err
		}

		if v, ok := mainBotReplyFlow.Load(uid); ok {
			if fk, ok := v.(mainBotFlowKind); ok {
				switch fk {
				case flowAwaitManualManagedBotUsername:
					return handleManualManagedBotUsernameEntry(b, uid, t)
				case flowAwaitManagedBotPick:
					if managedID, ok := lookupUnlinkedManagedBotIDByLabel(uid, t); ok {
						mainBotReplyFlow.Delete(uid)
						return selectManagedBotForBind(b, uid, managedID)
					}
				}
			}
		}
		return nil
	}
}

func lookupUnlinkedManagedBotIDByLabel(ownerUserID int64, text string) (int64, bool) {
	rows, err := database.BridgeManagedBotListUnlinked(ownerUserID)
	if err != nil {
		return 0, false
	}
	for _, r := range rows {
		if managedBotShortLabel(r.BridgeBotToken, r.ManagedBotUserID) == text {
			return r.ManagedBotUserID, true
		}
	}
	return 0, false
}

func sendUnlinkedManagedBotPicker(b *gotgbot.Bot, ownerUserID int64) error {
	_ = database.BridgeUserEnsure(ownerUserID)
	rows, err := database.BridgeManagedBotListUnlinked(ownerUserID)
	if err != nil {
		_, e := b.SendMessage(ownerUserID, "Could not load your managed bots: "+err.Error(), nil)
		return e
	}
	if len(rows) == 0 {
		mainBotReplyFlow.Store(ownerUserID, flowAwaitManualManagedBotUsername)
		_, e := b.SendMessage(ownerUserID,
			"No idle managed bridge bots found.\n\n"+
				"Try <code>@username</code> or, if Telegram won’t resolve it, the bot’s numeric id: <code>id:1234567890</code> (BotFather / <code>getMe</code>).",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	mainBotReplyFlow.Store(ownerUserID, flowAwaitManagedBotPick)
	var kb [][]gotgbot.KeyboardButton
	for _, row := range rows {
		if len(kb) >= 12 {
			break
		}
		label := managedBotShortLabel(row.BridgeBotToken, row.ManagedBotUserID)
		kb = append(kb, []gotgbot.KeyboardButton{{Text: label}})
	}
	kb = append(kb, []gotgbot.KeyboardButton{{Text: btnBackSubmenu}})
	_, err = b.SendMessage(ownerUserID,
		"<b>Choose a bridge bot</b>\n————————————————————\nOnly bots <i>not</i> linked to an active bridge are listed.",
		&gotgbot.SendMessageOpts{
			ParseMode:   gotgbot.ParseModeHTML,
			ReplyMarkup: gotgbot.ReplyKeyboardMarkup{
				Keyboard:        kb,
				ResizeKeyboard:  true,
				IsPersistent:    true,
				OneTimeKeyboard: false,
			},
		})
	return err
}

func managedBotShortLabel(token string, managedUserID int64) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "Bot id " + strconv.FormatInt(managedUserID, 10)
	}
	child, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return "Bot id " + strconv.FormatInt(managedUserID, 10)
	}
	me, err := child.GetMe(nil)
	if err != nil || me.Id == 0 {
		return "Bot id " + strconv.FormatInt(managedUserID, 10)
	}
	if u := strings.TrimSpace(me.Username); u != "" {
		s := "@" + u
		if len(s) > 60 {
			s = s[:57] + "…"
		}
		return s
	}
	name := strings.TrimSpace(me.FirstName)
	if name == "" {
		name = "Bridge bot"
	}
	if len(name) > 50 {
		name = name[:47] + "…"
	}
	return name + " · id" + strconv.FormatInt(me.Id, 10)
}

func selectManagedBotForBind(b *gotgbot.Bot, ownerUserID, managedBotUserID int64) error {
	row, err := database.BridgeManagedBotGetByOwnerAndManagedID(ownerUserID, managedBotUserID)
	if err != nil || row == nil {
		_, e := b.SendMessage(ownerUserID, "That bot is not in your registry (anymore).", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
		return e
	}
	if err := database.BridgePendingManagedUpsert(ownerUserID, row.ManagedBotUserID, row.BridgeBotToken, row.LabelHint); err != nil {
		_, e := b.SendMessage(ownerUserID, "Failed to set pending bridge: "+err.Error(), nil)
		return e
	}
	_, err = b.SendMessage(ownerUserID,
		"Selected. Next: pick the forum group — I’ll join briefly, try to add this bot as admin with <b>Manage topics</b>, then leave.",
		&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
	if err != nil {
		return err
	}
	return sendManagedBridgeChooseGroupPrompt(b, ownerUserID)
}
