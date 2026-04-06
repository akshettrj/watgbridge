package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
)

var (
	qrLoginMu        sync.Mutex
	qrLoginMessageID int64
)

// defaultForumGeneralMessageThreadID is the usual message_thread_id for the default "General" topic when
// telegram.general_thread_id is 0 (Bot API omits it on send). gotgbot's EditMessageMedia does not pass
// message_thread_id, which breaks in-place media edits in forum supergroups unless we supply it here.
const defaultForumGeneralMessageThreadID int64 = 1

func effectiveForumThreadIDForQRMedia(generalThreadID int64) int64 {
	if generalThreadID != 0 {
		return generalThreadID
	}
	return defaultForumGeneralMessageThreadID
}

// editMessageMediaWithForumThread calls editMessageMedia with message_thread_id. The generated gotgbot
// EditMessageMediaOpts omits this field, so forum topic photo updates would otherwise fail.
func editMessageMediaWithForumThread(bot *gotgbot.Bot, chatID, messageID, messageThreadID int64, media gotgbot.InputMedia) (*gotgbot.Message, bool, error) {
	v := map[string]any{
		"media":      media,
		"chat_id":    chatID,
		"message_id": messageID,
	}
	if messageThreadID != 0 {
		v["message_thread_id"] = messageThreadID
	}
	r, err := bot.RequestWithContext(context.Background(), "editMessageMedia", v, nil)
	if err != nil {
		return nil, false, err
	}
	var m gotgbot.Message
	if err := json.Unmarshal(r, &m); err != nil {
		var b bool
		if err := json.Unmarshal(r, &b); err != nil {
			return nil, false, err
		}
		return nil, b, nil
	}
	return &m, true, nil
}

// resetWhatsAppQRLoginMessageState clears the in-memory Telegram message id for the current QR session.
// Call when starting a new QR login flow (new process or new unlinked device) so the first code sends a fresh photo.
func resetWhatsAppQRLoginMessageState() {
	qrLoginMu.Lock()
	defer qrLoginMu.Unlock()
	qrLoginMessageID = 0
}

// deleteWhatsAppQRLoginMessage removes the single QR photo message after successful link (best-effort).
func deleteWhatsAppQRLoginMessage() {
	bot := state.State.TelegramBot
	if bot == nil {
		return
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return
	}
	qrLoginMu.Lock()
	mid := qrLoginMessageID
	qrLoginMessageID = 0
	qrLoginMu.Unlock()
	if mid == 0 {
		return
	}
	if _, err := bot.DeleteMessage(t.TargetChatID, mid, nil); err != nil && state.State.Logger != nil {
		state.State.Logger.Debug("whatsapp qr login: delete qr message after link", zap.Error(err))
	}
}

// sendOrUpdateWhatsAppQRToTelegram sends one photo on the first code event, then edits that message as WhatsApp rotates the QR.
// This avoids flooding the forum with dozens of images (WhatsApp refreshes the code periodically).
func sendOrUpdateWhatsAppQRToTelegram(qrPNG []byte, caption string) error {
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return fmt.Errorf("telegram.target_chat_id must be set")
	}

	qrLoginMu.Lock()
	defer qrLoginMu.Unlock()

	media := gotgbot.InputMediaPhoto{
		Media:   gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrPNG)),
		Caption: caption,
	}

	if qrLoginMessageID != 0 {
		media.ParseMode = gotgbot.ParseModeHTML
		opts := &gotgbot.EditMessageMediaOpts{
			ChatId:    t.TargetChatID,
			MessageId: qrLoginMessageID,
		}
		_, _, err := bot.EditMessageMedia(media, opts)
		if err != nil {
			tid := effectiveForumThreadIDForQRMedia(t.GeneralThreadID)
			_, _, err2 := editMessageMediaWithForumThread(bot, t.TargetChatID, qrLoginMessageID, tid, media)
			if err2 == nil {
				return nil
			}
			if state.State.Logger != nil {
				state.State.Logger.Debug("whatsapp qr: editMessageMedia failed (plain and with forum thread id)",
					zap.Error(err),
					zap.Error(err2),
					zap.Int64("message_id", qrLoginMessageID),
					zap.Int64("message_thread_id", tid))
			}
			qrLoginMessageID = 0
		} else {
			return nil
		}
	}

	opts := gotgbot.SendPhotoOpts{
		Caption:   caption,
		ParseMode: gotgbot.ParseModeHTML,
	}
	if t.GeneralThreadID != 0 {
		opts.MessageThreadId = t.GeneralThreadID
	}
	msg, err := bot.SendPhoto(t.TargetChatID, gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrPNG)), &opts)
	if err != nil {
		return err
	}
	if msg != nil {
		qrLoginMessageID = msg.MessageId
	}
	return nil
}

func sendWhatsAppQRTextToTelegram(text string) error {
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return fmt.Errorf("telegram.target_chat_id must be set")
	}
	opts := gotgbot.SendMessageOpts{}
	if t.GeneralThreadID != 0 {
		opts.MessageThreadId = t.GeneralThreadID
	}
	_, err := bot.SendMessage(t.TargetChatID, text, &opts)
	return err
}

func waPhoneDisplay(jid types.JID) string {
	if jid.IsEmpty() {
		return "unknown"
	}
	j := jid.ToNonAD()
	if j.Server == types.DefaultUserServer && j.User != "" {
		return "+" + j.User
	}
	return j.String()
}

// notifyWhatsAppLinked runs after a fresh QR login: message in General topic + optional DM from control (main) bot.
func notifyWhatsAppLinked(cli *whatsmeow.Client, zl *zap.Logger) {
	deleteWhatsAppQRLoginMessage()

	cfg := state.State.Config
	bot := state.State.TelegramBot
	if bot == nil {
		zl.Warn("wa linked notify: bridge telegram bot nil")
		return
	}
	t := cfg.Telegram

	jid := cli.Store.GetJID()
	phone := waPhoneDisplay(jid)

	linkOpts := gotgbot.SendMessageOpts{}
	if t.GeneralThreadID != 0 {
		linkOpts.MessageThreadId = t.GeneralThreadID
	}
	_, err := bot.SendMessage(t.TargetChatID, "Success linking your WA phone number to this group", &linkOpts)
	if err != nil {
		zl.Warn("wa linked: general topic message failed", zap.Error(err))
	}

	botNameHTML := html.EscapeString(strings.TrimSpace(bot.FirstName))
	if u := strings.TrimSpace(bot.Username); u != "" {
		botNameHTML = fmt.Sprintf(`<a href="https://t.me/%s">@%s</a>`, html.EscapeString(u), html.EscapeString(u))
	}

	token := strings.TrimSpace(t.ControlBotToken)
	var control *gotgbot.Bot
	if token != "" {
		var cErr error
		control, cErr = gotgbot.NewBot(token, &gotgbot.BotOpts{
			BotClient: &gotgbot.BaseBotClient{
				Client: http.Client{},
				DefaultRequestOpts: &gotgbot.RequestOpts{
					APIURL:  t.APIURL,
					Timeout: time.Duration(math.MaxInt64),
				},
			},
			DisableTokenCheck: true,
		})
		if cErr != nil {
			zl.Warn("wa linked: control bot init failed", zap.Error(cErr))
		}
	}

	// Multi-mode chat-picker adds the main (control) bot to the forum so bot_administrator_rights can apply; leave after onboarding.
	if control != nil && t.TargetChatID != 0 {
		farewell := fmt.Sprintf(
			"WhatsApp is linked on <b>%s</b>. I’m leaving this group — your bridge runs here with %s. "+
				"For settings and other bridges, open a private chat with me and send <code>/bridge_list</code>.",
			html.EscapeString(phone),
			botNameHTML,
		)
		fareOpts := gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML}
		if t.GeneralThreadID != 0 {
			fareOpts.MessageThreadId = t.GeneralThreadID
		}
		_, fareErr := control.SendMessage(t.TargetChatID, farewell, &fareOpts)
		if fareErr != nil {
			zl.Debug("wa linked: control bot farewell in group (ok if main bot was not in group)", zap.Error(fareErr))
		}
		if _, leaveErr := control.LeaveChat(t.TargetChatID, nil); leaveErr != nil {
			zl.Debug("wa linked: control bot leave group", zap.Error(leaveErr))
		}
	}

	if control == nil || t.OwnerID == 0 {
		return
	}

	chat, err := bot.GetChat(t.TargetChatID, nil)
	groupTitle := "your group"
	if err == nil && chat != nil && strings.TrimSpace(chat.Title) != "" {
		groupTitle = chat.Title
	} else if err != nil {
		zl.Debug("wa linked: getChat for title failed", zap.Error(err))
	}

	body := fmt.Sprintf(
		"Success linking %s in group <b>%s</b> to your WA on phone number <b>%s</b>. Use /bridge_list to list your mappings.",
		botNameHTML,
		html.EscapeString(groupTitle),
		html.EscapeString(phone),
	)
	if t.BridgeRegistryID != 0 {
		body += fmt.Sprintf(" (bridge id <code>%d</code>)", t.BridgeRegistryID)
	}

	_, err = control.SendMessage(t.OwnerID, body, &gotgbot.SendMessageOpts{
		ParseMode: gotgbot.ParseModeHTML,
	})
	if err != nil {
		zl.Warn("wa linked: control bot notify failed", zap.Error(err))
	}
}
