package mainbot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"

	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// handleManualManagedBotUsernameEntry resolves a managed bridge bot via getManagedBotToken.
// Input can be @username / t.me (getChat → id) or numeric bot user id (id:… / 8+ digits) when getChat is unreliable.
func handleManualManagedBotUsernameEntry(b *gotgbot.Bot, ownerUserID int64, text string) error {
	botUserID, handle, err := parseManualBotIdentity(text)
	if err != nil {
		_, e := b.SendMessage(ownerUserID,
			"Could not read that. Send <code>@username</code>, <code>t.me/…</code>, or the bot’s numeric id: <code>id:1234567890</code> (from BotFather / <code>getMe</code>).",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	if botUserID == 0 {
		raw, err := b.RequestWithContext(context.Background(), "getChat", map[string]any{
			"chat_id": "@" + handle,
		}, nil)
		if err != nil {
			_, e := b.SendMessage(ownerUserID,
				"Could not load <code>@"+handle+"</code> via Telegram (getChat). If the @username is correct, send the bot’s numeric id instead, e.g. <code>id:1234567890</code> — you can copy it from BotFather or <code>/getid</code> bots.",
				&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
			return e
		}
		var chat gotgbot.ChatFullInfo
		if err := json.Unmarshal(raw, &chat); err != nil || chat.Id == 0 {
			_, e := b.SendMessage(ownerUserID, "Unexpected reply from Telegram for that username.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
			return e
		}
		botUserID = chat.Id
	}
	token, err := tgGetManagedBotToken(b, botUserID)
	if err != nil || strings.TrimSpace(token) == "" {
		_, e := b.SendMessage(ownerUserID,
			"That account isn’t a <b>managed bridge bot</b> for this main bot (no token). Create one with <b>Create new bridge bot</b> or add a bridge with <code>/bridge_add &lt;token&gt; …</code>.",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	bridgeBot, err := gotgbot.NewBot(strings.TrimSpace(token), nil)
	if err != nil {
		_, e := b.SendMessage(ownerUserID, "Invalid token from registry.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	me, err := bridgeBot.GetMe(nil)
	if err != nil || me.Id == 0 {
		_, e := b.SendMessage(ownerUserID, "Could not verify that bot’s token.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	if !me.IsBot {
		_, e := b.SendMessage(ownerUserID, "That user is not a bot.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	if ownerHasActiveBridgeForToken(ownerUserID, token) {
		_, e := b.SendMessage(ownerUserID,
			"That bridge bot is <b>already linked</b> to an active bridge. Remove or rename the existing bridge first, or pick another bot.",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	if err := database.BridgeUserEnsure(ownerUserID); err != nil {
		_, e := b.SendMessage(ownerUserID, "Failed to register user: "+err.Error(), nil)
		return e
	}
	if err := database.BridgeManagedBotUpsert(ownerUserID, me.Id, token, ""); err != nil {
		_, e := b.SendMessage(ownerUserID, "Failed to save bot: "+err.Error(), nil)
		return e
	}
	if err := database.BridgePendingManagedUpsert(ownerUserID, me.Id, token, ""); err != nil {
		_, e := b.SendMessage(ownerUserID, "Failed to set pending bridge: "+err.Error(), nil)
		return e
	}
	mainBotReplyFlow.Delete(ownerUserID)
	label := formatBotMentionForOwner(handle, me)
	_, err = b.SendMessage(ownerUserID,
		"Using "+label+". Next: pick the forum group — I’ll join briefly, try to add this bot as admin with <b>Manage topics</b>, then leave.",
		&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
	if err != nil {
		return err
	}
	return sendManagedBridgeChooseGroupPrompt(b, ownerUserID)
}

func formatBotMentionForOwner(usernameHandle string, me *gotgbot.User) string {
	u := strings.TrimSpace(usernameHandle)
	if u != "" {
		return "<b>@" + html.EscapeString(u) + "</b>"
	}
	if me != nil && strings.TrimSpace(me.Username) != "" {
		return "<b>@" + html.EscapeString(strings.TrimSpace(me.Username)) + "</b>"
	}
	if me != nil && me.Id != 0 {
		return fmt.Sprintf("<code>id:%d</code>", me.Id)
	}
	return "that bot"
}

// parseManualBotIdentity returns botUserID if the user sent an id (skip getChat); otherwise username handle for getChat.
func parseManualBotIdentity(text string) (botUserID int64, username string, err error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, "", fmt.Errorf("empty")
	}
	low := strings.ToLower(t)
	if strings.HasPrefix(low, "id:") {
		rest := strings.TrimSpace(t[3:])
		id, e := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if e != nil || id <= 0 {
			return 0, "", fmt.Errorf("bad id")
		}
		return id, "", nil
	}
	if strings.HasPrefix(low, "id ") {
		rest := strings.TrimSpace(t[2:])
		id, e := strconv.ParseInt(rest, 10, 64)
		if e != nil || id <= 0 {
			return 0, "", fmt.Errorf("bad id")
		}
		return id, "", nil
	}
	if strings.Contains(t, "t.me/") {
		u, e := parseTelegramBotUsernameInput(text)
		if e != nil {
			return 0, "", e
		}
		return 0, u, nil
	}
	t2 := strings.TrimPrefix(t, "@")
	t2 = strings.TrimSpace(t2)
	t2 = strings.TrimSuffix(t2, "...")
	t2 = strings.TrimSpace(t2)
	if len(t2) >= 8 && isOnlyDigits(t2) {
		id, e := strconv.ParseInt(t2, 10, 64)
		if e != nil || id <= 0 {
			return 0, "", fmt.Errorf("bad id")
		}
		return id, "", nil
	}
	u, e := parseTelegramBotUsernameInput(text)
	if e != nil {
		return 0, "", e
	}
	return 0, u, nil
}

func isOnlyDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseTelegramBotUsernameInput(text string) (handle string, err error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", fmt.Errorf("empty")
	}
	if strings.Contains(t, "t.me/") {
		idx := strings.Index(t, "t.me/")
		rest := t[idx+5:]
		rest = strings.TrimPrefix(rest, "/")
		rest = strings.TrimSpace(rest)
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			rest = rest[:i]
		}
		t = rest
	}
	t = strings.TrimPrefix(t, "@")
	t = strings.TrimSpace(t)
	t = strings.TrimSuffix(t, "...")
	t = strings.TrimSpace(t)
	if len(t) < 3 || len(t) > 32 {
		return "", fmt.Errorf("length")
	}
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", fmt.Errorf("char")
	}
	return strings.ToLower(t), nil
}

func ownerHasActiveBridgeForToken(ownerUserID int64, token string) bool {
	want := database.HashBridgeToken(token)
	bridges, err := database.BridgeListByOwner(ownerUserID)
	if err != nil {
		return false
	}
	for _, br := range bridges {
		if database.HashBridgeToken(br.BridgeBotToken) == want {
			return true
		}
	}
	return false
}
