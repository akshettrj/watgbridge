package telegram

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// telegramGeneralTopicThreadID is Telegram's default forum "General" thread id. We reserve it so
// managed meta slots (Calls/Status) never bind to General.
const telegramGeneralTopicThreadID int64 = 1

// forumMetaTopicIconColor is Telegram Bot API icon_color 0x6FB9F0 (light blue).
const forumMetaTopicIconColor int64 = 7322096

type forumMetaSpec struct {
	slot  string
	title string
	// iconEmoji is matched against GetForumTopicIconStickers; it is not part of the topic title.
	iconEmoji string
	aliases   []string
	// chatKeys are ChatThreadPair IDs that may already map this logical slot.
	chatKeys []string
}

// singleModeForumMetaBridgeID is the bridge_provision_states row used when BridgeRegistryID is 0
// (single deployment: thread ids live in the local app DB, not the multi-mode registry).
const singleModeForumMetaBridgeID uint = 1

const (
	forumMetaSlotCalls  = "calls"
	forumMetaSlotStatus = "status"
)

const (
	forumMetaChatKeyCalls  = "calls"
	forumMetaChatKeyStatus = "status@broadcast"
)

func forumMetaProvisionBridgeID(cfg *state.Config) uint {
	if cfg.Telegram.BridgeRegistryID != 0 {
		return cfg.Telegram.BridgeRegistryID
	}
	return singleModeForumMetaBridgeID
}

// Topic names stay plain; icon emoji is applied via forum icon.
var standardForumMetaSpecs = []forumMetaSpec{
	{
		slot:      forumMetaSlotCalls,
		title:     "Calls",
		iconEmoji: "🔮",
		aliases:   []string{"Calls"},
		chatKeys:  []string{forumMetaChatKeyCalls},
	},
	{
		slot:      forumMetaSlotStatus,
		title:     "Status",
		iconEmoji: "📱",
		aliases:   []string{"Status"},
		chatKeys:  []string{forumMetaChatKeyStatus, "status"},
	},
}

func forumMetaSpecBySlot(slot string) (forumMetaSpec, bool) {
	for _, spec := range standardForumMetaSpecs {
		if spec.slot == slot {
			return spec, true
		}
	}
	return forumMetaSpec{}, false
}

// ForumMetaHints carries persisted meta topic thread ids (0 = unknown). Prefer loading from
// bridge_provision_states via ApplyForumMetaThreadIDsFromProvisionDB before provisioning.
type ForumMetaHints struct {
	CallsThreadID  int64
	StatusThreadID int64
}

var forumMetaIconStickersMu sync.Mutex
var forumMetaIconStickers []gotgbot.Sticker

// forumMetaEnsureMu serializes EnsureForumMetaTopicsProvisioned / CreateStandardForumMetaTopics
// so concurrent runs cannot create duplicate meta topics.
var forumMetaEnsureMu sync.Mutex

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

func reconcileForumMetaTopicStyle(bot *gotgbot.Bot, chatID, threadID int64, spec forumMetaSpec) {
	if threadID == 0 {
		return
	}
	name := utils.TruncateTelegramForumTopicName(spec.title)
	opts := &gotgbot.EditForumTopicOpts{Name: name}
	if id := pickForumMetaIconCustomEmojiID(bot, spec); id != "" {
		idCopy := id
		opts.IconCustomEmojiId = &idCopy
	}
	_, err := bot.EditForumTopic(chatID, threadID, opts)
	if err != nil && !utils.TgEditForumTopicUnchanged(err) {
		state.State.Logger.Debug("forum meta: EditForumTopic style reconcile failed",
			zap.String("slot", spec.slot),
			zap.String("title", spec.title),
			zap.Int64("thread_id", threadID),
			zap.Error(err))
	}
}

// ApplyForumMetaThreadIDsFromProvisionDB loads telegram thread ids from bridge_provision_states
// (registry bridge_id when BridgeRegistryID is set, else singleModeForumMetaBridgeID on the local DB).
// Call after DB connect and before EnsureForumMetaTopicsProvisioned.
func ApplyForumMetaThreadIDsFromProvisionDB(cfg *state.Config) error {
	t := &cfg.Telegram
	bid := forumMetaProvisionBridgeID(cfg)
	p, err := database.BridgeProvisionGet(bid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state.State.Logger.Debug("forum meta: no bridge_provision_states row; using runtime/local hints",
				zap.Uint("provision_bridge_id", bid))
			return nil
		}
		return err
	}
	if p == nil {
		return nil
	}
	t.BotMetaThreadID = 0
	if p.CallsThreadID != 0 {
		t.CallsThreadID = p.CallsThreadID
	}
	if p.StatusThreadID != 0 {
		t.StatusThreadID = p.StatusThreadID
	}
	state.State.Logger.Debug("forum meta: loaded thread ids from bridge_provision_states",
		zap.Uint("provision_bridge_id", bid),
		zap.Int64("calls_thread_id", t.CallsThreadID),
		zap.Int64("status_thread_id", t.StatusThreadID))
	return nil
}

// threadHintConflictsWithReserved is true when a persisted thread id is already used by General
// or an earlier meta slot (same id cannot be two topics).
func threadHintConflictsWithReserved(threadID int64, reserved []int64) bool {
	if threadID == 0 {
		return false
	}
	for _, id := range reserved {
		if id != 0 && threadID == id {
			return true
		}
	}
	return false
}

func forumMetaSlotCandidates(chatID int64, spec forumMetaSpec, hint int64) []int64 {
	var out []int64
	seen := map[int64]struct{}{}
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(hint)
	for _, key := range spec.chatKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tid, found, err := database.ChatThreadGetTgFromWa(key, chatID)
		if err != nil {
			state.State.Logger.Debug("forum meta: chat mapping candidate lookup failed",
				zap.String("slot", spec.slot),
				zap.String("wa_key", key),
				zap.Error(err))
			continue
		}
		if found {
			add(tid)
		}
	}
	return out
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

func provisionMetaSlot(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, threadID int64, reserved []int64) (int64, error) {
	candidates := forumMetaSlotCandidates(chatID, spec, threadID)
	hintProbeUnknown := false
	for _, candidate := range candidates {
		if threadHintConflictsWithReserved(candidate, reserved) {
			continue
		}
		result, probeErr := forumMetaProbeThread(bot, chatID, candidate, spec.slot, spec)
		switch result {
		case forumMetaThreadProbeValid:
			reconcileForumMetaTopicStyle(bot, chatID, candidate, spec)
			return candidate, nil
		case forumMetaThreadProbeMissing:
			continue
		default:
			// Inconclusive probe must not create duplicates. If the authoritative slot hint
			// is inconclusive, keep it and skip create for this slot on this run.
			if candidate == threadID && threadID != 0 {
				hintProbeUnknown = true
				state.State.Logger.Warn("forum meta: hint send probe inconclusive; keeping current thread id",
					zap.String("slot", spec.slot),
					zap.Int64("thread_id", candidate),
					zap.Error(probeErr))
				continue
			}
			state.State.Logger.Debug("forum meta: candidate send probe inconclusive; skipping candidate",
				zap.String("slot", spec.slot),
				zap.Int64("thread_id", candidate),
				zap.Error(probeErr))
			continue
		}
	}
	if hintProbeUnknown && threadID != 0 {
		return threadID, nil
	}
	tid, err := createForumMetaTopic(bot, chatID, spec)
	if err != nil {
		return 0, err
	}
	reconcileForumMetaTopicStyle(bot, chatID, tid, spec)
	return tid, nil
}

// CreateStandardForumMetaTopics manages only Calls and Status slots.
// General is not tracked; Bot's meta is intentionally not used.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64, hints ForumMetaHints, cfg *state.Config) (calls, status int64, err error) {
	if !forumMetaManagementEnabledForChat(chatID) {
		// Test-gated mode: skip forum-meta management for chats outside allowlist.
		return hints.CallsThreadID, hints.StatusThreadID, nil
	}
	reserved := []int64{telegramGeneralTopicThreadID}
	specCalls := standardForumMetaSpecs[0]
	specStatus := standardForumMetaSpecs[1]
	c, err := provisionMetaSlot(bot, chatID, specCalls, hints.CallsThreadID, reserved)
	if err != nil {
		return 0, 0, err
	}
	if cfg != nil {
		cfg.Telegram.BotMetaThreadID = 0
		cfg.Telegram.CallsThreadID = c
		if err := syncForumMetaRegistryState(cfg); err != nil {
			return c, 0, err
		}
	}
	reserved = append(reserved, c)
	s, err := provisionMetaSlot(bot, chatID, specStatus, hints.StatusThreadID, reserved)
	if err != nil {
		return c, 0, err
	}
	if cfg != nil {
		cfg.Telegram.StatusThreadID = s
		if err := syncForumMetaRegistryState(cfg); err != nil {
			return c, s, err
		}
	}
	return c, s, nil
}

func syncForumMetaRegistryState(cfg *state.Config) error {
	t := &cfg.Telegram
	bid := forumMetaProvisionBridgeID(cfg)
	t.BotMetaThreadID = 0
	if err := database.BridgeProvisionSet(
		bid,
		0, // general_thread_id is intentionally unused
		0, // bot_meta_thread_id is intentionally unused
		t.CallsThreadID,
		t.StatusThreadID,
		"ok",
		"",
	); err != nil {
		return fmt.Errorf("persist bridge_provision_states bridge_id=%d: %w", bid, err)
	}
	return nil
}

// EnsureForumMetaTopicsProvisioned loads persisted thread ids from bridge_provision_states when available,
// then verifies each slot with configured probe strategy; creates topics only when a slot is confirmed missing.
func EnsureForumMetaTopicsProvisioned() error {
	forumMetaEnsureMu.Lock()
	defer forumMetaEnsureMu.Unlock()

	cfg := state.State.Config
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := &cfg.Telegram
	if t.TargetChatID == 0 {
		return fmt.Errorf("telegram.target_chat_id is required")
	}
	if !forumMetaManagementEnabledForChat(t.TargetChatID) {
		state.State.Logger.Info("forum meta management skipped for target chat (not in allowlist)",
			zap.Int64("target_chat_id", t.TargetChatID))
		return nil
	}
	if err := ValidateTargetForumAndBotRights(bot, t.TargetChatID); err != nil {
		msg := "WaTgBridge forum setup failed: " + err.Error()
		state.State.Logger.Error(msg)
		sendTargetCheckFailure(msg)
		return err
	}

	if err := ApplyForumMetaThreadIDsFromProvisionDB(cfg); err != nil {
		return fmt.Errorf("load forum meta thread ids from provision db: %w", err)
	}

	hints := ForumMetaHints{
		CallsThreadID:  t.CallsThreadID,
		StatusThreadID: t.StatusThreadID,
	}
	prevC, prevS := t.CallsThreadID, t.StatusThreadID
	state.State.Logger.Info("forum meta reconcile started",
		zap.Int64("calls_hint_thread_id", prevC),
		zap.Int64("status_hint_thread_id", prevS))
	c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID, hints, cfg)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	// CreateStandardForumMetaTopics already assigned t.* and synced.
	changed := prevC != c || prevS != s
	if changed {
		state.State.Logger.Info("forum meta topics provisioned",
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	} else {
		state.State.Logger.Info("forum meta thread ids unchanged after reconcile",
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	}
	SeedMappedForumTopicsFromConfig(cfg)
	return nil
}
