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
	token string, method string, params map[string]any,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if shouldDisablePreview(params, method) {
		params["disable_web_page_preview"] = "true"
	}

	return b.BotClient.RequestWithContext(ctx, token, method, params, opts)
}

func DisableWebPagePreview(b gotgbot.BotClient) gotgbot.BotClient {
	return &disableWebPagePreviewBotClient{b}
}

func shouldDisablePreview(params map[string]any, method string) bool {
	if !strings.HasPrefix(method, "send") && !strings.HasPrefix(method, "edit") {
		return false
	}
	_, ok := params["link_preview_options"]
	return !ok
}
