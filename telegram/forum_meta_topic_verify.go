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
	forumMetaProbeMaxAttempts = 5
	forumMetaProbeRetryBase   = 300 * time.Millisecond
	forumMetaProbeText        = "."
	forumMetaResolveRounds    = 6
	forumMetaResolveBase      = 2 * time.Second
)

type forumMetaThreadProbeResult int

const (
	forumMetaThreadProbeUnknown forumMetaThreadProbeResult = iota
	forumMetaThreadProbeValid
	forumMetaThreadProbeMissing
)

func forumMetaProbeBackoff(attempt int) {
	if attempt <= 0 {
		return
	}
	shift := attempt - 1
	if shift > 5 {
		shift = 5
	}
	time.Sleep(forumMetaProbeRetryBase * time.Duration(1<<shift))
}

func forumMetaResolveBackoff(round int) {
	if round <= 0 {
		return
	}
	shift := round - 1
	if shift > 5 {
		shift = 5
	}
	time.Sleep(forumMetaResolveBase * time.Duration(1<<shift))
}

func forumMetaManagementEnabledForChat(chatID int64) bool {
	cfg := state.State.Config
	if cfg == nil {
		return true
	}
	allow := cfg.Telegram.ForumMetaSendProbeTargetChatIDs
	if len(allow) == 0 {
		return true
	}
	for _, id := range allow {
		if id == chatID {
			return true
		}
	}
	return false
}

// forumMetaProbeThreadBySend verifies a topic by trying to send one tiny probe message into it.
// On success we delete the probe message best-effort.
// Returns:
// - Valid: topic accepted a message (thread exists)
// - Missing: Telegram says thread/topic is invalid/not found
// - Unknown: transient/noise/other errors (caller should avoid destructive changes)
func forumMetaProbeThreadBySend(bot *gotgbot.Bot, chatID, threadID int64, slot string) (forumMetaThreadProbeResult, error) {
	if bot == nil {
		return forumMetaThreadProbeUnknown, fmt.Errorf("telegram bot not initialized")
	}
	if threadID == 0 {
		return forumMetaThreadProbeMissing, nil
	}
	var lastRetryable error
	for attempt := 0; attempt < forumMetaProbeMaxAttempts; attempt++ {
		forumMetaProbeBackoff(attempt)
		sent, err := bot.SendMessage(chatID, forumMetaProbeText, &gotgbot.SendMessageOpts{
			MessageThreadId:     threadID,
			DisableNotification: true,
		})
		if err == nil {
			if sent != nil {
				if _, delErr := bot.DeleteMessage(chatID, sent.MessageId, nil); delErr != nil {
					state.State.Logger.Debug("forum meta: probe cleanup delete failed",
						zap.String("slot", slot),
						zap.Int64("thread_id", threadID),
						zap.Error(delErr))
				}
			}
			return forumMetaThreadProbeValid, nil
		}
		if utils.TgErrForumTopicOrThreadInvalid(err) {
			return forumMetaThreadProbeMissing, nil
		}
		if utils.TgErrForumMetaProbeRetryable(err) {
			lastRetryable = err
			state.State.Logger.Debug("forum meta: send probe retry (retryable)",
				zap.String("slot", slot),
				zap.Int("attempt", attempt+1),
				zap.Int64("thread_id", threadID),
				zap.Error(err))
			continue
		}
		return forumMetaThreadProbeUnknown, fmt.Errorf("send probe non-retryable: %w", err)
	}
	if lastRetryable != nil {
		return forumMetaThreadProbeUnknown, fmt.Errorf("send probe after %d attempts: %w", forumMetaProbeMaxAttempts, lastRetryable)
	}
	return forumMetaThreadProbeUnknown, fmt.Errorf("send probe after %d attempts", forumMetaProbeMaxAttempts)
}

func forumMetaProbeThreadByEdit(bot *gotgbot.Bot, chatID, threadID int64, slot string, spec forumMetaSpec) (forumMetaThreadProbeResult, error) {
	if bot == nil {
		return forumMetaThreadProbeUnknown, fmt.Errorf("telegram bot not initialized")
	}
	if threadID == 0 {
		return forumMetaThreadProbeMissing, nil
	}
	name := utils.TruncateTelegramForumTopicName(strings.TrimSpace(spec.title))
	opts := &gotgbot.EditForumTopicOpts{Name: name}
	if id := pickForumMetaIconCustomEmojiID(bot, spec); id != "" {
		idCopy := id
		opts.IconCustomEmojiId = &idCopy
	}
	var lastRetryable error
	for attempt := 0; attempt < forumMetaProbeMaxAttempts; attempt++ {
		forumMetaProbeBackoff(attempt)
		_, err := bot.EditForumTopic(chatID, threadID, opts)
		if err == nil || utils.TgEditForumTopicUnchanged(err) {
			return forumMetaThreadProbeValid, nil
		}
		if utils.TgErrForumTopicOrThreadInvalid(err) {
			return forumMetaThreadProbeMissing, nil
		}
		if utils.TgErrForumMetaProbeRetryable(err) {
			lastRetryable = err
			state.State.Logger.Debug("forum meta: edit probe retry (retryable)",
				zap.String("slot", slot),
				zap.Int("attempt", attempt+1),
				zap.Int64("thread_id", threadID),
				zap.Error(err))
			continue
		}
		return forumMetaThreadProbeUnknown, fmt.Errorf("edit probe non-retryable: %w", err)
	}
	if lastRetryable != nil {
		return forumMetaThreadProbeUnknown, fmt.Errorf("edit probe after %d attempts: %w", forumMetaProbeMaxAttempts, lastRetryable)
	}
	return forumMetaThreadProbeUnknown, fmt.Errorf("edit probe after %d attempts", forumMetaProbeMaxAttempts)
}

func forumMetaProbeThread(bot *gotgbot.Bot, chatID, threadID int64, slot string, spec forumMetaSpec) (forumMetaThreadProbeResult, error) {
	if forumMetaManagementEnabledForChat(chatID) {
		return forumMetaProbeThreadBySend(bot, chatID, threadID, slot)
	}
	return forumMetaProbeThreadByEdit(bot, chatID, threadID, slot, spec)
}

// forumMetaProbeThreadResolved retries unknown probe outcomes across multiple rounds so
// topic deletion is self-healed in-process without requiring stack restarts.
func forumMetaProbeThreadResolved(bot *gotgbot.Bot, chatID, threadID int64, slot string, spec forumMetaSpec) (forumMetaThreadProbeResult, error) {
	var lastErr error
	for round := 0; round < forumMetaResolveRounds; round++ {
		forumMetaResolveBackoff(round)
		result, err := forumMetaProbeThread(bot, chatID, threadID, slot, spec)
		if result != forumMetaThreadProbeUnknown {
			return result, err
		}
		lastErr = err
		state.State.Logger.Info("forum meta: probe inconclusive; retrying",
			zap.String("slot", slot),
			zap.Int("round", round+1),
			zap.Int("rounds_total", forumMetaResolveRounds),
			zap.Int64("thread_id", threadID),
			zap.Error(err))
	}
	if lastErr != nil {
		return forumMetaThreadProbeUnknown, fmt.Errorf("probe inconclusive after %d rounds: %w", forumMetaResolveRounds, lastErr)
	}
	return forumMetaThreadProbeUnknown, fmt.Errorf("probe inconclusive after %d rounds", forumMetaResolveRounds)
}
