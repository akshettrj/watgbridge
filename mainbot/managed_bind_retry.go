package mainbot

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"watgbridge/bridge"
	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

// mbp:<targetChatID> — pending managed bridge token stays in DB (owner-scoped).
const managedBindProceedPrefix = "mbp:"

func managedBindProceedCallbackData(targetChatID int64) string {
	return managedBindProceedPrefix + strconv.FormatInt(targetChatID, 10)
}

func formatManagedBindTargetForumHTML(mainBot *gotgbot.Bot, targetChatID int64) string {
	chat, err := mainBot.GetChat(targetChatID, nil)
	if err != nil || chat == nil {
		return fmt.Sprintf("Forum / group <code>%d</code> <i>(title unavailable — add this main bot to the group or use the id from the group’s profile)</i>", targetChatID)
	}
	title := strings.TrimSpace(chat.Title)
	if title == "" {
		return fmt.Sprintf("<code>%d</code>", targetChatID)
	}
	return fmt.Sprintf("<b>%s</b> · chat <code>%d</code>", html.EscapeString(title), targetChatID)
}

func formatPendingBridgeBotHTML(bridgeBotToken string) string {
	token := strings.TrimSpace(bridgeBotToken)
	if token == "" {
		return "<i>(no token)</i>"
	}
	bridgeBot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return "<i>(invalid bridge bot token)</i>"
	}
	me, err := bridgeBot.GetMe(nil)
	if err != nil {
		return "<i>(could not load bridge bot profile)</i>"
	}
	if u := strings.TrimSpace(me.Username); u != "" {
		return fmt.Sprintf(`<a href="https://t.me/%s">@%s</a>`, html.EscapeString(u), html.EscapeString(u))
	}
	name := strings.TrimSpace(me.FirstName)
	if name == "" {
		name = "Bridge bot"
	}
	return fmt.Sprintf("%s · <code>id:%d</code>", html.EscapeString(name), me.Id)
}

func sendManagedBindRetryPrompt(b *gotgbot.Bot, ownerUserID int64, targetChatID int64, pending *database.BridgePendingManaged, addErr error) error {
	title := html.EscapeString(addErr.Error())
	token := ""
	if pending != nil {
		token = pending.BridgeBotToken
	}
	forumLine := formatManagedBindTargetForumHTML(b, targetChatID)
	botLine := formatPendingBridgeBotHTML(token)
	ctx := fmt.Sprintf(
		"<b>Proceed applies to:</b>\n"+
			"• <b>Forum / group:</b> %s\n"+
			"• <b>Bridge bot:</b> %s\n",
		forumLine,
		botLine,
	)
	if pending != nil && strings.TrimSpace(pending.LabelHint) != "" {
		ctx += fmt.Sprintf("• <b>Label hint:</b> %s\n", html.EscapeString(strings.TrimSpace(pending.LabelHint)))
	}
	body := ctx + "\nDouble-check that you've added <b>that</b> bridge bot to <b>that</b> group, turned on <b>Topics</b> (forum), " +
		"and made the bot an <b>administrator</b> with <b>Manage topics</b> enabled.\n\n" +
		"When you're done, tap <b>I'm done! Proceed</b> below to retry for this chat and bot."
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
