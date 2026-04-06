package telegram

import (
	"fmt"
	"strings"
	"time"

	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// forumMetaTopicScanLimit is how high we probe message_thread_id via getForumTopic when looking for
// existing standard meta topics. Groups with more topics may miss a duplicate title above this id.
const forumMetaTopicScanLimit int64 = 250

// telegramGeneralTopicThreadID is the message_thread_id of the default "General" topic in a forum supergroup
// (see Telegram Bot API: forum General topic uses this id).
const telegramGeneralTopicThreadID int64 = 1

type forumMetaSpec struct {
	title      string
	reuseLabel string
}

var standardForumMetaSpecs = []forumMetaSpec{
	{"General", "general purposes"},
	{"BotMeta", "bot's meta information"},
	{"Calls", "displaying calls"},
	{"Status", "showing status broadcasts"},
}

func isGetForumTopicMissing(err error) bool {
	return utils.TgErrForumTopicOrThreadInvalid(err)
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
		key := utils.TruncateTelegramForumTopicName(strings.TrimSpace(name))
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
	wantKey := utils.TruncateTelegramForumTopicName(strings.TrimSpace(spec.title))
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

// resolveGeneralForumThreadID finds the General topic only (never creates it). Telegram uses
// message_thread_id=1 for the default General topic; we retry for membership propagation, then fall back
// to a title scan if needed.
func resolveGeneralForumThreadID(bot *gotgbot.Bot, chatID int64) (int64, error) {
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
	// Fallback: locate by title (e.g. rare client / rename edge cases)
	idx, err := buildForumMetaTitleIndex(bot, chatID, 0)
	if err != nil {
		return 0, fmt.Errorf("scan forum topics looking for General: %w", err)
	}
	wantKey := utils.TruncateTelegramForumTopicName(standardForumMetaSpecs[0].title)
	if tid, ok := idx[wantKey]; ok {
		return tid, nil
	}
	if lastProbeErr != nil {
		return 0, fmt.Errorf("could not find General topic (expected thread id %d in a forum supergroup): %w", telegramGeneralTopicThreadID, lastProbeErr)
	}
	return 0, fmt.Errorf("could not find General topic in forum (expected thread id %d)", telegramGeneralTopicThreadID)
}

// CreateStandardForumMetaTopics resolves General (find only), then find-or-creates BotMeta, Calls, Status.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64) (general, botMeta, calls, status int64, err error) {
	generalID, err := resolveGeneralForumThreadID(bot, chatID)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	idx, err := buildForumMetaTitleIndex(bot, chatID, generalID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	idx[utils.TruncateTelegramForumTopicName(standardForumMetaSpecs[0].title)] = generalID

	_, sendErr := bot.SendMessage(chatID, "Reused existing topic for "+standardForumMetaSpecs[0].reuseLabel, &gotgbot.SendMessageOpts{
		MessageThreadId: generalID,
	})
	if sendErr != nil {
		state.State.Logger.Debug("forum meta: reuse notice send failed (General)",
			zap.Int64("thread_id", generalID),
			zap.Error(sendErr))
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
// (find only), find-or-creates BotMeta/Calls/Status, and persists config.
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
	anySet := t.GeneralThreadID != 0 || t.BotMetaThreadID != 0 || t.CallsThreadID != 0 || t.StatusThreadID != 0
	allSet := t.GeneralThreadID != 0 && t.BotMetaThreadID != 0 && t.CallsThreadID != 0 && t.StatusThreadID != 0
	if anySet && !allSet {
		return fmt.Errorf("telegram forum threads: set all four (general_thread_id, bot_meta_thread_id, calls_thread_id, status_thread_id) or omit all four for auto-provision")
	}
	if allSet {
		return nil
	}
	g, m, c, s, err := CreateStandardForumMetaTopics(bot, t.TargetChatID)
	if err != nil {
		return fmt.Errorf("create forum meta topics: %w", err)
	}
	t.GeneralThreadID = g
	t.BotMetaThreadID = m
	t.CallsThreadID = c
	t.StatusThreadID = s
	if err := cfg.SaveConfig(); err != nil {
		return fmt.Errorf("save config after forum topics: %w", err)
	}
	state.State.Logger.Info("standard forum meta topics provisioned and saved thread ids to config",
		zap.Int64("general_thread_id", g),
		zap.Int64("bot_meta_thread_id", m),
		zap.Int64("calls_thread_id", c),
		zap.Int64("status_thread_id", s),
	)
	return nil
}
