package telegram

import (
	"watgbridge/database"
	"watgbridge/state"
)

// SeedMappedForumTopicsFromConfig wires forum thread ids from runtime config into ChatThreadPair for
// calls/status. Called after forum meta provisioning and from reprovision.
func SeedMappedForumTopicsFromConfig(cfg *state.Config) {
	tgChat := cfg.Telegram.TargetChatID
	if tgChat == 0 {
		return
	}
	_ = database.ChatThreadDropPairByWaChat("bot_meta", tgChat)
	if cfg.Telegram.CallsThreadID != 0 {
		_ = database.ChatThreadAddNewPair(forumMetaChatKeyCalls, tgChat, cfg.Telegram.CallsThreadID)
	}
	if cfg.Telegram.StatusThreadID != 0 {
		_ = database.ChatThreadAddNewPair(forumMetaChatKeyStatus, tgChat, cfg.Telegram.StatusThreadID)
	}
}
