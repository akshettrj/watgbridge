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
		_ = database.BridgeUserEnsure(user.Id)
		args := c.Args()
		labelHint := ""
		if len(args) > 1 {
			labelHint = strings.TrimSpace(strings.Join(args[1:], " "))
		}
		if labelHint != "" {
			pendingManagedLabelHints.Store(user.Id, labelHint)
		}
		me, err := b.GetMe(nil)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "getMe: "+err.Error(), nil)
			return sendErr
		}
		if strings.TrimSpace(me.Username) == "" {
			_, err := b.SendMessage(c.EffectiveChat.Id, "Give this main bot a @username in @BotFather first (needed for managed-bot creation).", nil)
			return err
		}
		rid, err := randomManagedRequestID()
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "random request_id: "+err.Error(), nil)
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
			"chat_id":      c.EffectiveChat.Id,
			"text":         "Tap the button — Telegram will let you confirm the new bot. Requires Bot Management Mode on this main bot (@BotFather → Mini App → bot settings).",
			"reply_markup": markup,
		}, nil)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Could not show managed-bot keyboard (enable Bot Management Mode in @BotFather?): "+err.Error(), nil)
			return sendErr
		}
		deep := fmt.Sprintf("https://t.me/newbot/%s/%s?name=%s", me.Username, sugUser, url.QueryEscape(sugName))
		_, err = b.SendMessage(c.EffectiveChat.Id, "Or open: "+deep, nil)
		return err
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
			_, err := b.SendMessage(c.EffectiveChat.Id, "Usage: /bridge_bind <target_chat_id> [label]", nil)
			return err
		}
		targetChatID, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Invalid target_chat_id", nil)
			return sendErr
		}
		name := ""
		if len(args) > 2 {
			name = strings.TrimSpace(strings.Join(args[2:], " "))
		}
		pending, err := database.BridgePendingManagedGet(user.Id)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "No pending managed bot. Use /bridge_create_bot (or a t.me/newbot/… link) first.", nil)
			return sendErr
		}
		if name == "" && strings.TrimSpace(pending.LabelHint) != "" {
			name = pending.LabelHint
		}
		resp, addErr := addBridgeFromCredentials(b, manager, user.Id, pending.BridgeBotToken, targetChatID, name)
		if addErr != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, addErr.Error(), nil)
			return sendErr
		}
		_ = database.BridgePendingManagedDelete(user.Id)
		pendingManagedLabelHints.Delete(user.Id)
		_, err = b.SendMessage(c.EffectiveChat.Id, resp, nil)
		return err
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
