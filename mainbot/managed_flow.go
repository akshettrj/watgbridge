package mainbot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// ManagedBindOpts controls which bot sends retry prompts and bind errors (main vs managed bridge bot DMs).
type ManagedBindOpts struct {
	// NotifyBot sends retry prompts and non-retryable errors; defaults to mainBot when nil.
	NotifyBot *gotgbot.Bot
}

func completePendingManagedBind(mainBot *gotgbot.Bot, manager *bridge.Manager, user *gotgbot.User, targetChatID int64, labelArg string, opts *ManagedBindOpts) error {
	if user == nil {
		return nil
	}
	notify := mainBot
	if opts != nil && opts.NotifyBot != nil {
		notify = opts.NotifyBot
	}

	pending, err := database.BridgePendingManagedGet(user.Id)
	if err != nil {
		_, e := notify.SendMessage(user.Id, "No pending bridge bot. Use /bridge_create_bot first.", nil)
		return e
	}
	name := strings.TrimSpace(labelArg)
	if name == "" && strings.TrimSpace(pending.LabelHint) != "" {
		name = pending.LabelHint
	}
	// Stop temporary getUpdates on the bridge token before the bridge child process starts (same token cannot poll twice).
	tok := strings.TrimSpace(pending.BridgeBotToken)
	stopManagedBridgePollerForBridgeToken(tok)
	resp, addErr := addBridgeFromCredentials(mainBot, manager, user.Id, pending.BridgeBotToken, targetChatID, name)
	if addErr != nil {
		EnsureManagedBridgePoller(tok, mainBot, manager)
		if isRetryableManagedBindErr(addErr) {
			return sendManagedBindRetryPrompt(notify, user.Id, targetChatID, pending, addErr)
		}
		_, e := notify.SendMessage(user.Id, addErr.Error(), nil)
		return e
	}
	_ = database.BridgePendingManagedDelete(user.Id)
	pendingManagedLabelHints.Delete(user.Id)
	_, err = mainBot.SendMessage(user.Id, resp, &gotgbot.SendMessageOpts{
		ReplyMarkup: mainBotMainReplyKeyboard(),
	})
	if err != nil {
		return err
	}
	if opts != nil && opts.NotifyBot != nil && opts.NotifyBot != mainBot {
		_, _ = opts.NotifyBot.SendMessage(user.Id, "Bridge linked — details are in the main bot chat above.", &gotgbot.SendMessageOpts{
			ReplyMarkup: gotgbot.ReplyKeyboardRemove{RemoveKeyboard: true},
		})
	}
	return nil
}

// sendManagedBridgePairingLink posts a unique t.me/<bridge>?start=… link and starts polling that bridge token for /start + chat_shared.
func sendManagedBridgePairingLink(mainBot *gotgbot.Bot, manager *bridge.Manager, ownerUserID int64) error {
	pending, err := database.BridgePendingManagedGet(ownerUserID)
	if err != nil || pending == nil {
		_, e := mainBot.SendMessage(ownerUserID, "No pending bridge bot.", nil)
		return e
	}
	if strings.TrimSpace(pending.PairToken) == "" {
		_, e := mainBot.SendMessage(ownerUserID, "Pairing token missing — run /bridge_cancel_managed and set up the bridge again.", nil)
		return e
	}
	bridgeBot, err := gotgbot.NewBot(pending.BridgeBotToken, nil)
	if err != nil {
		return err
	}
	me, err := bridgeBot.GetMe(nil)
	if err != nil || me.Id == 0 {
		_, e := mainBot.SendMessage(ownerUserID, "Could not load the bridge bot profile.", nil)
		return e
	}
	un := strings.TrimSpace(me.Username)
	if un == "" {
		_, e := mainBot.SendMessage(ownerUserID,
			"Give this bridge bot a @username in @BotFather, then run /bridge_cancel_managed and repeat the managed-bridge step.",
			nil)
		return e
	}
	link := fmt.Sprintf("https://t.me/%s?start=%s", strings.TrimPrefix(un, "@"), pending.PairToken)
	EnsureManagedBridgePoller(pending.BridgeBotToken, mainBot, manager)
	mainBotReplyFlow.Delete(ownerUserID)
	// Drop any stale reply keyboard (old “Choose group” from before the bridge-only picker).
	_, _ = mainBot.SendMessage(ownerUserID, "\u2060", &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.ReplyKeyboardRemove{RemoveKeyboard: true},
	})
	_, err = mainBot.SendMessage(ownerUserID,
		"Open this link in Telegram (<b>same account</b> as here). <b>All forum steps happen in the bridge bot chat</b> — you do <b>not</b> add the control bot to the group:\n"+
			"<a href=\""+html.EscapeString(link)+"\">"+html.EscapeString(link)+"</a>\n\n"+
			"In that chat, tap <b>"+btnChooseGroup+"</b> to pick the forum. "+
			"Or bind manually from here: <code>/bridge_bind</code> &lt;group id&gt;.",
		&gotgbot.SendMessageOpts{
			ParseMode:   gotgbot.ParseModeHTML,
			ReplyMarkup: mainBotMainReplyKeyboard(),
		})
	return err
}

func randomManagedRequestID() (int32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(buf[:]) & 0x7fffffff), nil
}

func suggestedManagedBotUsername() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("watgbridge%xbot", binary.BigEndian.Uint32(buf[:]))
}

// SendManagedBotCreationKeyboard shows the request_managed_bot keyboard (private chat; ownerUserID is the user id in DM).
func SendManagedBotCreationKeyboard(b *gotgbot.Bot, ownerUserID int64, labelHint string) error {
	_ = database.BridgeUserEnsure(ownerUserID)
	if s := strings.TrimSpace(labelHint); s != "" {
		pendingManagedLabelHints.Store(ownerUserID, s)
	}
	me, err := b.GetMe(nil)
	if err != nil {
		_, sendErr := b.SendMessage(ownerUserID, "getMe: "+err.Error(), nil)
		return sendErr
	}
	if strings.TrimSpace(me.Username) == "" {
		_, err := b.SendMessage(ownerUserID, "Give this main bot a @username in @BotFather first (needed for managed-bot creation).", nil)
		return err
	}
	rid, err := randomManagedRequestID()
	if err != nil {
		_, sendErr := b.SendMessage(ownerUserID, "random request_id: "+err.Error(), nil)
		return sendErr
	}
	sugName := "WaTgBridge"
	sugUser := suggestedManagedBotUsername()
	markup := map[string]any{
		"keyboard": [][]map[string]any{
			{
				{
					"text": "Create managed bridge bot",
					"request_managed_bot": map[string]any{
						"request_id":         rid,
						"suggested_name":     sugName,
						"suggested_username": sugUser,
					},
				},
			},
		},
		"resize_keyboard":   true,
		"one_time_keyboard": true,
	}
	_, err = b.RequestWithContext(context.Background(), "sendMessage", map[string]any{
		"chat_id":      ownerUserID,
		"text":         "Tap the button — Telegram will let you confirm the new bot. Requires Bot Management Mode on this main bot (@BotFather → Mini App → bot settings).",
		"reply_markup": markup,
	}, nil)
	if err != nil {
		_, sendErr := b.SendMessage(ownerUserID, "Could not show managed-bot keyboard (enable Bot Management Mode in @BotFather?): "+err.Error(), nil)
		return sendErr
	}
	deep := fmt.Sprintf("https://t.me/newbot/%s/%s?name=%s", me.Username, sugUser, url.QueryEscape(sugName))
	_, err = b.SendMessage(ownerUserID, "Or open: "+deep, nil)
	return err
}

func bridgeCreateBotHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		if c.EffectiveChat.Type != gotgbot.ChatTypePrivate {
			_, err := b.SendMessage(c.EffectiveChat.Id, "Use /bridge_create_bot in private chat with the main bot.", nil)
			return err
		}
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		args := c.Args()
		labelHint := ""
		if len(args) > 1 {
			labelHint = strings.TrimSpace(strings.Join(args[1:], " "))
		}
		return SendManagedBotCreationKeyboard(b, user.Id, labelHint)
	}
}

func bridgeBindHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		args := c.Args()
		if len(args) < 2 {
			_, err := b.SendMessage(c.EffectiveChat.Id,
				"Open the pairing link the main bot sent for your managed bridge bot, or send:\n"+
					"<code>/bridge_bind</code> &lt;group id from group info&gt; [optional label]",
				&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
			return err
		}
		rawID, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "That doesn’t look like a number. Use the choose-group button or paste the id from the group’s info.", nil)
			return sendErr
		}
		targetChatID := NormalizeTargetChatID(rawID)
		name := ""
		if len(args) > 2 {
			name = strings.TrimSpace(strings.Join(args[2:], " "))
		}
		return completePendingManagedBind(b, manager, user, targetChatID, name, nil)
	}
}

func bridgeCancelManagedHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		stopManagedBridgePollerForOwner(user.Id)
		pendingManagedLabelHints.Delete(user.Id)
		_ = database.BridgePendingManagedDelete(user.Id)
		mainBotReplyFlow.Delete(user.Id)
		_, err := b.SendMessage(c.EffectiveChat.Id, "Cleared pending managed bridge (if any).",
			&gotgbot.SendMessageOpts{ReplyMarkup: mainBotMainReplyKeyboard()})
		return err
	}
}
