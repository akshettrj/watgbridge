package mainbot

import (
	"context"
	"errors"
	"strings"
	"time"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

var (
	// ErrMainBotNeedsBootstrapRights is returned when the main bot is in the target group but lacks rights to promote the bridge bot.
	ErrMainBotNeedsBootstrapRights = errors.New("main bot needs admin: invite users, add admins, and manage topics")
)

// maybeBootstrapBridgeInTarget runs when the main bot is already a member/admin of targetChatID: tries to add/promote the bridge bot.
// If the main bot is not in the chat, it no-ops (legacy manual-add flow).
func maybeBootstrapBridgeInTarget(mainBot *gotgbot.Bot, bridgeBotToken string, bridgeUserID int64, targetChatID int64) error {
	log := state.State.Logger
	me, err := mainBot.GetMe(nil)
	if err != nil {
		log.Debug("managed bootstrap: getMe", zap.Error(err))
		return nil
	}
	mainMem, err := getChatMemberWithRetry(mainBot, targetChatID, me.Id, 8, 400*time.Millisecond)
	if err != nil {
		log.Warn("managed bootstrap: main bot not visible in target yet (skipping auto-invite/promote); user can add bridge bot manually",
			zap.Int64("target_chat_id", targetChatID),
			zap.Int64("main_bot_id", me.Id),
			zap.Error(err))
		return nil
	}
	mainMerged := mainMem.MergeChatMember()
	if mainMerged.Status != "administrator" && mainMerged.Status != "creator" {
		log.Warn("managed bootstrap: main bot is not admin/creator in target",
			zap.Int64("target_chat_id", targetChatID),
			zap.String("status", mainMerged.Status))
		return nil
	}
	if mainMerged.Status == "administrator" {
		if !mainMerged.CanPromoteMembers || !mainMerged.CanInviteUsers || !mainMerged.CanManageTopics {
			log.Warn("managed bootstrap: main bot admin rights insufficient for auto-setup",
				zap.Int64("target_chat_id", targetChatID),
				zap.Bool("can_promote_members", mainMerged.CanPromoteMembers),
				zap.Bool("can_invite_users", mainMerged.CanInviteUsers),
				zap.Bool("can_manage_topics", mainMerged.CanManageTopics))
			return ErrMainBotNeedsBootstrapRights
		}
	}

	bridgeBot, err := gotgbot.NewBot(strings.TrimSpace(bridgeBotToken), nil)
	if err != nil {
		log.Debug("managed bootstrap: bridge bot client", zap.Error(err))
		return nil
	}
	bMe, err := bridgeBot.GetMe(nil)
	if err != nil || bMe.Id == 0 {
		log.Warn("managed bootstrap: bridge GetMe failed", zap.Error(err))
		return nil
	}
	if bMe.Id != bridgeUserID {
		bridgeUserID = bMe.Id
	}

	log.Info("managed bootstrap: attempting invite/promote bridge bot",
		zap.Int64("target_chat_id", targetChatID),
		zap.Int64("bridge_bot_user_id", bridgeUserID))

	brMem, err := mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
	if err != nil {
		invErr := tryInviteChatMemberRaw(mainBot, targetChatID, bridgeUserID)
		if invErr != nil {
			log.Warn("managed bootstrap: inviteChatMember failed (often unsupported for Bot API; add bridge bot manually)",
				zap.Int64("target_chat_id", targetChatID),
				zap.Int64("bridge_bot_user_id", bridgeUserID),
				zap.Error(invErr))
		}
		brMem, err = mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
		if err != nil {
			log.Warn("managed bootstrap: bridge bot still not a member after invite attempt",
				zap.Int64("target_chat_id", targetChatID),
				zap.Int64("bridge_bot_user_id", bridgeUserID),
				zap.Error(err))
			return ErrBridgeBotNotGroupMember
		}
	}
	brMerged := brMem.MergeChatMember()
	if brMerged.Status == "creator" || (brMerged.Status == "administrator" && brMerged.CanManageTopics) {
		log.Info("managed bootstrap: bridge bot already admin with manage topics", zap.Int64("target_chat_id", targetChatID))
		return nil
	}
	if brMerged.Status != "administrator" && brMerged.Status != "member" {
		invErr := tryInviteChatMemberRaw(mainBot, targetChatID, bridgeUserID)
		if invErr != nil {
			log.Warn("managed bootstrap: inviteChatMember failed (second try)", zap.Error(invErr))
		}
		brMem, err = mainBot.GetChatMember(targetChatID, bridgeUserID, nil)
		if err != nil {
			log.Warn("managed bootstrap: getChatMember bridge after invite", zap.Error(err))
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
	if err != nil {
		log.Warn("managed bootstrap: promoteChatMember failed",
			zap.Int64("target_chat_id", targetChatID),
			zap.Int64("bridge_bot_user_id", bridgeUserID),
			zap.Error(err))
	}
	return err
}

func getChatMemberWithRetry(bot *gotgbot.Bot, chatID, userID int64, attempts int, delay time.Duration) (gotgbot.ChatMember, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		m, err := bot.GetChatMember(chatID, userID, nil)
		if err == nil {
			return m, nil
		}
		lastErr = err
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	var zero gotgbot.ChatMember
	return zero, lastErr
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
