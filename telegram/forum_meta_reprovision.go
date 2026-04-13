package telegram

import (
	"fmt"
	"sync"
	"time"

	"watgbridge/state"
	"watgbridge/utils"

	"go.uber.org/zap"
)

var forumMetaReprovisionMu sync.Mutex
var forumMetaReprovisionDebounceMu sync.Mutex
var forumMetaReprovisionLastBySlot = make(map[string]time.Time)

const forumMetaReprovisionMinInterval = 10 * time.Second

func forumMetaReprovisionDebounceKey(chatID int64, slot string) string {
	return fmt.Sprintf("%d:%s", chatID, slot)
}

func forumMetaReprovisionAllowedNow(chatID int64, slot string) bool {
	now := time.Now()
	key := forumMetaReprovisionDebounceKey(chatID, slot)
	forumMetaReprovisionDebounceMu.Lock()
	defer forumMetaReprovisionDebounceMu.Unlock()
	if last, ok := forumMetaReprovisionLastBySlot[key]; ok {
		if now.Sub(last) < forumMetaReprovisionMinInterval {
			return false
		}
	}
	forumMetaReprovisionLastBySlot[key] = now
	return true
}

func forumMetaVerifySlotThread(botChatID, threadID int64, slot string) (bool, error) {
	bot := state.State.TelegramBot
	if bot == nil {
		return false, fmt.Errorf("telegram bot not initialized")
	}
	spec, ok := forumMetaSpecBySlot(slot)
	if !ok {
		return false, nil
	}
	result, err := forumMetaProbeThreadResolved(bot, botChatID, threadID, slot, spec)
	switch result {
	case forumMetaThreadProbeValid:
		return true, nil
	case forumMetaThreadProbeMissing:
		return false, nil
	default:
		return false, err
	}
}

func init() {
	// Registered on utils.ForumMetaOnThreadSendFailure (see utils.TgNotifyForumMetaSendFailure).
	utils.ForumMetaOnThreadSendFailure = handleForumMetaThreadSendFailure
}

func handleForumMetaThreadSendFailure(chatID, threadID int64, sendErr error) {
	if sendErr == nil || threadID == 0 {
		return
	}
	if !utils.TgErrForumTopicOrThreadInvalid(sendErr) {
		return
	}
	cfg := state.State.Config
	if cfg == nil {
		return
	}
	t := &cfg.Telegram
	if chatID != t.TargetChatID {
		return
	}
	if !forumMetaManagementEnabledForChat(chatID) {
		return
	}
	slot := ""
	switch threadID {
	case t.CallsThreadID:
		slot = forumMetaSlotCalls
	case t.StatusThreadID:
		slot = forumMetaSlotStatus
	}
	if slot == "" {
		return
	}
	if !forumMetaReprovisionAllowedNow(chatID, slot) {
		state.State.Logger.Debug("forum meta: reprovision debounced after send failure",
			zap.String("slot", slot),
			zap.Int64("chat_id", chatID),
			zap.Int64("thread_id", threadID))
		return
	}
	stillValid, verifyErr := forumMetaVerifySlotThread(chatID, threadID, slot)
	if verifyErr != nil {
		state.State.Logger.Warn("forum meta: send failure verify failed; skip reset",
			zap.String("slot", slot),
			zap.Int64("chat_id", chatID),
			zap.Int64("thread_id", threadID),
			zap.Error(verifyErr))
		return
	}
	if stillValid {
		state.State.Logger.Debug("forum meta: send failure but topic still valid; skip reset",
			zap.String("slot", slot),
			zap.Int64("chat_id", chatID),
			zap.Int64("thread_id", threadID),
			zap.Error(sendErr))
		return
	}

	forumMetaReprovisionMu.Lock()
	defer forumMetaReprovisionMu.Unlock()
	switch slot {
	case forumMetaSlotCalls:
		if t.CallsThreadID == threadID {
			t.CallsThreadID = 0
		}
	case forumMetaSlotStatus:
		if t.StatusThreadID == threadID {
			t.StatusThreadID = 0
		}
	}
	state.State.Logger.Info("forum meta: stale topic confirmed; reprovisioning slot",
		zap.String("slot", slot),
		zap.Int64("chat_id", chatID),
		zap.Int64("thread_id", threadID))
	if err := EnsureForumMetaTopicsProvisioned(); err != nil {
		state.State.Logger.Warn("forum meta: reprovision after send failure", zap.Error(err))
	}
}
