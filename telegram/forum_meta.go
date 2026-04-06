package telegram

import (
	"fmt"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// forumMetaTopicScanLimit is how high we probe message_thread_id via getForumTopic when looking for
// existing standard meta topics. Groups with more topics may miss a duplicate title above this id.
const forumMetaTopicScanLimit int64 = 250

// telegramGeneralTopicThreadID is often message_thread_id 1 for the default "General" topic, but some
// forum setups return Not Found for getForumTopic(1). Config value 0 means "default General" (omit
// message_thread_id when sending).
const telegramGeneralTopicThreadID int64 = 1

type forumMetaSpec struct {
	title      string
	reuseLabel string
}

var standardForumMetaSpecs = []forumMetaSpec{
	{"General", "general purposes"},
	{"Bot's meta", "bot's meta information"},
	{"Calls", "displaying calls"},
	{"Status", "showing status broadcasts"},
}

func isGetForumTopicMissing(err error) bool {
	return utils.TgErrForumTopicOrThreadInvalid(err)
}

// normalizeForumMetaTopicTitleKey matches Telegram forum topic titles to our canonical names.
// Clients may show a single-letter prefix in the sidebar (e.g. "B Bot's meta"); getForumTopic names
// can include that prefix, so we strip "X " when X is A–Z before comparing to "Bot's meta", etc.
func normalizeForumMetaTopicTitleKey(title string) string {
	s := strings.TrimSpace(title)
	s = strings.TrimPrefix(s, "#")
	if len(s) >= 3 && s[1] == ' ' && s[0] >= 'A' && s[0] <= 'Z' {
		s = strings.TrimSpace(s[2:])
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
	created, err := bot.CreateForumTopic(chatID, spec.title, nil)
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
		if err == nil && ok && strings.TrimSpace(name) != "" {
			return telegramGeneralTopicThreadID, nil
		}
		if err != nil {
			lastProbeErr = err
			if !utils.TgErrForumTopicOrThreadInvalid(err) {
				return 0, fmt.Errorf("getForumTopic General (thread %d): %w", telegramGeneralTopicThreadID, err)
			}
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
		return nil
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
	}
	state.State.Logger.Info("standard forum meta topics provisioned and saved thread ids to config",
		zap.Int64("general_thread_id", g),
		zap.Int64("bot_meta_thread_id", m),
		zap.Int64("calls_thread_id", c),
		zap.Int64("status_thread_id", s),
	)
	return nil
}
