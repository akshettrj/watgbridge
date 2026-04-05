package mainbot

import (
	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func managedChatSharedFilter(m *gotgbot.Message) bool {
	if m == nil || m.ChatShared == nil || m.From == nil {
		return false
	}
	if m.Chat.Type != gotgbot.ChatTypePrivate {
		return false
	}
	_, err := database.BridgePendingManagedGet(m.From.Id)
	return err == nil
}

func managedChatSharedHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		msg := c.Message
		if msg == nil || msg.ChatShared == nil || msg.From == nil {
			return nil
		}
		user := msg.From
		chatID := NormalizeTargetChatID(msg.ChatShared.ChatId)
		return completePendingManagedBind(b, manager, user, chatID, "")
	}
}
