package middlewares

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type sendWithoutReplyBotClient struct {
	gotgbot.BotClient
}

func (b *sendWithoutReplyBotClient) RequestWithContext(ctx context.Context, method string, params map[string]string, data map[string]gotgbot.NamedReader, opts *gotgbot.RequestOpts) (json.RawMessage, error) {
	if strings.HasPrefix(method, "send") || method == "copyMessage" {
		params["allow_sending_without_reply"] = "true"
	}

	val, err := b.BotClient.RequestWithContext(ctx, method, params, data, opts)
	if err != nil {
		fmt.Println("warning, got an error:", err)
	}
	return val, err
}

func SendWithoutReply(b gotgbot.BotClient) gotgbot.BotClient {
	return &sendWithoutReplyBotClient{b}
}
