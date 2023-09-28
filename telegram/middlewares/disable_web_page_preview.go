package middlewares

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type disableWebPagePreviewBotClient struct {
	gotgbot.BotClient
}

func (b *disableWebPagePreviewBotClient) RequestWithContext(ctx context.Context,
	token string, method string, params map[string]string,
	data map[string]gotgbot.NamedReader,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") {
		params["disable_web_page_preview"] = "true"
	}

	return b.BotClient.RequestWithContext(ctx, token, method, params, data, opts)
}

func DisableWebPagePreview(b gotgbot.BotClient) gotgbot.BotClient {
	return &disableWebPagePreviewBotClient{b}
}
