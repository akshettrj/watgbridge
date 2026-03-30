package telegram

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// LogLaunchVersion sends "Launched • version: …" as a DM from the main (control) bot to telegram.owner_id.
// It does not post to bridge target groups. If main_bot_token or owner_id is unset, it skips (no-op).
func LogLaunchVersion() {
	if os.Getenv("WATG_BRIDGE_ID") != "" {
		return // multi-mode bridge child; notification is sent by parent only
	}
	cfg := state.State.Config
	token := strings.TrimSpace(cfg.Telegram.MainBotToken)
	if token == "" {
		state.State.Logger.Debug("launch version: telegram.main_bot_token not set, skipping")
		return
	}
	if cfg.Telegram.OwnerID == 0 {
		state.State.Logger.Debug("launch version: telegram.owner_id not set, skipping")
		return
	}
	bot, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{Client: http.Client{}},
	})
	if err != nil {
		state.State.Logger.Warn("launch version: failed to create main bot client", zap.Error(err))
		return
	}
	msg := fmt.Sprintf("Launched • version: <code>%s</code>", state.WATGBRIDGE_VERSION)
	_, err = bot.SendMessage(cfg.Telegram.OwnerID, msg, &gotgbot.SendMessageOpts{
		ParseMode: gotgbot.ParseModeHTML,
	})
	if err != nil {
		state.State.Logger.Warn("launch version: failed to send DM", zap.Error(err))
	}
}
