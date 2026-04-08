package telegram

import (
	"fmt"
	"sync"
	"time"

	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

const (
	forumMetaProbeMaxAttempts = 6
	forumMetaProbeRetryBase   = 250 * time.Millisecond
)

// forumMetaProbeMessages tracks probe message ids so replies to them are ignored by the bridge.
var forumMetaProbeMessages sync.Map // key: "chatID:messageID" → struct{}

func forumMetaProbeKey(chatID, messageID int64) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

// RegisterForumMetaProbeMessage marks a chat message as a forum meta probe (reply handling ignores it).
func RegisterForumMetaProbeMessage(chatID, messageID int64) {
	forumMetaProbeMessages.Store(forumMetaProbeKey(chatID, messageID), struct{}{})
}

func unregisterForumMetaProbeMessage(chatID, messageID int64) {
	forumMetaProbeMessages.Delete(forumMetaProbeKey(chatID, messageID))
}

// IsForumMetaProbeReply is true when the user replied to a forum meta existence probe from the bot.
func IsForumMetaProbeReply(msg *gotgbot.Message) bool {
	if msg == nil || msg.ReplyToMessage == nil {
		return false
	}
	rt := msg.ReplyToMessage
	if rt.From == nil || !rt.From.IsBot {
		return false
	}
	_, ok := forumMetaProbeMessages.Load(forumMetaProbeKey(msg.Chat.Id, rt.MessageId))
	return ok
}

func forumMetaProbeMessageText(topicDisplayName string) string {
	return "DO NOT REPLY TO THIS!\n\n" +
		"Checking " + topicDisplayName + " existence...\n\n" +
		"This is a service message; it will be deleted immediately."
}

// forumMetaProbeTopicAlive sends a probe message to the forum thread. On success it deletes the message
// immediately (after briefly registering for reply-ignore). Returns (true, nil) if the topic exists;
// (false, nil) if Telegram reports the thread/topic invalid; (_, err) on hard errors after retries.
func forumMetaProbeTopicAlive(bot *gotgbot.Bot, chatID, threadID int64, topicDisplayName string) (bool, error) {
	if threadID == 0 {
		return false, nil
	}
	text := forumMetaProbeMessageText(topicDisplayName)
	var lastErr error
	for attempt := 0; attempt < forumMetaProbeMaxAttempts; attempt++ {
		if attempt > 0 {
			shift := attempt - 1
			if shift > 5 {
				shift = 5
			}
			d := forumMetaProbeRetryBase * time.Duration(1<<shift)
			time.Sleep(d)
		}
		sent, err := bot.SendMessage(chatID, text, &gotgbot.SendMessageOpts{
			MessageThreadId: threadID,
		})
		if err == nil {
			mid := sent.MessageId
			RegisterForumMetaProbeMessage(chatID, mid)
			_, delErr := bot.DeleteMessage(chatID, mid, nil)
			unregisterForumMetaProbeMessage(chatID, mid)
			if delErr != nil {
				state.State.Logger.Debug("forum meta: instant delete probe message",
					zap.Int64("chat_id", chatID),
					zap.Int64("message_id", mid),
					zap.Error(delErr))
			}
			return true, nil
		}
		lastErr = err
		if utils.TgErrForumTopicOrThreadInvalid(err) {
			return false, nil
		}
		state.State.Logger.Debug("forum meta: probe sendMessage retry",
			zap.Int("attempt", attempt+1),
			zap.Int64("thread_id", threadID),
			zap.Error(err))
	}
	return false, fmt.Errorf("probe sendMessage after %d attempts: %w", forumMetaProbeMaxAttempts, lastErr)
}
