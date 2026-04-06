package mainbot

import (
	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"go.uber.org/zap"
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
		state.State.Logger.Info("managed bind: chat_shared received",
			zap.Int64("owner_user_id", user.Id),
			zap.Int64("target_chat_id", chatID),
			zap.Int64("raw_chat_shared_chat_id", msg.ChatShared.ChatId))
		return completePendingManagedBind(b, manager, user, chatID, "")
	}
}
