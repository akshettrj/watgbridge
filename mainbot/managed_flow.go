package mainbot

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func completePendingManagedBind(b *gotgbot.Bot, manager *bridge.Manager, user *gotgbot.User, targetChatID int64, labelArg string) error {
	if user == nil {
		return nil
	}
	pending, err := database.BridgePendingManagedGet(user.Id)
	if err != nil {
		_, e := b.SendMessage(user.Id, "No pending bridge bot. Use /bridge_create_bot first.", nil)
		return e
	}
	name := strings.TrimSpace(labelArg)
	if name == "" && strings.TrimSpace(pending.LabelHint) != "" {
		name = pending.LabelHint
	}
	if bootErr := maybeBootstrapBridgeInTarget(b, pending.BridgeBotToken, pending.ManagedBotUserID, targetChatID); bootErr != nil {
		if isRetryableManagedBindErr(bootErr) {
			return sendManagedBindRetryPrompt(b, user.Id, targetChatID, pending, bootErr)
		}
		_, e := b.SendMessage(user.Id, bootErr.Error(), nil)
		return e
	}
	resp, addErr := addBridgeFromCredentials(b, manager, user.Id, pending.BridgeBotToken, targetChatID, name)
	if addErr != nil {
		if isRetryableManagedBindErr(addErr) {
			return sendManagedBindRetryPrompt(b, user.Id, targetChatID, pending, addErr)
		}
		_, e := b.SendMessage(user.Id, addErr.Error(), nil)
		return e
	}
	_ = database.BridgePendingManagedDelete(user.Id)
	pendingManagedLabelHints.Delete(user.Id)
	mainBotTryLeaveTarget(b, targetChatID)
	_, err = b.SendMessage(user.Id, resp, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.ReplyKeyboardRemove{RemoveKeyboard: true},
	})
	return err
}

// sendManagedBridgeChooseGroupPrompt asks the user to pick a forum group via Telegram’s chat picker (private chats only).
func sendManagedBridgeChooseGroupPrompt(b *gotgbot.Bot, ownerChatID int64) error {
	rid, err := randomManagedRequestID()
	if err != nil {
		_, sendErr := b.SendMessage(ownerChatID, "Could not build group picker: "+err.Error(), nil)
		return sendErr
	}
	forumTrue := true
	markup := map[string]any{
		"keyboard": [][]map[string]any{
			{
				{
					"text": "Choose group (with topics)",
					"request_chat": map[string]any{
						"request_id":      rid,
						"chat_is_channel": false,
						"chat_is_forum":   forumTrue,
						"request_title":   true,
						"user_administrator_rights": map[string]any{
							"is_anonymous":           false,
							"can_manage_chat":        true,
							"can_delete_messages":    false,
							"can_manage_video_chats": false,
							"can_restrict_members":   false,
							"can_promote_members":    true,
							"can_change_info":        true,
							"can_invite_users":       true,
							"can_post_stories":       false,
							"can_edit_stories":       false,
							"can_delete_stories":     false,
							"can_pin_messages":       true,
							"can_manage_topics":      true,
						},
						"bot_administrator_rights": map[string]any{
							"is_anonymous":           false,
							"can_manage_chat":        true,
							"can_delete_messages":    false,
							"can_manage_video_chats": false,
							"can_restrict_members":   false,
							"can_promote_members":    true,
							"can_change_info":        false,
							"can_invite_users":       true,
							"can_post_stories":       false,
							"can_edit_stories":       false,
							"can_delete_stories":     false,
							"can_pin_messages":       false,
							"can_manage_topics":      true,
						},
					},
				},
			},
		},
		"resize_keyboard":   true,
		"one_time_keyboard": true,
	}
	_, err = b.RequestWithContext(context.Background(), "sendMessage", map[string]any{
		"chat_id":      ownerChatID,
		"parse_mode":   "HTML",
		"text": "Tap the button, then select your <b>forum</b> group. Add this main bot as admin with <b>invite users</b>, <b>add new admins</b>, and <b>manage topics</b> when Telegram asks.\n\n" +
			"I’ll try to add your bridge bot and grant <b>Manage topics</b>, then leave the group. " +
			"If Telegram can’t add the bot automatically, add the bridge bot yourself and tap <b>I’m done! Proceed</b> from my next message.\n\n" +
			"<i>Or send</i> <code>/bridge_bind</code> <i>with the group id from the group profile.</i>",
		"reply_markup": markup,
	}, nil)
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
				"Use the “Choose group (with topics)” button from my message after your bridge bot was created.\n\n"+
					"Or send: /bridge_bind <numbers from group info> [optional label]",
				nil)
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
		return completePendingManagedBind(b, manager, user, targetChatID, name)
	}
}

func bridgeCancelManagedHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		pendingManagedLabelHints.Delete(user.Id)
		_ = database.BridgePendingManagedDelete(user.Id)
		_, err := b.SendMessage(c.EffectiveChat.Id, "Cleared pending managed bridge (if any).", nil)
		return err
	}
}
