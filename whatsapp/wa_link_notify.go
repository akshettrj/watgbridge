package whatsapp

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"net/http"
	"strings"
	"time"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
)

func sendWhatsAppQRToTelegram(qrPNG []byte, caption string) error {
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 || t.GeneralThreadID == 0 {
		return fmt.Errorf("telegram.target_chat_id and telegram.general_thread_id must be set (forum General topic is required)")
	}
	opts := gotgbot.SendPhotoOpts{
		Caption:         caption,
		MessageThreadId: t.GeneralThreadID,
	}
	_, err := bot.SendPhoto(t.TargetChatID, gotgbot.InputFileByReader("qrcode.png", bytes.NewReader(qrPNG)), &opts)
	return err
}

func sendWhatsAppQRTextToTelegram(text string) error {
	bot := state.State.TelegramBot
	if bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}
	t := state.State.Config.Telegram
	if t.TargetChatID == 0 || t.GeneralThreadID == 0 {
		return fmt.Errorf("telegram.target_chat_id and telegram.general_thread_id must be set (forum General topic is required)")
	}
	opts := gotgbot.SendMessageOpts{
		MessageThreadId: t.GeneralThreadID,
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
	cfg := state.State.Config
	bot := state.State.TelegramBot
	if bot == nil {
		zl.Warn("wa linked notify: bridge telegram bot nil")
		return
	}
	t := cfg.Telegram

	jid := cli.Store.GetJID()
	phone := waPhoneDisplay(jid)

	_, err := bot.SendMessage(t.TargetChatID, "Success linking your WA phone number to this group", &gotgbot.SendMessageOpts{
		MessageThreadId: t.GeneralThreadID,
	})
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
	if control != nil && t.TargetChatID != 0 && t.GeneralThreadID != 0 {
		farewell := fmt.Sprintf(
			"WhatsApp is linked on <b>%s</b>. I’m leaving this group — your bridge runs here with %s. "+
				"For settings and other bridges, open a private chat with me and send <code>/bridge_list</code>.",
			html.EscapeString(phone),
			botNameHTML,
		)
		_, fareErr := control.SendMessage(t.TargetChatID, farewell, &gotgbot.SendMessageOpts{
			MessageThreadId: t.GeneralThreadID,
			ParseMode:       gotgbot.ParseModeHTML,
		})
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
