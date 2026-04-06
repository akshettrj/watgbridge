package mainbot

import (
	"context"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"go.uber.org/zap"
)

// managedBridgePollers runs getUpdates for managed bridge bot tokens so /start deep links and
// KeyboardButtonRequestChat (chat_shared) work before the bridge child process exists.
var managedBridgePollers sync.Map // bridge bot token string -> context.CancelFunc

func stopManagedBridgePollerForBridgeToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if v, ok := managedBridgePollers.LoadAndDelete(token); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
}

// stopManagedBridgePollerForOwner stops polling for the pending managed bridge token (if any).
func stopManagedBridgePollerForOwner(ownerUserID int64) {
	row, err := database.BridgePendingManagedGet(ownerUserID)
	if err != nil || row == nil {
		return
	}
	stopManagedBridgePollerForBridgeToken(row.BridgeBotToken)
}

// EnsureManagedBridgePoller starts a single long-poll loop for this bridge token until cancelled.
func EnsureManagedBridgePoller(bridgeBotToken string, mainBot *gotgbot.Bot, manager *bridge.Manager) {
	bridgeBotToken = strings.TrimSpace(bridgeBotToken)
	if bridgeBotToken == "" || mainBot == nil || manager == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, loaded := managedBridgePollers.LoadOrStore(bridgeBotToken, cancel); loaded {
		cancel()
		return
	}
	go runManagedBridgePoller(ctx, cancel, bridgeBotToken, mainBot, manager)
}

func runManagedBridgePoller(ctx context.Context, cancel context.CancelFunc, bridgeToken string, mainBot *gotgbot.Bot, manager *bridge.Manager) {
	defer func() {
		cancel()
		managedBridgePollers.CompareAndDelete(bridgeToken, cancel)
	}()

	bot, err := gotgbot.NewBot(bridgeToken, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{Client: http.Client{}},
	})
	if err != nil {
		state.State.Logger.Warn("managed bridge poller: NewBot", zap.Error(err))
		return
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		UnhandledErrFunc: func(err error) {
			state.State.Logger.Debug("managed bridge dispatcher", zap.Error(err))
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})
	dispatcher.AddHandler(handlers.NewCommand("start", managedBridgeStartHandler(bridgeToken, mainBot, manager)))
	dispatcher.AddHandler(handlers.NewCallback(managedBindProceedCallbackFilter, managedBindProceedHandler(mainBot, manager)))
	dispatcher.AddHandler(handlers.NewMessage(managedBridgeChatSharedFilter, managedBridgeChatSharedHandler(mainBot, manager)))

	updater := ext.NewUpdater(dispatcher, &ext.UpdaterOpts{
		UnhandledErrFunc: func(err error) {
			state.State.Logger.Debug("managed bridge updater", zap.Error(err))
		},
	})

	if err := updater.StartPolling(bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout:        9,
			AllowedUpdates: []string{"message", "callback_query"},
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 10 * time.Second,
			},
		},
	}); err != nil {
		state.State.Logger.Warn("managed bridge poller: StartPolling", zap.Error(err))
		return
	}

	go func() {
		<-ctx.Done()
		updater.Stop()
	}()

	updater.Idle()
}

func managedBridgeStartHandler(bridgeToken string, mainBot *gotgbot.Bot, manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		if c.EffectiveChat.Type != gotgbot.ChatTypePrivate {
			return nil
		}
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		args := c.Args()
		payload := ""
		if len(args) > 1 {
			payload = strings.TrimSpace(strings.Join(args[1:], " "))
		}
		if payload == "" {
			_, err := b.SendMessage(c.EffectiveChat.Id,
				"Open this bot from the <b>pairing link</b> the main WaTgBridge bot sends after you create or pick a managed bridge bot.",
				&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
			return err
		}
		row, err := database.BridgePendingManagedGetByPairToken(payload)
		if err != nil || row == nil {
			_, e := b.SendMessage(c.EffectiveChat.Id,
				"Unknown or expired link. Ask the main bot for a new pairing link.",
				nil)
			return e
		}
		if row.OwnerUserID != user.Id {
			_, e := b.SendMessage(c.EffectiveChat.Id,
				"This pairing link is for another Telegram account.",
				nil)
			return e
		}
		if strings.TrimSpace(row.BridgeBotToken) != strings.TrimSpace(bridgeToken) {
			_, e := b.SendMessage(c.EffectiveChat.Id, "Internal mismatch — request a new link from the main bot.", nil)
			return e
		}
		me, err := b.GetMe(nil)
		if err != nil || me.Id == 0 || me.Id != row.ManagedBotUserID {
			_, e := b.SendMessage(c.EffectiveChat.Id, "Wrong bot — use the link from the main bot for this bridge.", nil)
			return e
		}

		rid, err := randomManagedRequestID()
		if err != nil {
			_, e := b.SendMessage(c.EffectiveChat.Id, "Could not build request: "+err.Error(), nil)
			return e
		}
		un := strings.TrimSpace(me.Username)
		if un != "" {
			un = "@" + html.EscapeString(un)
		} else {
			un = html.EscapeString(me.FirstName)
		}
		_, err = b.SendMessage(c.EffectiveChat.Id,
			"<b>Hi — you're paired for this bridge bot.</b>\n\n"+
				"Tap <b>"+btnChooseGroup+"</b> below, then pick your <b>supergroup with Topics</b>. "+
				"When Telegram asks, grant this bot admin with <b>Manage topics</b> enabled.\n\n"+
				"Bot: "+un,
			&gotgbot.SendMessageOpts{
				ParseMode:   gotgbot.ParseModeHTML,
				ReplyMarkup: ManagedBridgeChooseGroupReplyKeyboard(int64(rid)),
			})
		return err
	}
}

func managedBridgeChatSharedFilter(m *gotgbot.Message) bool {
	if m == nil || m.ChatShared == nil || m.From == nil {
		return false
	}
	if m.Chat.Type != gotgbot.ChatTypePrivate {
		return false
	}
	_, err := database.BridgePendingManagedGet(m.From.Id)
	return err == nil
}

func managedBridgeChatSharedHandler(mainBot *gotgbot.Bot, manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		msg := c.Message
		if msg == nil || msg.ChatShared == nil || msg.From == nil {
			return nil
		}
		user := msg.From
		chatID := NormalizeTargetChatID(msg.ChatShared.ChatId)
		state.State.Logger.Info("managed bind: chat_shared received (bridge bot)",
			zap.Int64("owner_user_id", user.Id),
			zap.Int64("target_chat_id", chatID),
			zap.Int64("raw_chat_shared_chat_id", msg.ChatShared.ChatId))
		return completePendingManagedBind(mainBot, manager, user, chatID, "", &ManagedBindOpts{
			NotifyBot: b,
		})
	}
}
