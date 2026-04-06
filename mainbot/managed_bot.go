package mainbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"watgbridge/database"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// pendingManagedLabelHints stores optional /bridge_create_bot [label] until the managed_bot update arrives.
var pendingManagedLabelHints sync.Map // int64 owner user id -> string label hint

// managedBotUpdated mirrors https://core.telegram.org/bots/api#managedbotupdated
type managedBotUpdated struct {
	User *gotgbot.User `json:"user"`
	Bot  *gotgbot.User `json:"bot"`
}

func tgGetManagedBotToken(b *gotgbot.Bot, managedBotUserID int64) (string, error) {
	raw, err := b.RequestWithContext(context.Background(), "getManagedBotToken", map[string]any{
		"user_id": managedBotUserID,
	}, nil)
	if err != nil {
		return "", err
	}
	var token string
	if err := json.Unmarshal(raw, &token); err != nil {
		return "", fmt.Errorf("decode getManagedBotToken result: %w", err)
	}
	return strings.TrimSpace(token), nil
}

func handleManagedBotUpdate(b *gotgbot.Bot, upd *managedBotUpdated) error {
	if upd == nil || upd.User == nil || upd.Bot == nil {
		return nil
	}
	ownerID := upd.User.Id
	managedID := upd.Bot.Id
	if managedID == 0 {
		return nil
	}
	token, err := tgGetManagedBotToken(b, managedID)
	if err != nil {
		_, _ = b.SendMessage(ownerID, "Could not read the new bot token (getManagedBotToken). Enable Bot Management Mode for this main bot in @BotFather, then try again.\n"+err.Error(), nil)
		return err
	}
	if err := database.BridgeUserEnsure(ownerID); err != nil {
		_, _ = b.SendMessage(ownerID, "Failed to register user: "+err.Error(), nil)
		return err
	}
	labelHint := ""
	if v, ok := pendingManagedLabelHints.LoadAndDelete(ownerID); ok {
		if s, ok := v.(string); ok {
			labelHint = strings.TrimSpace(s)
		}
	}
	if err := database.BridgePendingManagedUpsert(ownerID, managedID, token, labelHint); err != nil {
		_, _ = b.SendMessage(ownerID, "Failed to save pending bridge: "+err.Error(), nil)
		return err
	}
	if err := database.BridgeManagedBotUpsert(ownerID, managedID, token, labelHint); err != nil {
		_, _ = b.SendMessage(ownerID, "Failed to save managed bot registry: "+err.Error(), nil)
		return err
	}
	hint := ""
	if un := strings.TrimSpace(upd.Bot.Username); un != "" {
		hint = "@" + un
	} else {
		hint = fmt.Sprintf("id %d", managedID)
	}
	text := fmt.Sprintf("Your bridge bot is ready: <b>%s</b>\n\n"+
		"Next: I’ll send a <b>Choose group</b> button. Pick your <b>forum</b> and add this main bot as admin "+
		"(<b>invite users</b>, <b>add new admins</b>, <b>manage topics</b>) when Telegram asks. "+
		"I’ll try to pull <b>%s</b> in and grant <b>Manage topics</b>, then leave the group.\n\n"+
		"<i>If automatic add fails</i>, add the bridge bot manually and use <b>I’m done! Proceed</b>.\n\n"+
		"<i>Pairing:</i> your bridge bot token stays scoped to you until the group is linked.",
		hint, hint)
	_, err = b.SendMessage(ownerID, text, &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
	if err != nil {
		return err
	}
	return sendManagedBridgeChooseGroupPrompt(b, ownerID)
}
