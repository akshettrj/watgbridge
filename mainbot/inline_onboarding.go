package mainbot

import (
	"fmt"
	"strconv"
	"strings"

	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

const (
	cbNewWABridge       = "mnu:new_wa_bridge"
	cbListBridges       = "mnu:list_bridges"
	cbChooseExistingBot = "mnu:choose_existing_bot"
	cbCreateBridgeBot   = "mnu:create_bridge_bot"
	cbSelManagedPrefix  = "mbs:"
)

func mainBotStartInlineMarkup() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "🟢 New WhatsApp bridge", CallbackData: cbNewWABridge}},
			{{Text: "List my bridges", CallbackData: cbListBridges}},
		},
	}
}

func managedOnboardingSubMenuMarkup() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "Choose existing bot", CallbackData: cbChooseExistingBot}},
			{{Text: "Create new bridge bot", CallbackData: cbCreateBridgeBot}},
		},
	}
}

func managedOnboardingCallbackFilter(cq *gotgbot.CallbackQuery) bool {
	if cq == nil || cq.Data == "" {
		return false
	}
	d := cq.Data
	return strings.HasPrefix(d, "mnu:") || strings.HasPrefix(d, cbSelManagedPrefix)
}

func managedOnboardingCallbackHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		cq := c.CallbackQuery
		if cq == nil || cq.From.Id == 0 {
			return nil
		}
		if cq.Message != nil && cq.Message.GetChat().Type != gotgbot.ChatTypePrivate {
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Use this bot in private chat.", ShowAlert: true})
			return nil
		}
		data := cq.Data
		switch {
		case data == cbNewWABridge:
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			_, err := b.SendMessage(cq.From.Id,
				"<b>New WhatsApp bridge</b>\n\nPick how to supply the bridge bot:",
				&gotgbot.SendMessageOpts{
					ParseMode:   gotgbot.ParseModeHTML,
					ReplyMarkup: managedOnboardingSubMenuMarkup(),
				})
			return err
		case data == cbListBridges:
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			text, err := bridgeListTextForOwner(cq.From.Id)
			if err != nil {
				_, e := b.SendMessage(cq.From.Id, "Failed to list bridges", nil)
				return e
			}
			_, err = b.SendMessage(cq.From.Id, text, nil)
			return err
		case data == cbChooseExistingBot:
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			return sendUnlinkedManagedBotPicker(b, cq.From.Id)
		case data == cbCreateBridgeBot:
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			return SendManagedBotCreationKeyboard(b, cq.From.Id, "")
		case strings.HasPrefix(data, cbSelManagedPrefix):
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			raw := strings.TrimPrefix(data, cbSelManagedPrefix)
			managedID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || managedID == 0 {
				_, e := b.SendMessage(cq.From.Id, "Invalid bot selection.", nil)
				return e
			}
			return selectManagedBotForBind(b, cq.From.Id, managedID)
		default:
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
			return nil
		}
	}
}

func sendUnlinkedManagedBotPicker(b *gotgbot.Bot, ownerUserID int64) error {
	_ = database.BridgeUserEnsure(ownerUserID)
	rows, err := database.BridgeManagedBotListUnlinked(ownerUserID)
	if err != nil {
		_, e := b.SendMessage(ownerUserID, "Could not load your managed bots: "+err.Error(), nil)
		return e
	}
	if len(rows) == 0 {
		_, e := b.SendMessage(ownerUserID,
			"No idle managed bridge bots on file. Create one with <b>Create new bridge bot</b> (or <code>/bridge_create_bot</code>).",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
		return e
	}
	var kb [][]gotgbot.InlineKeyboardButton
	for _, row := range rows {
		if len(kb) >= 12 {
			break
		}
		label := managedBotShortLabel(row.BridgeBotToken, row.ManagedBotUserID)
		cb := cbSelManagedPrefix + strconv.FormatInt(row.ManagedBotUserID, 10)
		if len(cb) > 64 {
			continue
		}
		kb = append(kb, []gotgbot.InlineKeyboardButton{{Text: label, CallbackData: cb}})
	}
	_, err = b.SendMessage(ownerUserID,
		"<b>Choose a bridge bot</b>\n\nOnly bots <i>not</i> linked to an active bridge are listed.",
		&gotgbot.SendMessageOpts{
			ParseMode:   gotgbot.ParseModeHTML,
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: kb},
		})
	return err
}

func managedBotShortLabel(token string, managedUserID int64) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Sprintf("Bot id %d", managedUserID)
	}
	child, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return fmt.Sprintf("Bot id %d", managedUserID)
	}
	me, err := child.GetMe(nil)
	if err != nil || me.Id == 0 {
		return fmt.Sprintf("Bot id %d", managedUserID)
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
	return fmt.Sprintf("%s · id%d", name, me.Id)
}

func selectManagedBotForBind(b *gotgbot.Bot, ownerUserID, managedBotUserID int64) error {
	row, err := database.BridgeManagedBotGetByOwnerAndManagedID(ownerUserID, managedBotUserID)
	if err != nil || row == nil {
		_, e := b.SendMessage(ownerUserID, "That bot is not in your registry (anymore).", nil)
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
