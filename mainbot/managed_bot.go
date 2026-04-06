package mainbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"watgbridge/bridge"
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

func handleManagedBotUpdate(b *gotgbot.Bot, manager *bridge.Manager, upd *managedBotUpdated) error {
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
		"Next: I’ll send a <b>unique pairing link</b> for <b>%s</b>. Open it in Telegram (same account as here). "+
		"In that chat you’ll confirm pairing and tap <b>"+btnChooseGroup+"</b> to pick your forum and grant this bot <b>Manage topics</b>.\n\n"+
		"<i>Pairing</i> is tied to your Telegram account until the group is linked.",
		hint, hint)
	_, err = b.SendMessage(ownerID, text, &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
	if err != nil {
		return err
	}
	return sendManagedBridgePairingLink(b, manager, ownerID)
}
