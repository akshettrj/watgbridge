package whatsapp

import (
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// tgSendBridgeTargetMessage wraps SendMessage to the bridge target supergroup and triggers forum meta
// reprovision when a send to a meta topic thread fails with a stale thread id.
func tgSendBridgeTargetMessage(tgBot *gotgbot.Bot, targetChatID int64, text string, opts *gotgbot.SendMessageOpts) (*gotgbot.Message, error) {
	msg, err := tgBot.SendMessage(targetChatID, text, opts)
	if err != nil && opts != nil && opts.MessageThreadId != 0 {
		utils.TgNotifyForumMetaSendFailure(targetChatID, opts.MessageThreadId, err)
	}
	return msg, err
}
