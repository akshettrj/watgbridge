package telegram

import (
	"os"
	"path/filepath"
	"strconv"

	"watgbridge/database"
	"watgbridge/state"
)

// SeedMappedForumTopicsFromConfig wires forum thread ids from config into local state (meta topics →
// ChatThreadPair, bot_meta_topic_id file). Called after forum meta provisioning and from reprovision.
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
	if cfg.Telegram.BotMetaThreadID != 0 && cfg.Path != "" {
		p := filepath.Join(filepath.Dir(cfg.Path), "bot_meta_topic_id")
		_ = os.WriteFile(p, []byte(strconv.FormatInt(cfg.Telegram.BotMetaThreadID, 10)), 0o644)
	}
}
