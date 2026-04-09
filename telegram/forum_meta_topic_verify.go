package telegram

import (
	"fmt"
	"strings"
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

// forumMetaTopicNameMatches compares Telegram's topic title to our spec (after truncation).
func forumMetaTopicNameMatches(specTitle, remoteName string) bool {
	want := utils.TruncateTelegramForumTopicName(strings.TrimSpace(specTitle))
	got := utils.TruncateTelegramForumTopicName(strings.TrimSpace(remoteName))
	return want == got
}

// forumMetaTopicMatchesSpec returns true only if getForumTopic succeeds and the topic name matches
// the expected meta slot title. SendMessage cannot distinguish topics; this is the source of truth.
func forumMetaTopicMatchesSpec(bot *gotgbot.Bot, chatID, threadID int64, spec forumMetaSpec) (bool, error) {
	if threadID == 0 {
		return false, nil
	}
	var lastRetryable error
	for attempt := 0; attempt < forumMetaProbeMaxAttempts; attempt++ {
		if attempt > 0 {
			shift := attempt - 1
			if shift > 5 {
				shift = 5
			}
			d := forumMetaProbeRetryBase * time.Duration(1<<shift)
			time.Sleep(d)
		}
		name, nameOk, err := utils.TgFetchForumTopicName(bot, chatID, threadID)
		if err == nil {
			if !nameOk {
				return false, nil
			}
			if forumMetaTopicNameMatches(spec.title, name) {
				return true, nil
			}
			return false, nil
		}
		if utils.TgErrForumTopicOrThreadInvalid(err) {
			return false, nil
		}
		if utils.TgErrForumMetaProbeRetryable(err) {
			lastRetryable = err
			state.State.Logger.Debug("forum meta: getForumTopic retry (retryable)",
				zap.Int("attempt", attempt+1),
				zap.Int64("thread_id", threadID),
				zap.String("title", spec.title),
				zap.Error(err))
			continue
		}
		state.State.Logger.Debug("forum meta: getForumTopic failed; treating topic as missing",
			zap.Int64("thread_id", threadID),
			zap.String("title", spec.title),
			zap.Error(err))
		return false, nil
	}
	if lastRetryable != nil {
		return false, fmt.Errorf("getForumTopic after %d attempts: %w", forumMetaProbeMaxAttempts, lastRetryable)
	}
	return false, fmt.Errorf("getForumTopic after %d attempts", forumMetaProbeMaxAttempts)
}
