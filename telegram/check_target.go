package telegram

import (
	"fmt"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// CheckTargetGroupPermissions verifies the target group is a forum and the bot is admin with
// "Manage topics" permission. On failure, logs the error and sends the same message to the
// target group and to the owner.
func CheckTargetGroupPermissions() {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
		bot    = state.State.TelegramBot
	)
	chatId := cfg.Telegram.TargetChatID

	chat, err := bot.GetChat(chatId, nil)
	if err != nil {
		msg := fmt.Sprintf("WaTgBridge target group check failed: could not get chat info for target_chat_id %d. Ensure the bot is in the group. Error: %v", chatId, err)
		logger.Error(msg)
		sendTargetCheckFailure(msg)
		return
	}

	if !chat.IsForum {
		msg := "WaTgBridge target group check failed: the target chat is not a forum (Topics are disabled). Enable Topics in the group: Group settings → Topics → On."
		logger.Error(msg)
		sendTargetCheckFailure(msg)
		return
	}

	member, err := bot.GetChatMember(chatId, bot.Id, nil)
	if err != nil {
		msg := fmt.Sprintf("WaTgBridge target group check failed: could not get bot membership in the target group. Ensure the bot is added to the group. Error: %v", err)
		logger.Error(msg)
		sendTargetCheckFailure(msg)
		return
	}

	merged := member.MergeChatMember()
	if merged.Status != "administrator" && merged.Status != "creator" {
		msg := "WaTgBridge target group check failed: the bot is not an administrator in the target group. Add the bot as admin (with at least 'Manage topics' permission)."
		logger.Error(msg)
		sendTargetCheckFailure(msg)
		return
	}

	if merged.Status == "administrator" && !merged.CanManageTopics {
		msg := "WaTgBridge target group check failed: the bot does not have 'Manage topics' permission in the target group. Grant the bot admin permission 'Manage topics' in the group settings."
		logger.Error(msg)
		sendTargetCheckFailure(msg)
		return
	}

	logger.Info("target group check passed: chat is a forum and bot has Manage topics permission")
}

func sendTargetCheckFailure(msg string) {
	bot := state.State.TelegramBot
	cfg := state.State.Config
	opts := &gotgbot.SendMessageOpts{}
	_, _ = bot.SendMessage(cfg.Telegram.TargetChatID, msg, opts)
	_, _ = bot.SendMessage(cfg.Telegram.OwnerID, msg, opts)
}
