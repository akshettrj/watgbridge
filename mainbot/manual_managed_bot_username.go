package mainbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// handleManualManagedBotUsernameEntry resolves @username / t.me link via getChat + getManagedBotToken, then continues bind.
func handleManualManagedBotUsernameEntry(b *gotgbot.Bot, ownerUserID int64, text string) error {
	handle, err := parseTelegramBotUsernameInput(text)
	if err != nil {
		_, e := b.SendMessage(ownerUserID,
			"Could not read a bot username from that. Send something like <code>@myveryownwatgbridgebot</code> or a <code>t.me/...</code> link.",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	raw, err := b.RequestWithContext(context.Background(), "getChat", map[string]any{
		"chat_id": "@" + handle,
	}, nil)
	if err != nil {
		_, e := b.SendMessage(ownerUserID,
			"Could not load <code>@"+handle+"</code>. Check the username (BotFather, public username).",
			&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML, ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	var chat gotgbot.ChatFullInfo
	if err := json.Unmarshal(raw, &chat); err != nil || chat.Id == 0 {
		_, e := b.SendMessage(ownerUserID, "Unexpected reply from Telegram for that username.", &gotgbot.SendMessageOpts{ReplyMarkup: mainBotManualBotUsernamePromptReplyKeyboard()})
		return e
	}
	token, err := tgGetManagedBotToken(b, chat.Id)
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
	_, err = b.SendMessage(ownerUserID,
		"Using <b>@"+handle+"</b>. Next: pick the forum group — I’ll join briefly, try to add this bot as admin with <b>Manage topics</b>, then leave.",
		&gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
	if err != nil {
		return err
	}
	return sendManagedBridgeChooseGroupPrompt(b, ownerUserID)
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
