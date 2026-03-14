package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

const botMetaTopicName = "Bot's meta"

// botMetaTopicIdPath returns the path to the file storing the "Bot's meta" topic thread id (same dir as config).
func botMetaTopicIdPath() string {
	return filepath.Join(filepath.Dir(state.State.Config.Path), "bot_meta_topic_id")
}

// LogVersionToBotMetaTopic ensures the "Bot's meta" topic exists in the target group,
// then sends a launch message with the current version (short sha or tag from WATGBRIDGE_VERSION).
// The topic thread id is stored in a file so it persists across restarts (e.g. when config is regenerated from env).
func LogVersionToBotMetaTopic() {
	cfg := state.State.Config
	bot := state.State.TelegramBot
	chatId := cfg.Telegram.TargetChatID
	path := botMetaTopicIdPath()

	threadId := int64(0)
	if b, err := os.ReadFile(path); err == nil {
		if id, err := strconv.ParseInt(string(b), 10, 64); err == nil {
			threadId = id
		}
	}
	if threadId == 0 {
		newForum, err := bot.CreateForumTopic(chatId, botMetaTopicName, &gotgbot.CreateForumTopicOpts{})
		if err != nil {
			state.State.Logger.Error("failed to create Bot's meta topic", zap.Error(err))
			return
		}
		threadId = newForum.MessageThreadId
		if err := os.WriteFile(path, []byte(strconv.FormatInt(threadId, 10)), 0644); err != nil {
			state.State.Logger.Error("failed to write bot_meta_topic_id file", zap.Error(err))
		}
	}

	msg := fmt.Sprintf("Launched • version: <code>%s</code>", state.WATGBRIDGE_VERSION)
	_, _ = bot.SendMessage(chatId, msg, &gotgbot.SendMessageOpts{
		MessageThreadId: threadId,
		ParseMode:       "HTML",
	})
}
