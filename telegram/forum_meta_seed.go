package telegram

import (
	"watgbridge/database"
	"watgbridge/state"
)

// SeedMappedForumTopicsFromConfig wires forum thread ids from runtime config into ChatThreadPair for
// calls/status. Called after forum meta provisioning and from reprovision.
func SeedMappedForumTopicsFromConfig(cfg *state.Config) {
	tgChat := cfg.Telegram.TargetChatID
	if cfg.Telegram.CallsThreadID != 0 && tgChat != 0 {
		_, found, err := database.ChatThreadGetTgFromWa("calls", tgChat)
		if err == nil && !found {
			_ = database.ChatThreadAddNewPair("calls", tgChat, cfg.Telegram.CallsThreadID)
		}
	}
	if cfg.Telegram.StatusThreadID != 0 && tgChat != 0 {
		_, found, err := database.ChatThreadGetTgFromWa("status@broadcast", tgChat)
		if err == nil && !found {
			_ = database.ChatThreadAddNewPair("status@broadcast", tgChat, cfg.Telegram.StatusThreadID)
		}
	}
}
