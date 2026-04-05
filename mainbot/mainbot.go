package mainbot

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

func Start(token string, manager *bridge.Manager) error {
	bot, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
		},
	})
	if err != nil {
		return err
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		UnhandledErrFunc: func(err error) {
			fmt.Printf("main bot dispatcher error: %v\n", err)
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	dispatcher.AddHandler(handlers.NewCommand("start", startHandler))
	dispatcher.AddHandler(handlers.NewCommand("bridge_add", bridgeAddHandler(manager)))
	dispatcher.AddHandler(handlers.NewCommand("bridge_create_bot", bridgeCreateBotHandler()))
	dispatcher.AddHandler(handlers.NewCommand("bridge_bind", bridgeBindHandler(manager)))
	dispatcher.AddHandler(handlers.NewCommand("bridge_cancel_managed", bridgeCancelManagedHandler()))
	dispatcher.AddHandler(handlers.NewCommand("bridge_list", bridgeListHandler))
	dispatcher.AddHandler(handlers.NewCommand("bridge_enable", bridgeEnableHandler(manager)))
	dispatcher.AddHandler(handlers.NewCommand("bridge_disable", bridgeDisableHandler(manager)))
	dispatcher.AddHandler(handlers.NewCommand("bridge_delete", bridgeDeleteHandler(manager)))
	dispatcher.AddHandler(handlers.NewCommand("import_history", importHistoryCommandHandler()))
	dispatcher.AddHandler(handlers.NewMessage(managedChatSharedFilter, managedChatSharedHandler(manager)))
	dispatcher.AddHandler(handlers.NewMessage(importHistoryPendingDocumentFilter, importHistoryDocumentHandler()))

	relay := newManagedRelayDispatcher(dispatcher)
	updater := ext.NewUpdater(relay, &ext.UpdaterOpts{
		UnhandledErrFunc: func(err error) {
			fmt.Printf("main bot updater error: %v\n", err)
		},
	})

	if err := updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout:        9,
			AllowedUpdates: []string{"message", "managed_bot"},
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Second,
			},
		},
	}); err != nil {
		return err
	}
	updater.Idle()
	return nil
}

func startHandler(b *gotgbot.Bot, c *ext.Context) error {
	if c.EffectiveSender != nil && c.EffectiveSender.User != nil {
		_ = database.BridgeUserEnsure(c.EffectiveSender.User.Id)
	}
	text := "<b>WaTgBridge Main Bot</b>\n\n"
	text += "<b>What we store</b> (on the server that runs this bot): bridge registry (Telegram user id, bridge bot tokens, target chat/topic ids, enable flags), and each bridge process keeps its own SQLite for message id mappings and WhatsApp session state (whatsmeow store). We do <b>not</b> mirror your full chat history into that app database.\n\n"
	text += "<b>At rest</b>: if you set <code>WATG_SQLITE_MASTER_KEY</code> (64 hex chars = 32 bytes), SQLite files are encrypted with SQLCipher; the same host must keep that secret to open them. Bridge child processes receive a derived key via environment, not via the generated YAML.\n\n"
	text += "<b>Setup</b>\n"
	text += "1) Supergroup with Topics.\n"
	text += "2) Add the bridge bot with <b>Manage Topics</b>.\n"
	text += "3) <code>/bridge_add …</code> or managed flow below creates <b>General</b>, <b>BotMeta</b>, <b>Calls</b>, and <b>Status</b> topics.\n\n"
	text += "<b>Managed bridge bot</b> (Telegram <a href=\"https://core.telegram.org/bots/features#managed-bots\">managed bots</a>): in @BotFather enable <i>Bot Management Mode</i> for this main bot. Then <code>/bridge_create_bot</code> [label], follow the steps, use <b>Choose group</b> to finish (or <code>/bridge_bind</code> with numbers from group info). <code>/bridge_cancel_managed</code> clears a pending setup.\n\n"
	text += "<b>Chat history archive</b>: <code>/import_history &lt;bridge_id&gt;</code> then send your Telegram Desktop <code>result.json</code> or a zip of the export folder. Rows are stored in the registry SQLite for audit/search; they do <b>not</b> fill WhatsApp↔Telegram id mappings (those only come from live bridged traffic).\n\n"
	text += "<b>Manage</b>\n"
	text += "<code>/bridge_list</code> · <code>/bridge_enable</code> · <code>/bridge_disable</code> · <code>/bridge_delete</code> · <code>/import_history</code> · <code>/bridge_create_bot</code> · <code>/bridge_bind</code>"
	pm := gotgbot.ParseModeHTML
	_, err := b.SendMessage(c.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{ParseMode: pm})
	return err
}

func bridgeAddHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		args := c.Args()
		if len(args) < 3 {
			_, err := b.SendMessage(c.EffectiveChat.Id, "Usage: /bridge_add <bridge_bot_token> <group_id> [label]\n(group_id: digits from the group’s info; format is adjusted automatically.)", nil)
			return err
		}
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		token := strings.TrimSpace(args[1])
		rawChat, err := strconv.ParseInt(strings.TrimSpace(args[2]), 10, 64)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Invalid group id — use digits from the group’s info / profile.", nil)
			return sendErr
		}
		targetChatID := NormalizeTargetChatID(rawChat)
		name := ""
		if len(args) > 3 {
			name = strings.TrimSpace(strings.Join(args[3:], " "))
		}
		if name == "" {
			name, err = database.BridgeNextName(user.Id)
			if err != nil {
				_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to generate bridge name", nil)
				return sendErr
			}
		}
		if err := database.BridgeUserEnsure(user.Id); err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to ensure bridge user", nil)
			return sendErr
		}

		resp, addErr := addBridgeFromCredentials(b, manager, user.Id, token, targetChatID, name)
		if addErr != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, addErr.Error(), nil)
			return sendErr
		}
		_, err = b.SendMessage(c.EffectiveChat.Id, resp, nil)
		return err
	}
}

func tryForumThread(bot *gotgbot.Bot, chatID, threadID int64) error {
	msg, err := bot.SendMessage(chatID, "\u2060", &gotgbot.SendMessageOpts{
		MessageThreadId:     threadID,
		DisableNotification: true,
	})
	if err != nil {
		return err
	}
	_, _ = bot.DeleteMessage(chatID, msg.MessageId, nil)
	return nil
}

func bridgeListHandler(b *gotgbot.Bot, c *ext.Context) error {
	user := c.EffectiveSender.User
	if user == nil {
		return nil
	}
	bridges, err := database.BridgeListByOwner(user.Id)
	if err != nil {
		_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to list bridges", nil)
		return sendErr
	}
	if len(bridges) == 0 {
		_, sendErr := b.SendMessage(c.EffectiveChat.Id, "No bridges yet. Use /bridge_add", nil)
		return sendErr
	}
	var sb strings.Builder
	for _, bridge := range bridges {
		status := "disabled"
		if bridge.Enabled {
			status = "enabled"
		}
		sb.WriteString(fmt.Sprintf("ID %d | %s | %s | chat %d\n", bridge.ID, bridge.Name, status, bridge.TelegramTargetChat))
	}
	_, err = b.SendMessage(c.EffectiveChat.Id, sb.String(), nil)
	return err
}

func bridgeEnableHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		return toggleBridgeEnabled(b, c, manager, true)
	}
}

func bridgeDisableHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		return toggleBridgeEnabled(b, c, manager, false)
	}
}

func toggleBridgeEnabled(b *gotgbot.Bot, c *ext.Context, manager *bridge.Manager, enabled bool) error {
	user := c.EffectiveSender.User
	if user == nil {
		return nil
	}
	args := c.Args()
	if len(args) < 2 {
		_, err := b.SendMessage(c.EffectiveChat.Id, "Usage: /bridge_enable <id> or /bridge_disable <id>", nil)
		return err
	}
	id64, err := strconv.ParseUint(strings.TrimSpace(args[1]), 10, 64)
	if err != nil {
		_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Invalid bridge id", nil)
		return sendErr
	}
	bridgeID := uint(id64)
	if err := database.BridgeSetEnabled(user.Id, bridgeID, enabled); err != nil {
		_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to update bridge", nil)
		return sendErr
	}
	bridgeRecord, err := database.BridgeGetByID(user.Id, bridgeID)
	if err != nil {
		_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Bridge not found", nil)
		return sendErr
	}
	if enabled {
		if err := manager.StartBridge(bridgeRecord); err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to start runtime: "+err.Error(), nil)
			return sendErr
		}
	} else {
		if err := manager.StopBridge(bridgeID); err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to stop runtime: "+err.Error(), nil)
			return sendErr
		}
	}
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	_, err = b.SendMessage(c.EffectiveChat.Id, "Bridge "+status, nil)
	return err
}

func bridgeDeleteHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		args := c.Args()
		if len(args) < 2 {
			_, err := b.SendMessage(c.EffectiveChat.Id, "Usage: /bridge_delete <id>", nil)
			return err
		}
		id64, err := strconv.ParseUint(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Invalid bridge id", nil)
			return sendErr
		}
		bridgeID := uint(id64)
		_ = manager.StopBridge(bridgeID)
		if err := database.BridgeDelete(user.Id, bridgeID); err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Failed to delete bridge", nil)
			return sendErr
		}
		_, err = b.SendMessage(c.EffectiveChat.Id, "Bridge deleted", nil)
		return err
	}
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate")
}
