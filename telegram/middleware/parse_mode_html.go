package middlewares

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type parseModeHTMLBotClient struct {
	gotgbot.BotClient
}

func (b *parseModeHTMLBotClient) RequestWithContext(ctx context.Context, method string, params map[string]string, data map[string]gotgbot.NamedReader, opts *gotgbot.RequestOpts) (json.RawMessage, error) {
	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") || method == "copyMessage" {
		params["parse_mode"] = "html"
	}

	val, err := b.BotClient.RequestWithContext(ctx, method, params, data, opts)
	if err != nil {
		fmt.Println("warning, got an error:", err)
	}
	return val, err
}

func ParseAsHTML(b gotgbot.BotClient) gotgbot.BotClient {
	return &parseModeHTMLBotClient{b}
}
