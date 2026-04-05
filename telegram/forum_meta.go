package telegram

import (
	"fmt"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// CreateStandardForumMetaTopics creates General, BotMeta, Calls, and Status forum topics in one sequence.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64) (general, botMeta, calls, status int64, err error) {
	generalT, err := bot.CreateForumTopic(chatID, "General", nil)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	metaT, err := bot.CreateForumTopic(chatID, "BotMeta", nil)
	if err != nil {
		return generalT.MessageThreadId, 0, 0, 0, err
	}
	callsT, err := bot.CreateForumTopic(chatID, "Calls", nil)
	if err != nil {
		return generalT.MessageThreadId, metaT.MessageThreadId, 0, 0, err
	}
	statusT, err := bot.CreateForumTopic(chatID, "Status", nil)
	if err != nil {
		return generalT.MessageThreadId, metaT.MessageThreadId, callsT.MessageThreadId, 0, err
	}
	return generalT.MessageThreadId, metaT.MessageThreadId, callsT.MessageThreadId, statusT.MessageThreadId, nil
}

// EnsureForumMetaTopicsProvisioned requires a forum target group with Manage topics, then ensures all four
// meta topic IDs exist (creates every topic in one batch if any id is missing) and persists config.
func EnsureForumMetaTopicsProvisioned() error {
	cfg := state.State.Config
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := &cfg.Telegram
	if t.TargetChatID == 0 {
		return fmt.Errorf("telegram.target_chat_id is required")
	}
	if err := ValidateTargetForumAndBotRights(bot, t.TargetChatID); err != nil {
		msg := "WaTgBridge forum setup failed: " + err.Error()
		state.State.Logger.Error(msg)
		sendTargetCheckFailure(msg)
		return err
	}
	anySet := t.GeneralThreadID != 0 || t.BotMetaThreadID != 0 || t.CallsThreadID != 0 || t.StatusThreadID != 0
	allSet := t.GeneralThreadID != 0 && t.BotMetaThreadID != 0 && t.CallsThreadID != 0 && t.StatusThreadID != 0
	if anySet && !allSet {
		return fmt.Errorf("telegram forum threads: set all four (general_thread_id, bot_meta_thread_id, calls_thread_id, status_thread_id) or omit all four for auto-provision")
	}
	if allSet {
		return nil
	}
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	t.GeneralThreadID = g
	t.BotMetaThreadID = m
	t.CallsThreadID = c
	t.StatusThreadID = s
	if err := cfg.SaveConfig(); err != nil {
		return fmt.Errorf("save config after forum topics: %w", err)
	}
	state.State.Logger.Info("created standard forum meta topics and saved thread ids to config",
		zap.Int64("general_thread_id", g),
		zap.Int64("bot_meta_thread_id", m),
		zap.Int64("calls_thread_id", c),
		zap.Int64("status_thread_id", s),
	)
	return nil
}
