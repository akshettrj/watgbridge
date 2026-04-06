package mainbot

import (
	"context"
	"errors"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

var (
	// ErrMainBotNeedsBootstrapRights is returned when the main bot is in the target group but lacks rights to promote the bridge bot.
	ErrMainBotNeedsBootstrapRights = errors.New("main bot needs admin: invite users, add admins, and manage topics")
)

// maybeBootstrapBridgeInTarget runs when the main bot is already a member/admin of targetChatID: tries to add/promote the bridge bot.
// If the main bot is not in the chat, it no-ops (legacy manual-add flow).
func maybeBootstrapBridgeInTarget(mainBot *gotgbot.Bot, bridgeBotToken string, bridgeUserID int64, targetChatID int64) error {
	me, err := mainBot.GetMe(nil)
	if err != nil {
		return nil
	}
	mainMem, err := mainBot.GetChatMember(targetChatID, me.Id, nil)
	if err != nil {
		return nil
	}
	mainMerged := mainMem.MergeChatMember()
	if mainMerged.Status != "administrator" && mainMerged.Status != "creator" {
		return nil
	}
	if mainMerged.Status == "administrator" {
		if !mainMerged.CanPromoteMembers || !mainMerged.CanInviteUsers || !mainMerged.CanManageTopics {
			return ErrMainBotNeedsBootstrapRights
		}
	}

	bridgeBot, err := gotgbot.NewBot(strings.TrimSpace(bridgeBotToken), nil)
	if err != nil {
		return nil
	}
	bMe, err := bridgeBot.GetMe(nil)
	if err != nil || bMe.Id == 0 {
		return nil
	}
	if bMe.Id != bridgeUserID {
		bridgeUserID = bMe.Id
	}

	brMem, err := mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
	if err != nil {
		_ = tryInviteChatMemberRaw(mainBot, targetChatID, bridgeUserID)
		brMem, err = mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
		if err != nil {
			return ErrBridgeBotNotGroupMember
		}
	}
	brMerged := brMem.MergeChatMember()
	if brMerged.Status == "creator" || (brMerged.Status == "administrator" && brMerged.CanManageTopics) {
		return nil
	}
	if brMerged.Status != "administrator" && brMerged.Status != "member" {
		_ = tryInviteChatMemberRaw(mainBot, targetChatID, bridgeUserID)
		brMem, err = mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
		if err != nil {
			return ErrBridgeBotNotGroupMember
		}
		brMerged = brMem.MergeChatMember()
		if brMerged.Status == "left" || brMerged.Status == "kicked" {
			return ErrBridgeBotNotGroupMember
		}
	}

	_, err = mainBot.RequestWithContext(context.Background(), "promoteChatMember", map[string]any{
		"chat_id":                targetChatID,
		"user_id":                bridgeUserID,
		"is_anonymous":           false,
		"can_manage_chat":        true,
		"can_delete_messages":    false,
		"can_manage_video_chats": false,
		"can_restrict_members":   false,
		"can_promote_members":    false,
		"can_change_info":        false,
		"can_invite_users":       false,
		"can_post_stories":       false,
		"can_edit_stories":       false,
		"can_delete_stories":     false,
		"can_pin_messages":       false,
		"can_manage_topics":      true,
	}, nil)
	return err
}

func tryInviteChatMemberRaw(bot *gotgbot.Bot, chatID, userID int64) error {
	_, err := bot.RequestWithContext(context.Background(), "inviteChatMember", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}, nil)
	return err
}

func mainBotTryLeaveTarget(mainBot *gotgbot.Bot, targetChatID int64) {
	if mainBot == nil || targetChatID == 0 {
		return
	}
	_, _ = mainBot.LeaveChat(targetChatID, nil)
}
