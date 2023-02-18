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
	method string, params map[string]string,
	data map[string]gotgbot.NamedReader,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") {
		params["parse_mode"] = "html"
	}

	return b.BotClient.RequestWithContext(ctx, method, params, data, opts)
}

func ParseAsHTML(b gotgbot.BotClient) gotgbot.BotClient {
	return &parseModeHTMLBotClient{b}
}
