package mainbot

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"watgbridge/bridge"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// mbp:<targetChatID> — pending managed bridge token stays in DB (owner-scoped).
const managedBindProceedPrefix = "mbp:"

func managedBindProceedCallbackData(targetChatID int64) string {
	return managedBindProceedPrefix + strconv.FormatInt(targetChatID, 10)
}

func sendManagedBindRetryPrompt(b *gotgbot.Bot, ownerUserID int64, targetChatID int64, addErr error) error {
	title := html.EscapeString(addErr.Error())
	body := "Double-check that you've added the <b>bridge bot</b> to this group, turned on <b>Topics</b> (forum), " +
		"and made the bot an <b>administrator</b> with <b>Manage topics</b> enabled.\n\n" +
		"When you're done, tap <b>I'm done! Proceed</b> below to retry."
	text := fmt.Sprintf("<b>%s</b>\n\n%s", title, body)
	cb := managedBindProceedCallbackData(targetChatID)
	if len(cb) > 64 {
		_, e := b.SendMessage(ownerUserID, addErr.Error()+"\n\n(callback data too long; use /bridge_bind with the group id)", nil)
		return e
	}
	_, err := b.SendMessage(ownerUserID, text, &gotgbot.SendMessageOpts{
		ParseMode: gotgbot.ParseModeHTML,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{{Text: "I'm done! Proceed", CallbackData: cb}},
			},
		},
	})
	return err
}

func managedBindProceedCallbackFilter(cq *gotgbot.CallbackQuery) bool {
	return cq != nil && strings.HasPrefix(cq.Data, managedBindProceedPrefix)
}

func managedBindProceedHandler(manager *bridge.Manager) handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		cq := c.CallbackQuery
		if cq == nil || cq.From.Id == 0 {
			return nil
		}
		raw := strings.TrimPrefix(cq.Data, managedBindProceedPrefix)
		targetChatID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Invalid button data", ShowAlert: true})
			return nil
		}
		if cq.Message != nil {
			if cq.Message.GetChat().Type != gotgbot.ChatTypePrivate {
				_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Use this bot in private chat.", ShowAlert: true})
				return nil
			}
		}
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{})
		from := cq.From
		return completePendingManagedBind(b, manager, &from, targetChatID, "")
	}
}
