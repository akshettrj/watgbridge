package mainbot

import (
	"math"
	"net/http"
	"strings"
	"time"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

func newBridgeBotAPIClient(bridgeBotToken string) (*gotgbot.Bot, error) {
	token := strings.TrimSpace(bridgeBotToken)
	if token == "" {
		return nil, nil
	}
	apiURL := strings.TrimSpace(state.State.Config.Telegram.APIURL)
	if apiURL == "" {
		apiURL = gotgbot.DefaultAPIURL
	}
	return gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
			DefaultRequestOpts: &gotgbot.RequestOpts{
				APIURL:  apiURL,
				Timeout: time.Duration(math.MaxInt64),
			},
		},
		DisableTokenCheck: true,
	})
}

// bridgeBotLeaveTargetChat removes the bridge bot from its target supergroup (best-effort; ignores errors).
func bridgeBotLeaveTargetChat(bridgeBotToken string, targetChatID int64) {
	if targetChatID == 0 {
		return
	}
	bot, err := newBridgeBotAPIClient(bridgeBotToken)
	if err != nil {
		state.State.Logger.Debug("bridge delete: bridge bot client", zap.Error(err))
		return
	}
	if bot == nil {
		return
	}
	if _, err := bot.LeaveChat(targetChatID, nil); err != nil {
		state.State.Logger.Debug("bridge delete: leaveChat (ok if bot already left or token revoked)",
			zap.Int64("chat_id", targetChatID),
			zap.Error(err))
	}
}
