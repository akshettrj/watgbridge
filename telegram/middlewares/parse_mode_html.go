package middlewares

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type parseModeHTMLBotClient struct {
	gotgbot.BotClient
}

func (b *parseModeHTMLBotClient) RequestWithContext(ctx context.Context,
	token string, method string, params map[string]any,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") {
		if params == nil {
			params = make(map[string]any)
		}
		params["parse_mode"] = "HTML"
	}

	return b.BotClient.RequestWithContext(ctx, token, method, params, opts)
}

func ParseAsHTML(b gotgbot.BotClient) gotgbot.BotClient {
	return &parseModeHTMLBotClient{b}
}
