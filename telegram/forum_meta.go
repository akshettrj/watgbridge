package telegram

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"watgbridge/bridge"
	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// forumMetaTopicScanLimit is how high we probe message_thread_id via getForumTopic when looking for
// existing standard meta topics. Groups with more topics may miss a duplicate title above this id.
const forumMetaTopicScanLimit int64 = 250

// telegramGeneralTopicThreadID is message_thread_id 1 for the default "General" topic in Telegram
// forums. Config value 0 means "default General" (omit message_thread_id when sending).
const telegramGeneralTopicThreadID int64 = 1

// forumMetaTopicIconColor is Telegram Bot API icon_color 0x6FB9F0 (light blue) — same family as the
// default forum topic chip style (see createForumTopic icon_color allowed values).
const forumMetaTopicIconColor int64 = 7322096

type forumMetaSpec struct {
	title      string
	reuseLabel string
}

// Titles include leading emoji + Bot API icon_color so topics match the usual forum look. Matching
// still uses normalizeForumMetaTopicTitleKey (emoji / "B " prefixes stripped) so plain "Bot's meta"
// topics reuse correctly.
var standardForumMetaSpecs = []forumMetaSpec{
	{"General", "general purposes"},
	{"💻 Bot's meta", "bot's meta information"},
	{"🔮 Calls", "displaying calls"},
	{"📱 Status", "showing status broadcasts"},
}

func isGetForumTopicMissing(err error) bool {
	return utils.TgErrForumTopicOrThreadInvalid(err)
}

// isEmojiSequenceRune matches leading decoration many clients add before the real title (e.g. "💻 Bot's meta").
func isEmojiSequenceRune(r rune) bool {
	switch r {
	case 0x200D, 0xFE0F: // ZWJ, VS16
		return true
	}
	if r >= 0x1F000 && r <= 0x1FAFF {
		return true
	}
	if r >= 0x2600 && r <= 0x27BF {
		return true
	}
	if r >= 0x231A && r <= 0x23FF {
		return true
	}
	if r >= 0x2B50 && r <= 0x2B55 {
		return true
	}
	if r >= 0x1F600 && r <= 0x1F64F {
		return true
	}
	// Regional indicators (flag sequences)
	if r >= 0x1F1E6 && r <= 0x1F1FF {
		return true
	}
	return false
}

// stripLeadingEmojiCluster removes one leading emoji cluster (incl. ZWJ/VS16 continuations) from the title.
func stripLeadingEmojiCluster(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if w == 0 {
			break
		}
		if isEmojiSequenceRune(r) {
			i += w
			continue
		}
		break
	}
	if i == 0 {
		return s
	}
	return strings.TrimSpace(s[i:])
}

// normalizeForumMetaTopicTitleKey matches Telegram forum topic titles to our canonical names.
// Clients may show a single-letter prefix in the sidebar (e.g. "B Bot's meta"); getForumTopic names
// can include that prefix, so we strip "X " when X is A–Z before comparing to "Bot's meta", etc.
// Users often set emoji icons on topics (e.g. "💻 Bot's meta"); we strip leading emoji so those reuse
// the same logical topic as plain "Bot's meta".
func normalizeForumMetaTopicTitleKey(title string) string {
	s := strings.TrimSpace(title)
	s = strings.ReplaceAll(s, "\u2019", "'")
	s = strings.ReplaceAll(s, "\u2018", "'")
	for {
		before := s
		s = strings.TrimSpace(s)
		s = strings.TrimPrefix(s, "#")
		s = strings.TrimSpace(s)
		if len(s) >= 3 && s[1] == ' ' && s[0] >= 'A' && s[0] <= 'Z' {
			s = strings.TrimSpace(s[2:])
			continue
		}
		next := stripLeadingEmojiCluster(s)
		if next != s {
			s = next
			continue
		}
		if s == before {
			break
		}
	}
	return utils.TruncateTelegramForumTopicName(strings.TrimSpace(s))
}

// buildForumMetaTitleIndex maps normalized topic title (first occurrence) → message_thread_id.
// skipThreadID avoids re-probing a thread we already resolved (e.g. General); pass 0 to scan all.
func buildForumMetaTitleIndex(bot *gotgbot.Bot, chatID int64, skipThreadID int64) (map[string]int64, error) {
	idx := make(map[string]int64)
	for tid := int64(1); tid <= forumMetaTopicScanLimit; tid++ {
		if skipThreadID != 0 && tid == skipThreadID {
			continue
		}
		name, ok, err := utils.TgFetchForumTopicName(bot, chatID, tid)
		if err != nil {
			if isGetForumTopicMissing(err) {
				continue
			}
			return nil, fmt.Errorf("getForumTopic thread %d: %w", tid, err)
		}
		if !ok {
			continue
		}
		key := normalizeForumMetaTopicTitleKey(name)
		if key == "" {
			continue
		}
		if _, have := idx[key]; !have {
			idx[key] = tid
		}
	}
	return idx, nil
}

func ensureFindOrCreateForumMetaTopic(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, idx map[string]int64) (threadID int64, err error) {
	wantKey := normalizeForumMetaTopicTitleKey(spec.title)
	if tid, ok := idx[wantKey]; ok {
		_, sendErr := bot.SendMessage(chatID, "Reused existing topic for "+spec.reuseLabel, &gotgbot.SendMessageOpts{
			MessageThreadId: tid,
		})
		if sendErr != nil {
			state.State.Logger.Debug("forum meta: reuse notice send failed",
				zap.String("title", spec.title),
				zap.Int64("thread_id", tid),
				zap.Error(sendErr))
		}
		return tid, nil
	}
	name := utils.TruncateTelegramForumTopicName(spec.title)
	opts := &gotgbot.CreateForumTopicOpts{IconColor: forumMetaTopicIconColor}
	created, err := bot.CreateForumTopic(chatID, name, opts)
	if err != nil {
		return 0, err
	}
	tid := created.MessageThreadId
	idx[wantKey] = tid
	return tid, nil
}

// tryResolveGeneralForumThreadID finds the General topic only (never creates it). If Telegram does not
// expose a resolvable thread id (common: getForumTopic(1) Not Found), returns (0, nil) — callers should
// omit message_thread_id to target the default General topic.
func tryResolveGeneralForumThreadID(bot *gotgbot.Bot, chatID int64) (int64, error) {
	var lastProbeErr error
	for attempt := 0; attempt < 12; attempt++ {
		name, ok, err := utils.TgFetchForumTopicName(bot, chatID, telegramGeneralTopicThreadID)
		if err != nil {
			lastProbeErr = err
			if !utils.TgErrForumTopicOrThreadInvalid(err) {
				return 0, fmt.Errorf("getForumTopic General (thread %d): %w", telegramGeneralTopicThreadID, err)
			}
			time.Sleep(400 * time.Millisecond)
			continue
		}
		if ok && strings.TrimSpace(name) != "" {
			// Default General is always message_thread_id 1 in Telegram forums.
			return telegramGeneralTopicThreadID, nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	idx, err := buildForumMetaTitleIndex(bot, chatID, 0)
	if err != nil {
		return 0, fmt.Errorf("scan forum topics looking for General: %w", err)
	}
	wantKey := normalizeForumMetaTopicTitleKey(standardForumMetaSpecs[0].title)
	if tid, ok := idx[wantKey]; ok {
		return tid, nil
	}
	if lastProbeErr != nil {
		state.State.Logger.Debug("forum meta: General not resolvable via getForumTopic; using config thread id 0 (omit message_thread_id)",
			zap.Error(lastProbeErr))
	} else {
		state.State.Logger.Debug("forum meta: General not found by title scan; using config thread id 0 (omit message_thread_id)")
	}
	return 0, nil
}

// CreateStandardForumMetaTopics resolves General when possible (find only; 0 = default General), then
// find-or-creates Bot's meta, Calls, Status.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64) (general, botMeta, calls, status int64, err error) {
	generalID, err := tryResolveGeneralForumThreadID(bot, chatID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	idx, err := buildForumMetaTitleIndex(bot, chatID, generalID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if generalID != 0 {
		idx[normalizeForumMetaTopicTitleKey(standardForumMetaSpecs[0].title)] = generalID
	}

	if generalID != 0 {
		_, sendErr := bot.SendMessage(chatID, "Reused existing topic for "+standardForumMetaSpecs[0].reuseLabel, &gotgbot.SendMessageOpts{
			MessageThreadId: generalID,
		})
		if sendErr != nil {
			state.State.Logger.Debug("forum meta: reuse notice send failed (General)",
				zap.Int64("thread_id", generalID),
				zap.Error(sendErr))
		}
	}

	var ids [4]int64
	ids[0] = generalID
	for i := 1; i < len(standardForumMetaSpecs); i++ {
		tid, e := ensureFindOrCreateForumMetaTopic(bot, chatID, standardForumMetaSpecs[i], idx)
		if e != nil {
			if i == 1 {
				return ids[0], 0, 0, 0, e
			}
			if i == 2 {
				return ids[0], ids[1], 0, 0, e
			}
			return ids[0], ids[1], ids[2], 0, e
		}
		ids[i] = tid
	}
	return ids[0], ids[1], ids[2], ids[3], nil
}

// forumThreadTitleMatchesSpec returns true if getForumTopic resolves and the name matches canonicalTitle
// after normalization (emoji / letter prefixes, etc.).
func forumThreadTitleMatchesSpec(bot *gotgbot.Bot, chatID, threadID int64, canonicalTitle string) (bool, error) {
	if threadID == 0 {
		return true, nil
	}
	name, ok, err := utils.TgFetchForumTopicName(bot, chatID, threadID)
	if err != nil {
		if isGetForumTopicMissing(err) {
			return false, nil
		}
		return false, err
	}
	if !ok {
		return false, nil
	}
	want := normalizeForumMetaTopicTitleKey(canonicalTitle)
	got := normalizeForumMetaTopicTitleKey(name)
	return got == want, nil
}

// EnsureForumMetaTopicsProvisioned requires a forum target group with Manage topics, then resolves General
// when possible (0 = default General), find-or-creates Bot's meta/Calls/Status, and persists config.
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
	metaAny := t.BotMetaThreadID != 0 || t.CallsThreadID != 0 || t.StatusThreadID != 0
	metaAll := t.BotMetaThreadID != 0 && t.CallsThreadID != 0 && t.StatusThreadID != 0
	if metaAny && !metaAll {
		return fmt.Errorf("telegram forum threads: set all three (bot_meta_thread_id, calls_thread_id, status_thread_id) or omit all three for auto-provision")
	}
	if metaAll {
		botMetaOK, err := forumThreadTitleMatchesSpec(bot, t.TargetChatID, t.BotMetaThreadID, standardForumMetaSpecs[1].title)
		if err != nil {
			return fmt.Errorf("check existing bot_meta_thread_id=%d: %w", t.BotMetaThreadID, err)
		}
		callsOK, err := forumThreadTitleMatchesSpec(bot, t.TargetChatID, t.CallsThreadID, standardForumMetaSpecs[2].title)
		if err != nil {
			return fmt.Errorf("check existing calls_thread_id=%d: %w", t.CallsThreadID, err)
		}
		statusOK, err := forumThreadTitleMatchesSpec(bot, t.TargetChatID, t.StatusThreadID, standardForumMetaSpecs[3].title)
		if err != nil {
			return fmt.Errorf("check existing status_thread_id=%d: %w", t.StatusThreadID, err)
		}
		if botMetaOK && callsOK && statusOK {
			return nil
		}
		state.State.Logger.Warn("forum meta thread ids stale or wrong topic; reprovisioning",
			zap.Int64("bot_meta_thread_id", t.BotMetaThreadID),
			zap.Int64("calls_thread_id", t.CallsThreadID),
			zap.Int64("status_thread_id", t.StatusThreadID),
			zap.Bool("bot_meta_ok", botMetaOK),
			zap.Bool("calls_ok", callsOK),
			zap.Bool("status_ok", statusOK),
		)
	}
	prevGeneral := t.GeneralThreadID
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	if prevGeneral != 0 {
		t.GeneralThreadID = prevGeneral
	} else {
		t.GeneralThreadID = g
	}
	t.BotMetaThreadID = m
	t.CallsThreadID = c
	t.StatusThreadID = s
	if err := cfg.SaveConfig(); err != nil {
		return fmt.Errorf("save config after forum topics: %w", err)
	}
	if cfg.Telegram.BridgeRegistryID != 0 {
		if err := database.BridgeProvisionSet(
			cfg.Telegram.BridgeRegistryID,
			t.GeneralThreadID,
			t.BotMetaThreadID,
			t.CallsThreadID,
			t.StatusThreadID,
			"ok",
			"",
		); err != nil {
			state.State.Logger.Warn("forum meta: could not sync thread ids to bridge registry DB (child YAML may reprovision on restart)",
				zap.Uint("bridge_registry_id", cfg.Telegram.BridgeRegistryID),
				zap.Error(err))
		}
		// Multi-mode: per-bridge DB is separate from the registry; BridgeProvisionSet is routed to the
		// registry when WATG_REGISTRY_SQLITE_PATH is set. Sidecar is a fallback if registry open fails.
		if cfg.Path != "" {
			if err := bridge.WriteProvisionSidecar(filepath.Dir(cfg.Path), cfg.Telegram.BridgeRegistryID,
				t.GeneralThreadID, t.BotMetaThreadID, t.CallsThreadID, t.StatusThreadID); err != nil {
				state.State.Logger.Warn("forum meta: could not write provision sidecar (parent may reprovision forum topics on restart)",
					zap.Uint("bridge_registry_id", cfg.Telegram.BridgeRegistryID),
					zap.Error(err))
			}
		}
	}
	state.State.Logger.Info("standard forum meta topics provisioned and saved thread ids to config",
		zap.Int64("general_thread_id", g),
		zap.Int64("bot_meta_thread_id", m),
		zap.Int64("calls_thread_id", c),
		zap.Int64("status_thread_id", s),
	)
	return nil
}
