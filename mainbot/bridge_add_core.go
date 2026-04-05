package mainbot

import (
	"fmt"
	"strings"

	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/telegram"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// addBridgeFromCredentials validates token, forum group, provisions topics, persists the bridge, and starts the runtime.
func addBridgeFromCredentials(b *gotgbot.Bot, manager *bridge.Manager, ownerUserID int64, token string, targetChatID int64, name string) (resp string, err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		var genErr error
		name, genErr = database.BridgeNextName(ownerUserID)
		if genErr != nil {
			return "", fmt.Errorf("generate bridge name: %w", genErr)
		}
	}
	if err := database.BridgeUserEnsure(ownerUserID); err != nil {
		return "", fmt.Errorf("ensure bridge user: %w", err)
	}

	bridgeBot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		return "", fmt.Errorf("invalid bridge token")
	}
	if _, err := bridgeBot.GetMe(nil); err != nil {
		return "", fmt.Errorf("token validation failed")
	}
	chat, err := bridgeBot.GetChat(targetChatID, nil)
	if err != nil {
		return "", fmt.Errorf("bridge bot cannot access target group")
	}
	if !chat.IsForum {
		return "", fmt.Errorf("target group must have Topics enabled")
	}
	member, err := bridgeBot.GetChatMember(targetChatID, bridgeBot.Id, nil)
	if err != nil {
		return "", fmt.Errorf("bridge bot must be in target group as admin")
	}
	merged := member.MergeChatMember()
	if merged.Status != "creator" && (merged.Status != "administrator" || !merged.CanManageTopics) {
		return "", fmt.Errorf("bridge bot needs admin + Manage Topics permission")
	}

	var record *database.Bridge
	var createErr error
	for attempt := 0; attempt < 8; attempt++ {
		waSession, genErr := utils.RandomWhatsAppDeviceLabel()
		if genErr != nil {
			return "", fmt.Errorf("generate WhatsApp device label: %w", genErr)
		}
		record, createErr = database.BridgeCreate(ownerUserID, name, token, targetChatID, waSession, true)
		if createErr == nil {
			break
		}
		if !isUniqueConstraintError(createErr) {
			break
		}
	}
	if createErr != nil {
		return "", fmt.Errorf("create bridge record: %w", createErr)
	}

	general, botMeta, calls, status, provErr := telegram.CreateStandardForumMetaTopics(bridgeBot, targetChatID)
	if provErr != nil {
		_ = database.BridgeProvisionSet(record.ID, 0, 0, 0, 0, "provision_error", provErr.Error())
	} else {
		_ = database.BridgeProvisionSet(record.ID, general, botMeta, calls, status, "ok", "")
	}
	if err := manager.StartBridge(record); err != nil {
		return "", fmt.Errorf("start runtime: %w", err)
	}
	resp = fmt.Sprintf("Bridge enabled.\nID: %d\nLabel: %s\nTarget: %d\nWhatsApp linked device name: %s\n\nManage with /bridge_list /bridge_disable /bridge_delete",
		record.ID, record.Name, record.TelegramTargetChat, record.WaSessionName)
	return resp, nil
}
