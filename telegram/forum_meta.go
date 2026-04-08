package telegram

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
// existing topics by title. Telegram has no list-forum-topics API; IDs can exceed 250 in busy groups.
// We also seed from saved config thread ids first (see seedForumMetaIndexFromHints).
const forumMetaTopicScanLimit int64 = 4096

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

// Titles are plain text; icon appearance is handled by Telegram topic icon settings.
// Matching still uses normalizeForumMetaTopicTitleKey so legacy emoji-prefixed titles reuse correctly.
var standardForumMetaSpecs = []forumMetaSpec{
	{"General", "general purposes"},
	{"Bot's meta", "bot's meta information"},
	{"Calls", "displaying calls"},
	{"Status", "showing status broadcasts"},
}

// ForumMetaHints carries optional thread ids from config or bridge_provision_states (0 = unknown).
type ForumMetaHints struct {
	GeneralThreadID int64
	BotMetaThreadID int64
	CallsThreadID   int64
	StatusThreadID  int64
}

// ForumMetaProbeState marks which meta slots were already verified by sendMessage probe in the current
// provision run (avoids duplicate probes when some topics fail and we reconcile).
type ForumMetaProbeState struct {
	BotMeta bool
	Calls   bool
	Status  bool
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

// forumMetaLeadingEmojiPrefix returns the leading emoji cluster in the title (for matching forum icon stickers).
func forumMetaLeadingEmojiPrefix(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	i := 0
	for i < len(title) {
		r, w := utf8.DecodeRuneInString(title[i:])
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
		return ""
	}
	return title[:i]
}

func pickForumMetaIconCustomEmojiID(bot *gotgbot.Bot, specTitle string) string {
	want := forumMetaLeadingEmojiPrefix(specTitle)
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
	id := pickForumMetaIconCustomEmojiID(bot, spec.title)
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

// seedForumMetaIndexFromHints maps normalized titles for specific message_thread_ids (from config /
// registry) so we reuse topics whose ids are above the linear scan limit or not hit during scan order.
func seedForumMetaIndexFromHints(bot *gotgbot.Bot, chatID int64, skipThreadID int64, threadIDs []int64) map[string]int64 {
	idx := make(map[string]int64)
	seenTid := make(map[int64]struct{}, len(threadIDs))
	for _, tid := range threadIDs {
		if tid == 0 || (skipThreadID != 0 && tid == skipThreadID) {
			continue
		}
		if _, dup := seenTid[tid]; dup {
			continue
		}
		seenTid[tid] = struct{}{}
		name, ok, err := utils.TgFetchForumTopicName(bot, chatID, tid)
		if err != nil {
			if isGetForumTopicMissing(err) {
				continue
			}
			state.State.Logger.Debug("forum meta: seed hint getForumTopic failed",
				zap.Int64("thread_id", tid), zap.Error(err))
			continue
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
	return idx
}

// buildForumMetaTitleIndex maps normalized topic title (first occurrence) → message_thread_id.
// seed (if non-nil) is merged first so hinted thread ids win; scan only fills keys not yet present.
// skipThreadID avoids re-probing a thread we already resolved (e.g. General); pass 0 to scan all.
func buildForumMetaTitleIndex(bot *gotgbot.Bot, chatID int64, skipThreadID int64, seed map[string]int64) (map[string]int64, error) {
	idx := make(map[string]int64)
	for k, v := range seed {
		idx[k] = v
	}
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

func ensureFindOrCreateForumMetaTopic(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, idx map[string]int64) (threadID int64, created bool, err error) {
	wantKey := normalizeForumMetaTopicTitleKey(spec.title)
	if tid, ok := idx[wantKey]; ok {
		return tid, false, nil
	}
	name := utils.TruncateTelegramForumTopicName(spec.title)
	opts := &gotgbot.CreateForumTopicOpts{}
	if emojiID := pickForumMetaIconCustomEmojiID(bot, spec.title); emojiID != "" {
		opts.IconCustomEmojiId = emojiID
	} else {
		opts.IconColor = forumMetaTopicIconColor
	}
	createdTopic, err := bot.CreateForumTopic(chatID, name, opts)
	if err != nil {
		return 0, false, err
	}
	tid := createdTopic.MessageThreadId
	idx[wantKey] = tid
	return tid, true, nil
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
	idx, err := buildForumMetaTitleIndex(bot, chatID, 0, nil)
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

func resolveGeneralWithHint(bot *gotgbot.Bot, chatID int64, hintGeneral int64) (int64, error) {
	if hintGeneral != 0 {
		ok, err := forumThreadTitleMatchesSpec(bot, chatID, hintGeneral, standardForumMetaSpecs[0].title)
		if err != nil {
			return 0, err
		}
		if ok {
			return hintGeneral, nil
		}
	}
	return tryResolveGeneralForumThreadID(bot, chatID)
}

// resolveForumMetaRole uses a sendMessage probe when hint is set (unless preProbed), then the title
// index or createForumTopic (emoji title + icon sticker or blue icon_color).
func resolveForumMetaRole(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, hint int64, idx map[string]int64, preProbed bool) (threadID int64, created bool, err error) {
	wantKey := normalizeForumMetaTopicTitleKey(spec.title)
	if hint != 0 {
		if preProbed {
			idx[wantKey] = hint
			applyForumMetaTopicIcon(bot, chatID, hint, spec)
			return hint, false, nil
		}
		ok, err := forumMetaProbeTopicAlive(bot, chatID, hint, spec.title)
		if err != nil {
			return 0, false, err
		}
		if ok {
			idx[wantKey] = hint
			applyForumMetaTopicIcon(bot, chatID, hint, spec)
			return hint, false, nil
		}
	}
	if tid, ok := idx[wantKey]; ok {
		applyForumMetaTopicIcon(bot, chatID, tid, spec)
		return tid, false, nil
	}
	return ensureFindOrCreateForumMetaTopic(bot, chatID, spec, idx)
}

// CreateStandardForumMetaTopics resolves General when possible (find only; 0 = default General), then
// find-or-creates Bot's meta, Calls, Status. Pass hints from config/registry so existing thread ids are
// validated first (no duplicate topics when mappings are already correct).
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64, hints ForumMetaHints, preProbed ForumMetaProbeState) (general, botMeta, calls, status int64, err error) {
	generalID, err := resolveGeneralWithHint(bot, chatID, hints.GeneralThreadID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	seedTIDs := []int64{
		generalID,
		hints.BotMetaThreadID,
		hints.CallsThreadID,
		hints.StatusThreadID,
	}
	seed := seedForumMetaIndexFromHints(bot, chatID, generalID, seedTIDs)
	idx, err := buildForumMetaTitleIndex(bot, chatID, generalID, seed)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if generalID != 0 {
		idx[normalizeForumMetaTopicTitleKey(standardForumMetaSpecs[0].title)] = generalID
	}

	var ids [4]int64
	ids[0] = generalID
	for i := 1; i < len(standardForumMetaSpecs); i++ {
		h := int64(0)
		pre := false
		switch i {
		case 1:
			h = hints.BotMetaThreadID
			pre = preProbed.BotMeta
		case 2:
			h = hints.CallsThreadID
			pre = preProbed.Calls
		case 3:
			h = hints.StatusThreadID
			pre = preProbed.Status
		}
		tid, _, e := resolveForumMetaRole(bot, chatID, standardForumMetaSpecs[i], h, idx, pre)
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
	metaAll := t.BotMetaThreadID != 0 && t.CallsThreadID != 0 && t.StatusThreadID != 0
	hints := ForumMetaHints{
		GeneralThreadID: t.GeneralThreadID,
		BotMetaThreadID: t.BotMetaThreadID,
		CallsThreadID:   t.CallsThreadID,
		StatusThreadID:  t.StatusThreadID,
	}
	preProbed := ForumMetaProbeState{}
	if metaAll {
		bmOk, cOk, sOk, err := forumMetaProbeThreeMeta(bot, t.TargetChatID, t.BotMetaThreadID, t.CallsThreadID, t.StatusThreadID)
		if err != nil {
			return fmt.Errorf("forum meta: probe meta topics: %w", err)
		}
		if bmOk && cOk && sOk {
			for i, tid := range []int64{t.BotMetaThreadID, t.CallsThreadID, t.StatusThreadID} {
				applyForumMetaTopicIcon(bot, t.TargetChatID, tid, standardForumMetaSpecs[1+i])
			}
			syncForumMetaRegistryState(cfg)
			state.State.Logger.Debug("forum meta: probes ok, skipped topic creation",
				zap.Int64("bot_meta_thread_id", t.BotMetaThreadID),
				zap.Int64("calls_thread_id", t.CallsThreadID),
				zap.Int64("status_thread_id", t.StatusThreadID),
			)
			SeedMappedForumTopicsFromConfig(cfg)
			return nil
		}
		state.State.Logger.Warn("forum meta: probe failed for some topics; reprovisioning",
			zap.Int64("bot_meta_thread_id", t.BotMetaThreadID),
			zap.Int64("calls_thread_id", t.CallsThreadID),
			zap.Int64("status_thread_id", t.StatusThreadID),
			zap.Bool("bot_meta_ok", bmOk),
			zap.Bool("calls_ok", cOk),
			zap.Bool("status_ok", sOk),
		)
		preProbed = ForumMetaProbeState{BotMeta: bmOk, Calls: cOk, Status: sOk}
		hints.BotMetaThreadID, hints.CallsThreadID, hints.StatusThreadID = 0, 0, 0
		if bmOk {
			hints.BotMetaThreadID = t.BotMetaThreadID
		}
		if cOk {
			hints.CallsThreadID = t.CallsThreadID
		}
		if sOk {
			hints.StatusThreadID = t.StatusThreadID
		}
	}
	prevGeneral := t.GeneralThreadID
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID, hints, preProbed)
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
