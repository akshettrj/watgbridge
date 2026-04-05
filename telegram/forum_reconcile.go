package telegram

import (
	"fmt"
	"html"
	"strings"

	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type forumMappingRemoved struct {
	WaKey    string
	ThreadID int64
}

// NotifyForumTopicMappingRemoved posts to the bridge forum General hub when a stale mapping was dropped on-demand.
func NotifyForumTopicMappingRemoved(bot *gotgbot.Bot, cfg *state.Config, waKey string, threadID int64) {
	postForumMappingRemovedAlert(bot, cfg, []forumMappingRemoved{{WaKey: waKey, ThreadID: threadID}})
}

// postForumMappingRemovedAlert sends a visible notice to the bridge forum General hub (or whole chat if legacy).
func postForumMappingRemovedAlert(bot *gotgbot.Bot, cfg *state.Config, entries []forumMappingRemoved) {
	if bot == nil || len(entries) == 0 || cfg.Telegram.TargetChatID == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("⚠️ <b>Bridge: Telegram forum topic missing</b> — WhatsApp↔Telegram mapping cleared.\n\n")
	sb.WriteString("The topic was deleted or the stored thread id is invalid. Recreate with <code>/check</code> or by messaging from WhatsApp.\n\n")
	for _, e := range entries {
		label := html.EscapeString(e.WaKey)
		if j, ok := utils.WaParseJID(e.WaKey); ok {
			label = html.EscapeString(utils.WaGetForumTopicName(j.ToNonAD()))
		}
		sb.WriteString("• <b>")
		sb.WriteString(label)
		sb.WriteString("</b>")
		if e.ThreadID != 0 {
			sb.WriteString(fmt.Sprintf(" <code>thread %d</code>", e.ThreadID))
		}
		sb.WriteByte('\n')
	}
	opts := gotgbot.SendMessageOpts{ParseMode: "HTML"}
	if cfg.Telegram.GeneralThreadID != 0 {
		opts.MessageThreadId = cfg.Telegram.GeneralThreadID
	}
	_, _ = bot.SendMessage(cfg.Telegram.TargetChatID, sb.String(), &opts)
}
