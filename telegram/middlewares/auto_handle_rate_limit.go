package middlewares

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type autoHandleRateLimitBotClient struct {
	gotgbot.BotClient
}

func (b *autoHandleRateLimitBotClient) RequestWithContext(ctx context.Context,
	token string, method string, params map[string]string,
	data map[string]gotgbot.NamedReader,
	opts *gotgbot.RequestOpts) (json.RawMessage, error) {

	if strings.HasPrefix(method, "send") || strings.HasPrefix(method, "edit") {
		params["parse_mode"] = "html"
	}

	for {
		response, err := b.BotClient.RequestWithContext(ctx, token, method, params, data, opts)
		if err == nil {
			return response, err
		}

		tgError, ok := err.(*gotgbot.TelegramError)
		if !ok {
			return response, err
		}

		if tgError.Code == 429 {
			fields := strings.Fields(tgError.Description)
			timeToSleep, _ := strconv.ParseInt(fields[len(fields)-1], 10, 64)
			log.Printf("[auto_handle_rate_limit] sleeping for %v seconds", timeToSleep)
			time.Sleep(time.Second * time.Duration(timeToSleep))
			continue
		}

		return response, err
	}
}

func AutoHandleRateLimit(b gotgbot.BotClient) gotgbot.BotClient {
	return &autoHandleRateLimitBotClient{b}
}
