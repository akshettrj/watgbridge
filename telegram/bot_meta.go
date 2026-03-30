package telegram

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// LogLaunchVersion sends "Launched • version: …" from the main (control) bot to every registry user
// (bridge_users + bridge owners), plus config owner_id and sudo_users_id (deduped). Skips bridge children.
func LogLaunchVersion() {
	if os.Getenv("WATG_BRIDGE_ID") != "" {
		return
	}
	cfg := state.State.Config
	token := strings.TrimSpace(cfg.Telegram.MainBotToken)
	if token == "" {
		state.State.Logger.Debug("launch version: telegram.main_bot_token not set, skipping")
		return
	}
	bot, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{Client: http.Client{}},
	})
	if err != nil {
		state.State.Logger.Warn("launch version: failed to create main bot client", zap.Error(err))
		return
	}
	ver := strings.TrimSpace(state.WATGBRIDGE_VERSION)
	if ver == "" {
		ver = "unknown"
	}
	msg := fmt.Sprintf("Launched • version: <code>%s</code>", ver)

	seen := make(map[int64]struct{})
	var recipients []int64
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		recipients = append(recipients, id)
	}
	if ids, err := database.BridgeRegistryNotifyUserIDs(); err != nil {
		state.State.Logger.Warn("launch version: failed to list registry users", zap.Error(err))
	} else {
		for _, id := range ids {
			add(id)
		}
	}
	add(cfg.Telegram.OwnerID)
	for _, id := range cfg.Telegram.SudoUsersID {
		add(id)
	}
	if len(recipients) == 0 {
		state.State.Logger.Info("launch version: no recipients (empty registry and no owner/sudo in config)")
		return
	}
	opts := &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML}
	for _, chatID := range recipients {
		if _, err := bot.SendMessage(chatID, msg, opts); err != nil {
			state.State.Logger.Warn("launch version: failed to send DM",
				zap.Int64("telegram_user_id", chatID),
				zap.Error(err))
		}
	}
}
