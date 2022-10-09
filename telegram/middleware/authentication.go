package middlewares

import (
	"wa-tg-bridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func CheckAuthorized(b *gotgbot.Bot, c *ext.Context) bool {
	cfg := state.State.Config
	ownerID := cfg.Telegram.OwnerID
	sender := c.EffectiveSender.User

	if sender != nil && sender.Id == ownerID {
		return true
	}

	if c.CallbackQuery != nil {
		b.AnswerCallbackQuery(c.CallbackQuery.Id, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Not authorized",
			ShowAlert: false,
			CacheTime: 60,
		})
	}

	return false
}
