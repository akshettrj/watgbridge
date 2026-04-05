package telegram

import (
	"fmt"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// ValidateTargetForumAndBotRights returns an error if the chat is not a forum or the bot lacks Manage topics (or membership).
func ValidateTargetForumAndBotRights(bot *gotgbot.Bot, chatID int64) error {
	chat, err := bot.GetChat(chatID, nil)
	if err != nil {
		return fmt.Errorf("could not get chat info for target_chat_id %d (is the bot in the group?): %w", chatID, err)
	}
	if !chat.IsForum {
		return fmt.Errorf("the target chat is not a forum (enable Topics: group settings → Topics → On)")
	}
	member, err := bot.GetChatMember(chatID, bot.Id, nil)
	if err != nil {
		return fmt.Errorf("could not get bot membership in the target group: %w", err)
	}
	merged := member.MergeChatMember()
	if merged.Status != "administrator" && merged.Status != "creator" {
		return fmt.Errorf("the bot is not an administrator in the target group")
	}
	if merged.Status == "administrator" && !merged.CanManageTopics {
		return fmt.Errorf("the bot does not have Manage topics permission in the target group")
	}
	return nil
}

// CheckTargetGroupPermissions verifies the target group is a forum and the bot is admin with
// "Manage topics" permission. On failure, logs the error and sends the same message to the
// target group and to the owner.
func CheckTargetGroupPermissions() {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
		bot    = state.State.TelegramBot
	)
	chatID := cfg.Telegram.TargetChatID
	if err := ValidateTargetForumAndBotRights(bot, chatID); err != nil {
		msg := fmt.Sprintf("WaTgBridge target group check failed: %v", err)
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
