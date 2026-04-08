package telegram

import (
	"sync"

	"watgbridge/state"
	"watgbridge/utils"

	"go.uber.org/zap"
)

var forumMetaReprovisionMu sync.Mutex

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
	matched := false
	switch threadID {
	case t.BotMetaThreadID:
		t.BotMetaThreadID = 0
		matched = true
	case t.CallsThreadID:
		t.CallsThreadID = 0
		matched = true
	case t.StatusThreadID:
		t.StatusThreadID = 0
		matched = true
	}
	if !matched {
		return
	}
	forumMetaReprovisionMu.Lock()
	defer forumMetaReprovisionMu.Unlock()
	if err := cfg.SaveConfig(); err != nil {
		state.State.Logger.Warn("forum meta: save after clearing stale meta thread id", zap.Error(err))
	}
	if err := EnsureForumMetaTopicsProvisioned(); err != nil {
		state.State.Logger.Warn("forum meta: reprovision after send failure", zap.Error(err))
	}
}
