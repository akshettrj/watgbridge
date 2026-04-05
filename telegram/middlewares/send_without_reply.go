package middlewares

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type sendWithoutReplyBotClient struct {
	gotgbot.BotClient
}

func (b *sendWithoutReplyBotClient) RequestWithContext(ctx context.Context,
	token string, method string, params map[string]any,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if strings.HasPrefix(method, "send") || method == "copyMessage" {
		if params == nil {
			params = make(map[string]any)
		}
		params["allow_sending_without_reply"] = true
	}

	return b.BotClient.RequestWithContext(ctx, token, method, params, opts)
}

func SendWithoutReply(b gotgbot.BotClient) gotgbot.BotClient {
	return &sendWithoutReplyBotClient{b}
}
