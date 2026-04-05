package telegram

import (
	"fmt"
	"strings"

	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

// forumMetaTopicScanLimit is how high we probe message_thread_id via getForumTopic when looking for
// existing standard meta topics. Groups with more topics may miss a duplicate title above this id.
const forumMetaTopicScanLimit int64 = 250

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
func buildForumMetaTitleIndex(bot *gotgbot.Bot, chatID int64) (map[string]int64, error) {
	idx := make(map[string]int64)
	for tid := int64(1); tid <= forumMetaTopicScanLimit; tid++ {
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

func ensureOneForumMetaTopic(bot *gotgbot.Bot, chatID int64, spec forumMetaSpec, idx map[string]int64) (threadID int64, err error) {
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

// CreateStandardForumMetaTopics ensures General, BotMeta, Calls, and Status forum topics exist.
// Existing topics with the same title (within the scanned thread id range) are reused; a short notice is posted in each reused topic.
func CreateStandardForumMetaTopics(bot *gotgbot.Bot, chatID int64) (general, botMeta, calls, status int64, err error) {
	idx, err := buildForumMetaTitleIndex(bot, chatID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var ids [4]int64
	for i := range standardForumMetaSpecs {
		tid, e := ensureOneForumMetaTopic(bot, chatID, standardForumMetaSpecs[i], idx)
		if e != nil {
			if i == 0 {
				return 0, 0, 0, 0, e
			}
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

// EnsureForumMetaTopicsProvisioned requires a forum target group with Manage topics, then ensures all four
// meta topic IDs exist (creates every topic in one batch if any id is missing) and persists config.
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
