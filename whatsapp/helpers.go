package whatsapp

import (
	"fmt"
	"html"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
)

// bridgeContext holds the common state needed when bridging a single
// WhatsApp message to Telegram. It is created once per incoming message
// and passed to the individual media-type handlers.
type bridgeContext struct {
	cfg          *state.Config
	logger       *zap.Logger
	tgBot        *gotgbot.Bot
	waClient     *whatsmeow.Client
	bridgedText  string
	replyToMsgId int64
	threadId     int64
	msgId        string
	senderStr    string
	chatStr      string
	replyMarkup  gotgbot.InlineKeyboardMarkup
}

// savePair persists the WA↔TG message-ID mapping if the Telegram message
// was sent successfully.
func (bc *bridgeContext) savePair(sentMsg *gotgbot.Message) {
	if sentMsg != nil && sentMsg.MessageId != 0 {
		database.MsgIdAddNewPair(
			bc.msgId, bc.senderStr, bc.chatStr,
			bc.cfg.Telegram.TargetChatID,
			sentMsg.MessageId, sentMsg.MessageThreadId,
		)
	}
}

// sendFallbackText sends a text-only message (header + extra info) to
// Telegram and saves the pair. Used when media cannot be sent.
func (bc *bridgeContext) sendFallbackText(extraText string) {
	sentMsg, _ := bc.tgBot.SendMessage(
		bc.cfg.Telegram.TargetChatID,
		bc.bridgedText+extraText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		},
	)
	bc.savePair(sentMsg)
}

// addCaption appends a caption (truncated at 1020 chars if needed) to
// the bridged text.
func addCaption(bridgedText *string, caption string) {
	if caption == "" {
		return
	}
	if len(caption) > 1020 {
		*bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
	} else {
		*bridgedText += html.EscapeString(caption)
	}
}

// getContextInfo extracts the ContextInfo from any WhatsApp message type.
// Protobuf nil-safe getters make chained calls safe even when the
// sub-message is nil.
func getContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if ci := msg.GetExtendedTextMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetImageMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetVideoMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPtvMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetAudioMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetDocumentMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetStickerMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetContactMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetContactsArrayMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetLocationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetLiveLocationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessageV2().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetPollCreationMessageV3().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetButtonsMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetTemplateMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetInteractiveMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := msg.GetListMessage().GetContextInfo(); ci != nil {
		return ci
	}
	return nil
}

// resolveThreadId determines the correct Telegram thread (topic) for a
// WhatsApp message based on chat type: status, broadcast, group, or private.
func resolveThreadId(info waTypes.MessageInfo, cfg *state.Config, tgBot *gotgbot.Bot) (int64, error) {
	chatStr := info.Chat.String()

	if chatStr == "status@broadcast" {
		return utils.TgGetOrMakeThreadFromWa_String(
			"status@broadcast", cfg.Telegram.TargetChatID, "Status",
		)
	}

	if info.IsIncomingBroadcast() {
		if info.MessageSource.AddressingMode == waTypes.AddressingModePN || info.MessageSource.SenderAlt.IsEmpty() {
			jid := info.MessageSource.Sender.ToNonAD()
			return utils.TgGetOrMakeThreadFromWa(jid, cfg.Telegram.TargetChatID, utils.WaGetContactName(jid))
		}
		jid := info.MessageSource.SenderAlt.ToNonAD()
		return utils.TgGetOrMakeThreadFromWa(jid, cfg.Telegram.TargetChatID, utils.WaGetContactName(jid))
	}

	if info.IsGroup {
		return utils.TgGetOrMakeThreadFromWa(
			info.Chat, cfg.Telegram.TargetChatID, utils.WaGetGroupName(info.Chat),
		)
	}

	targetJID := info.Chat.ToNonAD()
	return utils.TgGetOrMakeThreadFromWa(
		targetJID, cfg.Telegram.TargetChatID, utils.WaGetContactName(targetJID),
	)
}

// buildBridgedHeader builds the sender/group/timestamp header that
// precedes every bridged message.
func buildBridgedHeader(info waTypes.MessageInfo, cfg *state.Config, isEdited bool) string {
	var text string

	if cfg.WhatsApp.SkipChatDetails {
		if info.IsIncomingBroadcast() {
			text += "👥: <b>(Broadcast)</b>\n"
		} else if info.IsFromMe {
			text += "🧑: <b>You [other device]</b>\n"
		} else if info.IsGroup {
			text += fmt.Sprintf("🧑: <b>%s</b>\n",
				html.EscapeString(utils.WaGetContactName(info.MessageSource.Sender)))
		}
	} else {
		if info.IsFromMe {
			text += "🧑: <b>You [other device]</b>\n"
		} else {
			text += fmt.Sprintf("🧑: <b>%s</b>\n",
				html.EscapeString(utils.WaGetContactName(info.MessageSource.Sender)))
		}
		if info.IsIncomingBroadcast() {
			text += "👥: <b>(Broadcast)</b>\n"
		} else if info.IsGroup {
			text += fmt.Sprintf("👥: <b>%s</b>\n",
				html.EscapeString(utils.WaGetGroupName(info.Chat)))
		} else {
			text += "👥: <b>(PVT)</b>\n"
		}
	}

	if isEdited {
		text += "<i>Edited</i>\n"
	}

	if time.Since(info.Timestamp).Seconds() > 60 {
		text += fmt.Sprintf("🕛: <b>%s</b>\n",
			html.EscapeString(info.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))
	}

	return text
}
