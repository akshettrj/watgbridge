package telegram

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.uber.org/zap"
)

const (
	forumMetaProbeMaxAttempts = 6
	forumMetaProbeRetryBase   = 250 * time.Millisecond
)

var forumMetaSidebarPrefixTokens = map[string]struct{}{
	"topic":  {},
	"topics": {},
	"thread": {},
	"forum":  {},
	"chat":   {},
}

func forumMetaTokenAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// forumMetaNormalizeTopicName removes emoji/punctuation noise and known sidebar-like prefixes so
// old/plain titles and emoji titles resolve to the same logical name.
func forumMetaNormalizeTopicName(raw string) string {
	s := utils.TruncateTelegramForumTopicName(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// Normalize apostrophe variants to avoid Unicode punctuation mismatches.
	s = strings.ReplaceAll(s, "’", "'")
	s = strings.ReplaceAll(s, "‘", "'")

	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	tokens := strings.Fields(b.String())
	for len(tokens) > 1 {
		t := tokens[0]
		if _, ok := forumMetaSidebarPrefixTokens[t]; ok || forumMetaTokenAllDigits(t) {
			tokens = tokens[1:]
			continue
		}
		break
	}
	return strings.Join(tokens, " ")
}

// forumMetaTopicNameMatches compares Telegram topic titles with meta-slot aliases after
// normalization (emoji/plain/sidebar-prefix tolerant).
func forumMetaTopicNameMatches(spec forumMetaSpec, remoteName string) bool {
	got := forumMetaNormalizeTopicName(remoteName)
	if got == "" {
		return false
	}
	if got == forumMetaNormalizeTopicName(spec.title) {
		return true
	}
	for _, alias := range spec.aliases {
		if got == forumMetaNormalizeTopicName(alias) {
			return true
		}
	}
	return false
}

func forumMetaFetchTopicName(bot *gotgbot.Bot, chatID, threadID int64) (string, bool, error) {
	if threadID == 0 {
		return "", false, nil
	}
	var lastRetryable error
	for attempt := 0; attempt < forumMetaProbeMaxAttempts; attempt++ {
		if attempt > 0 {
			shift := attempt - 1
			if shift > 5 {
				shift = 5
			}
			d := forumMetaProbeRetryBase * time.Duration(1<<shift)
			time.Sleep(d)
		}
		name, nameOk, err := utils.TgFetchForumTopicName(bot, chatID, threadID)
		if err == nil {
			if !nameOk {
				return "", false, nil
			}
			return name, true, nil
		}
		if utils.TgErrForumTopicOrThreadInvalid(err) {
			return "", false, nil
		}
		if utils.TgErrForumMetaProbeRetryable(err) {
			lastRetryable = err
			state.State.Logger.Debug("forum meta: getForumTopic retry (retryable)",
				zap.Int("attempt", attempt+1),
				zap.Int64("thread_id", threadID),
				zap.Error(err))
			continue
		}
		state.State.Logger.Debug("forum meta: getForumTopic failed; treating topic as missing",
			zap.Int64("thread_id", threadID),
			zap.Error(err))
		return "", false, nil
	}
	if lastRetryable != nil {
		return "", false, fmt.Errorf("getForumTopic after %d attempts: %w", forumMetaProbeMaxAttempts, lastRetryable)
	}
	return "", false, fmt.Errorf("getForumTopic after %d attempts", forumMetaProbeMaxAttempts)
}

// forumMetaTopicMatchesSpec returns true only if getForumTopic succeeds and the topic name matches
// the expected meta slot title. SendMessage cannot distinguish topics; this is the source of truth.
func forumMetaTopicMatchesSpec(bot *gotgbot.Bot, chatID, threadID int64, spec forumMetaSpec) (bool, error) {
	name, ok, err := forumMetaFetchTopicName(bot, chatID, threadID)
	if err != nil || !ok {
		return false, err
	}
	return forumMetaTopicNameMatches(spec, name), nil
}
