package telegram

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// telegramGeneralTopicThreadID is message_thread_id 1 for the default "General" topic in Telegram
// forums. Config value 0 means "default General" (omit message_thread_id when sending).
const telegramGeneralTopicThreadID int64 = 1

// forumMetaTopicIconColor is Telegram Bot API icon_color 0x6FB9F0 (light blue).
const forumMetaTopicIconColor int64 = 7322096

type forumMetaSpec struct {
	title string
	// iconEmoji is matched against GetForumTopicIconStickers; it is not part of the topic title.
	iconEmoji string
}

// Plain topic titles; icons come from iconEmoji + Telegram forum sticker set.
var standardForumMetaSpecs = []forumMetaSpec{
	{"General", ""},
	{"Bot's meta", "💻"},
	{"Calls", "🔮"},
	{"Status", "📱"},
}

// ForumMetaHints carries thread ids (0 = unknown). Prefer loading persisted ids from
// bridge_provision_states via ApplyForumMetaThreadIDsFromProvisionDB before provisioning.
type ForumMetaHints struct {
	GeneralThreadID int64
	BotMetaThreadID int64
	CallsThreadID   int64
	StatusThreadID  int64
}

var forumMetaIconStickersMu sync.Mutex
var forumMetaIconStickers []gotgbot.Sticker

func forumMetaGetIconStickers(bot *gotgbot.Bot) []gotgbot.Sticker {
	forumMetaIconStickersMu.Lock()
	defer forumMetaIconStickersMu.Unlock()
	if forumMetaIconStickers != nil {
		return forumMetaIconStickers
	}
	stickers, err := bot.GetForumTopicIconStickers(nil)
	if err != nil {
		state.State.Logger.Debug("forum meta: GetForumTopicIconStickers failed", zap.Error(err))
		return nil
	}
	forumMetaIconStickers = stickers
	return forumMetaIconStickers
}

func pickForumMetaIconCustomEmojiID(bot *gotgbot.Bot, spec forumMetaSpec) string {
	want := strings.TrimSpace(spec.iconEmoji)
	if want == "" {
		return ""
	}
	stickers := forumMetaGetIconStickers(bot)
	for _, s := range stickers {
		if s.CustomEmojiId != "" && s.Emoji == want {
			return s.CustomEmojiId
		}
	}
	for _, s := range stickers {
		if s.CustomEmojiId != "" && strings.HasPrefix(s.Emoji, want) {
			return s.CustomEmojiId
		}
	}
	return ""
}

func applyForumMetaTopicIcon(bot *gotgbot.Bot, chatID, threadID int64, spec forumMetaSpec) {
	if threadID == 0 {
		return
	}
	id := pickForumMetaIconCustomEmojiID(bot, spec)
	if id == "" {
		return
	}
	idCopy := id
	_, err := bot.EditForumTopic(chatID, threadID, &gotgbot.EditForumTopicOpts{
		IconCustomEmojiId: &idCopy,
	})
	if err != nil && !utils.TgEditForumTopicUnchanged(err) {
		state.State.Logger.Debug("forum meta: EditForumTopic custom emoji icon",
			zap.String("title", spec.title),
			zap.Int64("thread_id", threadID),
			zap.Error(err))
	}
}

// ApplyForumMetaThreadIDsFromProvisionDB overwrites telegram thread ids from bridge_provision_states
// when the bridge has a registry id and the DB row has non-zero ids. Call after DB connect and
// before EnsureForumMetaTopicsProvisioned so persisted ids take precedence over YAML.
func ApplyForumMetaThreadIDsFromProvisionDB(cfg *state.Config) {
	t := &cfg.Telegram
	if t.BridgeRegistryID == 0 {
		return
	}
	p, err := database.BridgeProvisionGet(t.BridgeRegistryID)
	if err != nil || p == nil {
		return
	}
	if p.GeneralThreadID != 0 {
		t.GeneralThreadID = p.GeneralThreadID
	}
	if p.BotMetaThreadID != 0 {
		t.BotMetaThreadID = p.BotMetaThreadID
	}
	if p.CallsThreadID != 0 {
		t.CallsThreadID = p.CallsThreadID
	}
	if p.StatusThreadID != 0 {
		t.StatusThreadID = p.StatusThreadID
	}
}

func resolveGeneralThreadID(bot *gotgbot.Bot, chatID int64, hint int64) (int64, error) {
	if hint != 0 {
		ok, err := forumMetaProbeTopicAlive(bot, chatID, hint, standardForumMetaSpecs[0].title)
		if err != nil {
			return 0, err
		}
		if ok {
			return hint, nil
		}
	}
	ok, err := forumMetaProbeTopicAlive(bot, chatID, telegramGeneralTopicThreadID, standardForumMetaSpecs[0].title)
	if err != nil {
		return 0, err
	}
	if ok {
		return telegramGeneralTopicThreadID, nil
	}
	return 0, nil
}

func createForumMetaTopic(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec) (int64, error) {
	name := utils.TruncateTelegramForumTopicName(spec.title)
	opts := &gotgbot.CreateForumTopicOpts{}
	if emojiID := pickForumMetaIconCustomEmojiID(bot, spec); emojiID != "" {
		opts.IconCustomEmojiId = emojiID
	} else {
		opts.IconColor = forumMetaTopicIconColor
	}
	created, err := bot.CreateForumTopic(chatID, name, opts)
	if err != nil {
		return 0, err
	}
	return created.MessageThreadId, nil
}

func provisionMetaSlot(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, threadID int64) (int64, error) {
	if threadID != 0 {
		ok, err := forumMetaProbeTopicAlive(bot, chatID, threadID, spec.title)
		if err != nil {
			return 0, err
		}
		if ok {
			applyForumMetaTopicIcon(bot, chatID, threadID, spec)
			return threadID, nil
		}
	}
	tid, err := createForumMetaTopic(bot, chatID, spec)
	if err != nil {
		return 0, err
	}
	applyForumMetaTopicIcon(bot, chatID, tid, spec)
	return tid, nil
}

// CreateStandardForumMetaTopics resolves General (probe only; never creates), then for Bot's meta,
// Calls, Status: probe saved thread id with sendMessage; if missing, create topic and apply icon.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64, hints ForumMetaHints) (general, botMeta, calls, status int64, err error) {
	g, err := resolveGeneralThreadID(bot, chatID, hints.GeneralThreadID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	m, err := provisionMetaSlot(bot, chatID, standardForumMetaSpecs[1], hints.BotMetaThreadID)
	if err != nil {
		return g, 0, 0, 0, err
	}
	c, err := provisionMetaSlot(bot, chatID, standardForumMetaSpecs[2], hints.CallsThreadID)
	if err != nil {
		return g, m, 0, 0, err
	}
	s, err := provisionMetaSlot(bot, chatID, standardForumMetaSpecs[3], hints.StatusThreadID)
	if err != nil {
		return g, m, c, 0, err
	}
	return g, m, c, s, nil
}

func effectiveGeneralThreadID(prevFromConfig int64, resolved int64) int64 {
	if prevFromConfig != 0 {
		return prevFromConfig
	}
	return resolved
}

func syncForumMetaRegistryState(cfg *state.Config) {
	t := &cfg.Telegram
	if t.BridgeRegistryID == 0 {
		return
	}
	if err := database.BridgeProvisionSet(
		t.BridgeRegistryID,
		t.GeneralThreadID,
		t.BotMetaThreadID,
		t.CallsThreadID,
		t.StatusThreadID,
		"ok",
		"",
	); err != nil {
		state.State.Logger.Warn("forum meta: could not sync thread ids to bridge registry DB (child YAML may reprovision on restart)",
			zap.Uint("bridge_registry_id", t.BridgeRegistryID),
			zap.Error(err))
	}
	if cfg.Path != "" {
		if err := bridge.WriteProvisionSidecar(filepath.Dir(cfg.Path), t.BridgeRegistryID,
			t.GeneralThreadID, t.BotMetaThreadID, t.CallsThreadID, t.StatusThreadID); err != nil {
			state.State.Logger.Warn("forum meta: could not write provision sidecar (parent may reprovision forum topics on restart)",
				zap.Uint("bridge_registry_id", t.BridgeRegistryID),
				zap.Error(err))
		}
	}
}

// EnsureForumMetaTopicsProvisioned loads persisted thread ids from the registry DB when available,
// then probes each slot (send + delete); creates topics only when the stored id is missing or dead.
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

	ApplyForumMetaThreadIDsFromProvisionDB(cfg)

	hints := ForumMetaHints{
		GeneralThreadID: t.GeneralThreadID,
		BotMetaThreadID: t.BotMetaThreadID,
		CallsThreadID:   t.CallsThreadID,
		StatusThreadID:  t.StatusThreadID,
	}
	prevGeneral := t.GeneralThreadID
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID, hints)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	effG := effectiveGeneralThreadID(prevGeneral, g)
	changed := t.BotMetaThreadID != m || t.CallsThreadID != c || t.StatusThreadID != s || t.GeneralThreadID != effG
	t.GeneralThreadID = effG
	t.BotMetaThreadID = m
	t.CallsThreadID = c
	t.StatusThreadID = s
	if changed {
		if err := cfg.SaveConfig(); err != nil {
			return fmt.Errorf("save config after forum topics: %w", err)
		}
		state.State.Logger.Info("standard forum meta topics provisioned and saved thread ids to config",
			zap.Int64("general_thread_id", effG),
			zap.Int64("bot_meta_thread_id", m),
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	} else {
		state.State.Logger.Debug("forum meta thread ids unchanged after reconcile; skipped config write",
			zap.Int64("general_thread_id", effG),
			zap.Int64("bot_meta_thread_id", m),
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	}
	syncForumMetaRegistryState(cfg)
	SeedMappedForumTopicsFromConfig(cfg)
	return nil
}
