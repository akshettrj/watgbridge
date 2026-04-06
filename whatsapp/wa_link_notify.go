package whatsapp

import (
	"bytes"
	"errors"
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

// CallbackDataReconnect is Telegram inline button for starting a new QR session (≤64 bytes).
const CallbackDataReconnect = "watg_qr_rec"

// reconnectCooldownTiers: after each successful Reconnect, the next allowed attempt is after this wait.
// After the last tier, index wraps to 0 (5 min again).
var reconnectCooldownTiers = []time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

var (
	qrLoginMu sync.Mutex
	// Active QR photo message (rotating codes).
	qrLoginMessageID int64
	qrLastPNG        []byte
	// Session-closed / logged-out notice with Reconnect button.
	qrSessionClosedMessageID int64

	reconnectMu            sync.Mutex
	reconnectNextAllowedAt time.Time
	reconnectTierIndex     int
)

func qrReconnectKeyboard() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
			{{Text: "Reconnect", CallbackData: CallbackDataReconnect}},
		},
	}
}

func qrLoginCaptionHTML() string {
	loc := time.UTC
	if state.State.LocalLocation != nil {
		loc = state.State.LocalLocation
	}
	updated := html.EscapeString(time.Now().In(loc).Format("Mon 2 Jan 15:04:05"))
	return "Scan this code in WhatsApp → Settings → Linked devices, on the phone you want to use for this group.\n\n" +
		"Last updated: " + updated
}

// resetWhatsAppQRLoginMessageState clears QR message tracking for a new pairing attempt.
func resetWhatsAppQRLoginMessageState() {
	qrLoginMu.Lock()
	defer qrLoginMu.Unlock()
	qrLoginMessageID = 0
	qrLastPNG = nil
}

func resetReconnectRateLimit() {
	reconnectMu.Lock()
	defer reconnectMu.Unlock()
	reconnectNextAllowedAt = time.Time{}
	reconnectTierIndex = 0
}

// ReconnectAllowed returns whether Reconnect is allowed now, and the next allowed time if not.
func ReconnectAllowed() (ok bool, nextAt time.Time) {
	reconnectMu.Lock()
	defer reconnectMu.Unlock()
	if reconnectNextAllowedAt.IsZero() || !time.Now().Before(reconnectNextAllowedAt) {
		return true, time.Time{}
	}
	return false, reconnectNextAllowedAt
}

func applyReconnectCooldown() {
	reconnectMu.Lock()
	defer reconnectMu.Unlock()
	reconnectNextAllowedAt = time.Now().Add(reconnectCooldownTiers[reconnectTierIndex])
	reconnectTierIndex = (reconnectTierIndex + 1) % len(reconnectCooldownTiers)
}

func formatNextReconnectTime(t time.Time) string {
	loc := time.UTC
	if state.State.LocalLocation != nil {
		loc = state.State.LocalLocation
	}
	return t.In(loc).Format("Mon 2 Jan 2006 15:04:05 MST")
}

func deleteSessionClosedMessageIfAny() {
	bot := state.State.TelegramBot
	if bot == nil {
		return
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return
	}
	qrLoginMu.Lock()
	mid := qrSessionClosedMessageID
	qrSessionClosedMessageID = 0
	qrLoginMu.Unlock()
	if mid == 0 {
		return
	}
	if _, err := bot.DeleteMessage(t.TargetChatID, mid, nil); err != nil && state.State.Logger != nil {
		state.State.Logger.Debug("whatsapp qr: delete session-closed notice", zap.Error(err))
	}
}

// deleteWhatsAppQRLoginMessage removes the QR photo after successful link and resets reconnect limits.
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
	qrLastPNG = nil
	qrLoginMu.Unlock()
	if mid != 0 {
		if _, err := bot.DeleteMessage(t.TargetChatID, mid, nil); err != nil && state.State.Logger != nil {
			state.State.Logger.Debug("whatsapp qr login: delete qr message after link", zap.Error(err))
		}
	}
	deleteSessionClosedMessageIfAny()
	resetReconnectRateLimit()
}

// sendWhatsAppQRPhotoToTelegram deletes the previous QR photo (if any) and sends a new one (auto-updates on each WA code).
func sendWhatsAppQRPhotoToTelegram(qrPNG []byte) error {
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

	buf := make([]byte, len(qrPNG))
	copy(buf, qrPNG)
	qrLastPNG = buf

	deleteSessionClosedMessageLocked(bot, t.TargetChatID)

	if qrLoginMessageID != 0 {
		if _, err := bot.DeleteMessage(t.TargetChatID, qrLoginMessageID, nil); err != nil && state.State.Logger != nil {
			state.State.Logger.Debug("whatsapp qr: delete previous qr photo", zap.Error(err))
		}
		qrLoginMessageID = 0
	}

	caption := qrLoginCaptionHTML()
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

func deleteSessionClosedMessageLocked(bot *gotgbot.Bot, chatID int64) {
	if qrSessionClosedMessageID == 0 {
		return
	}
	mid := qrSessionClosedMessageID
	qrSessionClosedMessageID = 0
	if _, err := bot.DeleteMessage(chatID, mid, nil); err != nil && state.State.Logger != nil {
		state.State.Logger.Debug("whatsapp qr: delete session-closed notice before new qr", zap.Error(err))
	}
}

// OnWhatsAppQRCodeReceived updates the buffered QR and posts/updates Telegram (delete old + new photo).
func OnWhatsAppQRCodeReceived(qrPNG []byte) {
	if err := sendWhatsAppQRPhotoToTelegram(qrPNG); err != nil && state.State.Logger != nil {
		state.State.Logger.Warn("whatsapp qr photo send failed", zap.Error(err))
	}
}

// OnWhatsAppQRSessionClosed shows that the pairing session ended and offers Reconnect.
func OnWhatsAppQRSessionClosed(logger *zap.Logger) {
	bot := state.State.TelegramBot
	if bot == nil {
		return
	}
	cfg := state.State.Config.Telegram
	if cfg.TargetChatID == 0 {
		return
	}

	qrLoginMu.Lock()
	if qrLoginMessageID != 0 {
		mid := qrLoginMessageID
		qrLoginMessageID = 0
		qrLastPNG = nil
		qrLoginMu.Unlock()
		if _, err := bot.DeleteMessage(cfg.TargetChatID, mid, nil); err != nil && logger != nil {
			logger.Debug("whatsapp qr: delete qr on session closed", zap.Error(err))
		}
	} else {
		qrLoginMu.Unlock()
	}

	body := "<b>WhatsApp pairing session ended</b>\n\n" +
		"The QR session closed before your device linked (timeout or disconnect). " +
		"Tap <b>Reconnect</b> to start a new QR login when you are ready."
	postSessionNoticeWithReconnect(bot, body, logger)
}

// ShowWhatsAppSessionDisabledReconnect posts the same Reconnect affordance after logout / disabled session.
func ShowWhatsAppSessionDisabledReconnect(reason string, logger *zap.Logger) {
	bot := state.State.TelegramBot
	if bot == nil {
		return
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return
	}
	reason = strings.TrimSpace(reason)
	body := "<b>WhatsApp session disconnected</b>\n\n"
	if reason != "" {
		body += html.EscapeString(reason) + "\n\n"
	}
	body += "Tap <b>Reconnect</b> to sign in again with a new QR code."
	postSessionNoticeWithReconnect(bot, body, logger)
}

func postSessionNoticeWithReconnect(bot *gotgbot.Bot, bodyHTML string, logger *zap.Logger) {
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 {
		return
	}
	qrLoginMu.Lock()
	if qrSessionClosedMessageID != 0 {
		mid := qrSessionClosedMessageID
		qrSessionClosedMessageID = 0
		qrLoginMu.Unlock()
		if _, err := bot.DeleteMessage(t.TargetChatID, mid, nil); err != nil && logger != nil {
			logger.Debug("whatsapp qr: replace old session notice", zap.Error(err))
		}
	} else {
		qrLoginMu.Unlock()
	}

	kb := qrReconnectKeyboard()
	opts := gotgbot.SendMessageOpts{
		ParseMode:   gotgbot.ParseModeHTML,
		ReplyMarkup: &kb,
	}
	if t.GeneralThreadID != 0 {
		opts.MessageThreadId = t.GeneralThreadID
	}
	msg, err := bot.SendMessage(t.TargetChatID, bodyHTML, &opts)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp session notice send failed", zap.Error(err))
		}
		return
	}
	if msg != nil {
		qrLoginMu.Lock()
		qrSessionClosedMessageID = msg.MessageId
		qrLoginMu.Unlock()
	}
}

// HandleReconnectCallback starts a new QR session if rate limit allows.
func HandleReconnectCallback(b *gotgbot.Bot, cq *gotgbot.CallbackQuery, chatID int64) error {
	if cq == nil {
		return nil
	}
	cfg := state.State.Config.Telegram
	if chatID != cfg.TargetChatID {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Wrong chat.", ShowAlert: true})
		return nil
	}

	ok, nextAt := ReconnectAllowed()
	if !ok {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Next reconnect: " + formatNextReconnectTime(nextAt),
			ShowAlert: true,
		})
		return nil
	}

	if state.State.WhatsAppClient == nil {
		_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "WhatsApp client not ready.", ShowAlert: true})
		return nil
	}

	_, _ = cq.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: "Starting new QR session…"})

	zl := state.State.Logger
	go func() {
		if err := StartWhatsAppQRReconnect(zl); err != nil && zl != nil {
			if errors.Is(err, ErrReconnectInProgress) {
				return
			}
			zl.Warn("whatsapp reconnect failed", zap.Error(err))
			_ = sendWhatsAppQRTextToTelegram("Reconnect failed: " + err.Error())
		}
	}()
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
