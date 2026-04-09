package telegram

import (
	"fmt"
	"strings"
	"sync"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// telegramGeneralTopicThreadID is the usual message_thread_id Telegram uses for the default General
// topic; resolveGeneralThreadID probes it when no other General thread is found.
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
	forumMetaSlotGeneral = "general"
	forumMetaSlotBotMeta = "bot_meta"
	forumMetaSlotCalls   = "calls"
	forumMetaSlotStatus  = "status"
)

const (
	forumMetaChatKeyBotMeta = "bot_meta"
	forumMetaChatKeyCalls   = "calls"
	forumMetaChatKeyStatus  = "status@broadcast"
)

func forumMetaProvisionBridgeID(cfg *state.Config) uint {
	if cfg.Telegram.BridgeRegistryID != 0 {
		return cfg.Telegram.BridgeRegistryID
	}
	return singleModeForumMetaBridgeID
}

// Canonical topic titles are emoji-prefixed. Aliases keep compatibility with old/plain names.
var standardForumMetaSpecs = []forumMetaSpec{
	{slot: forumMetaSlotGeneral, title: "General"},
	{
		slot:      forumMetaSlotBotMeta,
		title:     "💻 Bot's meta",
		iconEmoji: "💻",
		aliases:   []string{"Bot's meta", "Bots meta"},
		chatKeys:  []string{forumMetaChatKeyBotMeta},
	},
	{
		slot:      forumMetaSlotCalls,
		title:     "🔮 Calls",
		iconEmoji: "🔮",
		aliases:   []string{"Calls"},
		chatKeys:  []string{forumMetaChatKeyCalls},
	},
	{
		slot:      forumMetaSlotStatus,
		title:     "📱 Status",
		iconEmoji: "📱",
		aliases:   []string{"Status"},
		chatKeys:  []string{forumMetaChatKeyStatus, "status"},
	},
}

// ForumMetaHints carries persisted meta topic thread ids (0 = unknown). Prefer loading from
// bridge_provision_states via ApplyForumMetaThreadIDsFromProvisionDB before provisioning.
// General hub is resolved at provision time into state.State.ForumHubMessageThreadID, not stored here.
type ForumMetaHints struct {
	BotMetaThreadID int64
	CallsThreadID   int64
	StatusThreadID  int64
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
func ApplyForumMetaThreadIDsFromProvisionDB(cfg *state.Config) {
	t := &cfg.Telegram
	p, err := database.BridgeProvisionGet(forumMetaProvisionBridgeID(cfg))
	if err != nil || p == nil {
		return
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
		ok, err := forumMetaTopicExists(bot, chatID, hint)
		if err != nil {
			return 0, err
		}
		if ok {
			return hint, nil
		}
	}
	ok, err := forumMetaTopicExists(bot, chatID, telegramGeneralTopicThreadID)
	if err != nil {
		return 0, err
	}
	if ok {
		return telegramGeneralTopicThreadID, nil
	}
	return 0, nil
}

// forumMetaReservedGeneralSlots returns thread ids that are forbidden for Bot's meta / Calls / Status.
// Probes cannot tell topics apart; reserve the resolved General hub id and the usual default thread id.
func forumMetaReservedGeneralSlots(resolvedGeneral int64) []int64 {
	out := []int64{telegramGeneralTopicThreadID}
	if resolvedGeneral != 0 && resolvedGeneral != telegramGeneralTopicThreadID {
		out = append(out, resolvedGeneral)
	}
	return out
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

func forumMetaRegistrySlotCandidates(cfg *state.Config, chatID int64, spec forumMetaSpec) []int64 {
	if cfg == nil || chatID == 0 {
		return nil
	}
	rows, err := database.BridgeProvisionListByTargetChat(chatID)
	if err != nil {
		state.State.Logger.Debug("forum meta: sibling provision hints lookup failed",
			zap.String("slot", spec.slot),
			zap.Int64("target_chat_id", chatID),
			zap.Error(err))
		return nil
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		switch spec.slot {
		case forumMetaSlotBotMeta:
			if r.BotMetaThreadID != 0 {
				out = append(out, r.BotMetaThreadID)
			}
		case forumMetaSlotCalls:
			if r.CallsThreadID != 0 {
				out = append(out, r.CallsThreadID)
			}
		case forumMetaSlotStatus:
			if r.StatusThreadID != 0 {
				out = append(out, r.StatusThreadID)
			}
		}
	}
	return out
}

func forumMetaSlotCandidates(cfg *state.Config, chatID int64, spec forumMetaSpec, hint int64) []int64 {
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
	for _, id := range forumMetaRegistrySlotCandidates(cfg, chatID, spec) {
		add(id)
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

func provisionMetaSlot(bot *gotgbot.Bot, cfg *state.Config, chatID int64, spec forumMetaSpec, threadID int64, reserved []int64) (int64, error) {
	candidates := forumMetaSlotCandidates(cfg, chatID, spec, threadID)
	for _, candidate := range candidates {
		if threadHintConflictsWithReserved(candidate, reserved) {
			continue
		}
		ok, err := forumMetaTopicMatchesSpec(bot, chatID, candidate, spec)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		reconcileForumMetaTopicStyle(bot, chatID, candidate, spec)
		return candidate, nil
	}
	tid, err := createForumMetaTopic(bot, chatID, spec)
	if err != nil {
		return 0, err
	}
	reconcileForumMetaTopicStyle(bot, chatID, tid, spec)
	return tid, nil
}

// CreateStandardForumMetaTopics resolves General (getForumTopic name match; never creates), then for
// Bot's meta, Calls, Status: verify saved thread id matches the expected topic title; if missing,
// wrong title, or hint conflicts with General/another slot, create the topic and apply icon.
// If cfg is non-nil, updates cfg.Telegram and state.State.ForumHubMessageThreadID incrementally and
// syncs bridge_provision_states after each step so a crash mid-run cannot lose progress or duplicate on restart.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64, hints ForumMetaHints, cfg *state.Config) (general, botMeta, calls, status int64, err error) {
	g, err := resolveGeneralThreadID(bot, chatID, 0)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if cfg != nil {
		state.State.ForumHubMessageThreadID = g
		syncForumMetaRegistryState(cfg)
	}
	reserved := forumMetaReservedGeneralSlots(g)
	specBotMeta := standardForumMetaSpecs[1]
	specCalls := standardForumMetaSpecs[2]
	specStatus := standardForumMetaSpecs[3]
	m, err := provisionMetaSlot(bot, cfg, chatID, specBotMeta, hints.BotMetaThreadID, reserved)
	if err != nil {
		return g, 0, 0, 0, err
	}
	if cfg != nil {
		cfg.Telegram.BotMetaThreadID = m
		syncForumMetaRegistryState(cfg)
	}
	reserved = append(reserved, m)
	c, err := provisionMetaSlot(bot, cfg, chatID, specCalls, hints.CallsThreadID, reserved)
	if err != nil {
		return g, m, 0, 0, err
	}
	if cfg != nil {
		cfg.Telegram.CallsThreadID = c
		syncForumMetaRegistryState(cfg)
	}
	reserved = append(reserved, c)
	s, err := provisionMetaSlot(bot, cfg, chatID, specStatus, hints.StatusThreadID, reserved)
	if err != nil {
		return g, m, c, 0, err
	}
	if cfg != nil {
		cfg.Telegram.StatusThreadID = s
		syncForumMetaRegistryState(cfg)
	}
	return g, m, c, s, nil
}

func syncForumMetaRegistryState(cfg *state.Config) {
	t := &cfg.Telegram
	bid := forumMetaProvisionBridgeID(cfg)
	if err := database.BridgeProvisionSet(
		bid,
		0, // general_thread_id column unused; hub id lives in state.State.ForumHubMessageThreadID
		t.BotMetaThreadID,
		t.CallsThreadID,
		t.StatusThreadID,
		"ok",
		"",
	); err != nil {
		state.State.Logger.Warn("forum meta: could not persist thread ids to bridge_provision_states",
			zap.Uint("provision_bridge_id", bid),
			zap.Error(err))
	}
}

// EnsureForumMetaTopicsProvisioned loads persisted thread ids from bridge_provision_states when available,
// then verifies each slot via getForumTopic (title match); creates topics when missing or wrong.
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
	if err := ValidateTargetForumAndBotRights(bot, t.TargetChatID); err != nil {
		msg := "WaTgBridge forum setup failed: " + err.Error()
		state.State.Logger.Error(msg)
		sendTargetCheckFailure(msg)
		return err
	}

	ApplyForumMetaThreadIDsFromProvisionDB(cfg)

	hints := ForumMetaHints{
		BotMetaThreadID: t.BotMetaThreadID,
		CallsThreadID:   t.CallsThreadID,
		StatusThreadID:  t.StatusThreadID,
	}
	prevB, prevC, prevS := t.BotMetaThreadID, t.CallsThreadID, t.StatusThreadID
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID, hints, cfg)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	// CreateStandardForumMetaTopics already assigned t.*, ForumHubMessageThreadID, and synced.
	changed := prevB != m || prevC != c || prevS != s
	if changed {
		state.State.Logger.Info("standard forum meta topics provisioned",
			zap.Int64("forum_hub_message_thread_id", g),
			zap.Int64("bot_meta_thread_id", m),
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	} else {
		state.State.Logger.Debug("forum meta thread ids unchanged after reconcile",
			zap.Int64("forum_hub_message_thread_id", g),
			zap.Int64("bot_meta_thread_id", m),
			zap.Int64("calls_thread_id", c),
			zap.Int64("status_thread_id", s),
		)
	}
	SeedMappedForumTopicsFromConfig(cfg)
	return nil
}
