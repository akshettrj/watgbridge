package utils

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	goVCard "github.com/emersion/go-vcard"
	"github.com/forPelevin/gomoji"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

const (
	DownloadSizeLimit int64  = 20971520
	UploadSizeLimit   uint64 = 52428800
)

// CheckContactPingMessage is posted in the contact forum topic when the user confirms "Send ping" from /check.
const CheckContactPingMessage = "\n⚙️ Bridge bot message\n\nPing!"

// TgEffectiveMessageThreadId returns the forum topic thread id for DB routing.
// Telegram (and some clients) omit message_thread_id on the outer message when the user
// replies in a topic; walk reply_to_message until a non-zero thread id is found.
func TgEffectiveMessageThreadId(msg *gotgbot.Message) int64 {
	if msg == nil {
		return 0
	}
	for cur := msg; cur != nil; cur = cur.ReplyToMessage {
		if cur.MessageThreadId != 0 {
			return cur.MessageThreadId
		}
	}
	return 0
}

func TgRegisterBotCommands(b *gotgbot.Bot, commands ...gotgbot.BotCommand) error {
	_, err := b.SetMyCommands(commands, &gotgbot.SetMyCommandsOpts{
		LanguageCode: "en",
		Scope:        gotgbot.BotCommandScopeDefault{},
	})
	return err
}

// TgMessageIsInGeneralHub is true for commands that must run in the forum "General" hub topic
// (/list_contacts, /getwagroups, etc.). Uses state.State.ForumHubMessageThreadID from forum meta
// provisioning when set; otherwise best-effort heuristics before provision completes.
func TgMessageIsInGeneralHub(cfg *state.Config, msg *gotgbot.Message) bool {
	if msg == nil {
		return false
	}
	tid := TgEffectiveMessageThreadId(msg)
	if hub := state.State.ForumHubMessageThreadID; hub != 0 {
		return tid == hub
	}
	if !msg.IsTopicMessage {
		return true
	}
	return tid == 0 || tid == 1
}

// TgMessageIsInContactTopic is true inside a per-contact (or per-chat) WA-linked topic, not the hub.
func TgMessageIsInContactTopic(cfg *state.Config, msg *gotgbot.Message) bool {
	if msg == nil {
		return false
	}
	if TgEffectiveMessageThreadId(msg) == 0 {
		return false
	}
	if TgMessageIsInGeneralHub(cfg, msg) {
		return false
	}
	return true
}

func TgGetOrMakeThreadFromWa_String(waChatIdString string, tgChatId int64, threadName string) (int64, bool, error) {
	threadId, threadFound, err := database.ChatThreadGetTgFromWa(waChatIdString, tgChatId)
	if err != nil {
		return 0, false, err
	}
	// Corrupt mapping (thread id 0): Telegram never created a real topic; repair by re-creating.
	if threadFound && threadId == 0 {
		state.State.Logger.Warn("dropping chat_thread row with tg_thread_id=0; will recreate forum topic",
			zap.String("wa_chat", waChatIdString),
			zap.Int64("tg_chat", tgChatId))
		if err := database.ChatThreadDropPairByWaChat(waChatIdString, tgChatId); err != nil {
			return 0, false, err
		}
		threadFound = false
	}

	if !threadFound {
		tgBot := state.State.TelegramBot
		newForum, err := tgBot.CreateForumTopic(tgChatId, TruncateTelegramForumTopicName(threadName), &gotgbot.CreateForumTopicOpts{})
		if err != nil {
			return 0, false, err
		}
		err = database.ChatThreadAddNewPair(waChatIdString, tgChatId, newForum.MessageThreadId)
		if err != nil {
			return newForum.MessageThreadId, true, err
		}
		if TopicMetadataIsWAChatKey(waChatIdString) {
			_ = database.ChatThreadTopicMetadataSetTopicCreatedAt(waChatIdString, tgChatId, time.Now())
			_ = database.ChatThreadForumSyncedTitleSet(waChatIdString, tgChatId, TruncateTelegramForumTopicName(threadName))
		}
		return newForum.MessageThreadId, true, nil
	}

	return threadId, false, nil
}

const maxForumTopicNameLen = 128

// TgPinChatMessageInThread pins a chat message. For forum supergroups, message_thread_id must
// match the topic the message belongs to; omitting it (as gotgbot.PinChatMessage does) pins in General.
func TgPinChatMessageInThread(b *gotgbot.Bot, chatId, messageId, messageThreadId int64, disableNotification bool) error {
	if b == nil {
		return fmt.Errorf("nil bot")
	}
	params := map[string]any{
		"chat_id":    chatId,
		"message_id": messageId,
	}
	if messageThreadId != 0 {
		params["message_thread_id"] = messageThreadId
	}
	if disableNotification {
		params["disable_notification"] = true
	}
	_, err := b.RequestWithContext(context.Background(), "pinChatMessage", params, nil)
	return err
}

// TgAbsChatIDForTMe converts a Bot API supergroup id (e.g. -100…) to the numeric segment used in t.me/c/… links.
func TgAbsChatIDForTMe(chatID int64) int64 {
	x := chatID
	if x < 0 {
		x = -x
	}
	if x >= 1_000_000_000_000 {
		x -= 1_000_000_000_000
	}
	return x
}

// TgForumTopicOpenLink builds a t.me URL that opens a forum topic by linking a message inside that topic.
// Per https://core.telegram.org/api/links private message links use t.me/c/<channel>/<message_id>?thread=<thread_id>
// When bot is non-nil, getForumTopic is used first: if the thread no longer exists, the DB mapping is dropped and
// droppedStale is true. (Bots cannot "open" t.me URLs as the user; this is the correct existence check.)
func TgForumTopicOpenLink(bot *gotgbot.Bot, tgChatId, messageThreadId int64, waKey string, waJID waTypes.JID) (url string, droppedStale bool) {
	if tgChatId == 0 || messageThreadId == 0 {
		return "", false
	}
	if bot != nil {
		_, nameOk, ftErr := TgFetchForumTopicName(bot, tgChatId, messageThreadId)
		if ftErr != nil && TgErrForumTopicOrThreadInvalid(ftErr) {
			_ = database.ChatThreadDropPairByTg(tgChatId, messageThreadId)
			return "", true
		}
		if ftErr == nil && !nameOk {
			_ = database.ChatThreadDropPairByTg(tgChatId, messageThreadId)
			return "", true
		}
	}
	var msgId int64
	if pair, ok := database.ChatThreadGetPair(waKey, tgChatId); ok {
		msgId = pair.MetadataTgMsgId
	}
	if msgId == 0 && TopicMetadataIsWAChatKey(waKey) {
		TgTopicMetadataEnsurePostedForChat(tgChatId, messageThreadId, waKey, waJID.ToNonAD())
		if pair, ok := database.ChatThreadGetPair(waKey, tgChatId); ok {
			msgId = pair.MetadataTgMsgId
		}
	}
	if msgId == 0 {
		return "", false
	}
	return fmt.Sprintf("https://t.me/c/%d/%d?thread=%d", TgAbsChatIDForTMe(tgChatId), msgId, messageThreadId), false
}

// TgErrForumTopicOrThreadInvalid is true when Telegram reports that a forum thread/topic id is invalid or missing
// (topic deleted, stale DB mapping). Covers sendMessage and getForumTopic style errors.
func TgErrForumTopicOrThreadInvalid(err error) bool {
	var te *gotgbot.TelegramError
	if !errors.As(err, &te) {
		return false
	}
	d := strings.ToUpper(te.Description)
	// getForumTopic for a non-existent message_thread_id often returns 400 with description "Not Found" only
	// (no TOPIC/THREAD substring), e.g. probing empty thread slots in forum_meta title scan.
	if te.Method == "getForumTopic" && strings.TrimSpace(d) == "NOT FOUND" {
		return true
	}
	return strings.Contains(d, "MESSAGE THREAD NOT FOUND") ||
		strings.Contains(d, "THREAD NOT FOUND") ||
		strings.Contains(d, "TOPIC_ID_INVALID") ||
		strings.Contains(d, "MESSAGE_THREAD_ID_INVALID") ||
		strings.Contains(d, "TOPIC NOT FOUND") ||
		(strings.Contains(d, "NOT FOUND") && (strings.Contains(d, "TOPIC") || strings.Contains(d, "THREAD")))
}

// TgErrForumMetaProbeRetryable is true for transient failures where retrying SendMessage may succeed
// (rate limits, Telegram 5xx, timeouts). If false, forum meta probe may treat the error as a dead topic.
func TgErrForumMetaProbeRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var te *gotgbot.TelegramError
	if errors.As(err, &te) {
		if te.Code == 429 {
			return true
		}
		if te.Code >= 500 {
			return true
		}
		d := strings.ToUpper(te.Description)
		if strings.Contains(d, "TOO MANY REQUESTS") || strings.Contains(d, "RETRY AFTER") {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "eof") {
		return true
	}
	return false
}

// TgErrMessageThreadMissing is true for sendMessage failures when the target forum thread no longer exists.
func TgErrMessageThreadMissing(err error) bool {
	return TgErrForumTopicOrThreadInvalid(err)
}

// TruncateTelegramForumTopicName shortens titles to Telegram's forum topic limit.
func TruncateTelegramForumTopicName(title string) string {
	if len(title) <= maxForumTopicNameLen {
		return title
	}
	return title[:maxForumTopicNameLen-3] + "..."
}

func tgTopicMetadataCardHTML(waDisplay string, tgTopicCreated, waDialog sql.NullTime, isGroup bool, timeFormat string, loc *time.Location) string {
	waLine := html.EscapeString(waDisplay)
	if waLine == "" {
		waLine = "—"
	}
	tgLine := "—"
	if tgTopicCreated.Valid {
		tgLine = html.EscapeString(tgTopicCreated.Time.In(loc).Format(timeFormat))
	}
	var waDlgLine string
	if waDialog.Valid {
		waDlgLine = html.EscapeString(waDialog.Time.In(loc).Format(timeFormat))
	} else if isGroup {
		waDlgLine = "—"
	} else {
		waDlgLine = "<i>Not available for 1:1 chats</i>"
	}
	return fmt.Sprintf(
		"<b>Topic metadata</b>\n• <b>WhatsApp name</b>: %s\n• <b>Telegram topic created</b>: %s\n• <b>WhatsApp chat created</b>: %s",
		waLine, tgLine, waDlgLine,
	)
}

// TgTopicMetadataEnsurePostedForChat sends and pins the metadata card when missing (1:1 and groups).
func TgTopicMetadataEnsurePostedForChat(tgChatId, tgThreadId int64, waKey string, waJID waTypes.JID) {
	if !TopicMetadataIsWAChatKey(waKey) {
		return
	}
	b := state.State.TelegramBot
	if b == nil {
		return
	}
	cfg := state.State.Config
	logger := state.State.Logger
	ctx := context.Background()

	pair, ok := database.ChatThreadGetPair(waKey, tgChatId)
	if !ok || pair.MetadataTgMsgId != 0 {
		return
	}

	waJID = waJID.ToNonAD()
	isGroup := waJID.Server == waTypes.GroupServer
	disp := WaSourceDisplayNameForMetadata(waJID)
	waDlg := WaChatDialogCreatedAt(ctx, waJID)

	tgCreated := pair.TgTopicCreatedAt
	waDlgSt := pair.WaDialogCreatedAt
	if !waDlgSt.Valid && waDlg.Valid {
		waDlgSt = waDlg
	}

	if err := database.ChatThreadTopicMetadataWrite(waKey, tgChatId, disp, tgCreated, waDlgSt, 0); err != nil {
		logger.Warn("topic metadata write", zap.Error(err))
	}

	body := tgTopicMetadataCardHTML(disp, tgCreated, waDlgSt, isGroup, cfg.TimeFormat, state.State.LocalLocation)
	sent, err := b.SendMessage(tgChatId, body, &gotgbot.SendMessageOpts{
		MessageThreadId:     tgThreadId,
		ParseMode:           "HTML",
		DisableNotification: true,
	})
	if err != nil || sent == nil {
		logger.Warn("topic metadata send", zap.Error(err))
		if err != nil {
			TgNotifyForumMetaSendFailure(tgChatId, tgThreadId, err)
		}
		return
	}
	if pinErr := TgPinChatMessageInThread(b, tgChatId, sent.MessageId, tgThreadId, true); pinErr != nil {
		logger.Warn("topic metadata pin", zap.Error(pinErr))
	}
	if err := database.ChatThreadTopicMetadataWrite(waKey, tgChatId, disp, tgCreated, waDlgSt, sent.MessageId); err != nil {
		logger.Warn("topic metadata save message id", zap.Error(err))
	}
}

// TgTopicMetadataRefreshFromWA updates the pinned card after the WhatsApp-side display name (or group creation) changes.
func TgTopicMetadataRefreshFromWA(tgChatId, tgThreadId int64, waKey string, waJID waTypes.JID) {
	if !TopicMetadataIsWAChatKey(waKey) {
		return
	}
	b := state.State.TelegramBot
	if b == nil {
		return
	}
	cfg := state.State.Config
	logger := state.State.Logger
	ctx := context.Background()

	waJID = waJID.ToNonAD()
	isGroup := waJID.Server == waTypes.GroupServer
	disp := WaSourceDisplayNameForMetadata(waJID)
	waDlg := WaChatDialogCreatedAt(ctx, waJID)

	pair, ok := database.ChatThreadGetPair(waKey, tgChatId)
	if !ok {
		return
	}
	if pair.MetadataTgMsgId == 0 {
		TgTopicMetadataEnsurePostedForChat(tgChatId, tgThreadId, waKey, waJID)
		return
	}

	tgCreated := pair.TgTopicCreatedAt
	waDlgSt := pair.WaDialogCreatedAt
	if !waDlgSt.Valid && waDlg.Valid {
		waDlgSt = waDlg
	}

	if err := database.ChatThreadTopicMetadataWrite(waKey, tgChatId, disp, tgCreated, waDlgSt, pair.MetadataTgMsgId); err != nil {
		logger.Warn("topic metadata refresh write", zap.Error(err))
	}

	body := tgTopicMetadataCardHTML(disp, tgCreated, waDlgSt, isGroup, cfg.TimeFormat, state.State.LocalLocation)
	_, _, err := b.EditMessageText(body, &gotgbot.EditMessageTextOpts{
		ChatId:    tgChatId,
		MessageId: pair.MetadataTgMsgId,
		ParseMode: "HTML",
	})
	if err != nil {
		logger.Warn("topic metadata edit", zap.Error(err))
	}
}

// TgEditForumTopicUnchanged reports whether Telegram rejected editForumTopic because the name/icon
// was already identical (Bot API: Bad Request: TOPIC_NOT_MODIFIED). That case should be treated as success.
func TgEditForumTopicUnchanged(err error) bool {
	var te *gotgbot.TelegramError
	return errors.As(err, &te) && strings.Contains(strings.ToUpper(te.Description), "TOPIC_NOT_MODIFIED")
}

// TgFetchForumTopicName returns the current forum topic name via Telegram getForumTopic (Bot API).
func TgFetchForumTopicName(b *gotgbot.Bot, chatId, messageThreadId int64) (name string, ok bool, err error) {
	r, err := b.RequestWithContext(context.Background(), "getForumTopic", map[string]any{
		"chat_id":           chatId,
		"message_thread_id": messageThreadId,
	}, nil)
	if err != nil {
		return "", false, err
	}
	var topic struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(r, &topic); err != nil {
		return "", false, err
	}
	return topic.Name, topic.Name != "", nil
}

// TgApplyForumTopicSyncFromWA is used by /synccontactname and /synctopicnames.
// If the Telegram topic title still matches the last bridge-applied WA title (or already matches latest WA),
// the forum title is updated to the latest WA canonical name when it changed.
// If the user renamed the topic in Telegram, only the pinned metadata message is refreshed.
func TgApplyForumTopicSyncFromWA(tgChatId, threadId int64, waKey string, waJID waTypes.JID) error {
	tgBot := state.State.TelegramBot
	logger := state.State.Logger
	waJID = waJID.ToNonAD()
	expected := TruncateTelegramForumTopicName(WaGetForumTopicName(waJID))

	pair, pairOk := database.ChatThreadGetPair(waKey, tgChatId)
	syncedTitle := ""
	if pairOk {
		syncedTitle = pair.TgForumTitleSyncedFromWA
	}

	current, ftOk, ftErr := TgFetchForumTopicName(tgBot, tgChatId, threadId)
	shouldEditForum := false

	switch {
	case ftErr != nil || !ftOk:
		logger.Debug("getForumTopic unavailable; updating forum title from WA",
			zap.Error(ftErr),
			zap.String("wa_key", waKey))
		shouldEditForum = true
	case current == expected:
		shouldEditForum = false
		if syncedTitle != expected {
			_ = database.ChatThreadForumSyncedTitleSet(waKey, tgChatId, expected)
		}
	default:
		// Telegram title differs from latest WA canonical.
		if syncedTitle == "" {
			// Legacy / first sync: align TG title to current WA once, then track it.
			shouldEditForum = true
		} else if current == syncedTitle {
			// Topic still shows our last bridge title; WA canonical moved.
			shouldEditForum = true
		} else {
			// User (or another admin) changed the forum topic name.
			shouldEditForum = false
		}
	}

	if shouldEditForum {
		if _, err := tgBot.EditForumTopic(tgChatId, threadId, &gotgbot.EditForumTopicOpts{Name: expected}); err != nil && !TgEditForumTopicUnchanged(err) {
			return err
		}
		_ = database.ChatThreadForumSyncedTitleSet(waKey, tgChatId, expected)
	}
	TgTopicMetadataRefreshFromWA(tgChatId, threadId, waKey, waJID)
	return nil
}

// TgSyncForumTopicTitleFromWa runs /synccontactname-style forum + metadata sync for one private topic.
func TgSyncForumTopicTitleFromWa(tgChatId, threadId int64, waJID waTypes.JID) error {
	j := waJID.ToNonAD()
	return TgApplyForumTopicSyncFromWA(tgChatId, threadId, j.String(), j)
}

// TgGetOrMakeThreadFromWa resolves LID→PN, then maps WA chat → Telegram forum thread.
// New topics are titled from WhatsApp: private chats via WaGetForumTopicName; groups as "GROUP: <name>".
// For groups, pass threadName when you already have a display name (e.g. from an event); if empty, the name is fetched via GetGroupInfo.
// The bool is true if a new forum topic was created in this call (not if it already existed).
func TgGetOrMakeThreadFromWa(waChatId waTypes.JID, tgChatId int64, threadName string) (int64, bool, error) {
	if waChatId.Server == waTypes.HiddenUserServer {
		waClient := state.State.WhatsAppClient
		pn, err := waClient.Store.LIDs.GetPNForLID(context.Background(), waChatId)
		if err != nil {
			return 0, false, err
		}
		waChatId = pn
	}
	waChatId = waChatId.ToNonAD()
	waChatIdString := waChatId.String()

	var title string
	if waChatId.Server == waTypes.GroupServer {
		if threadName == "" {
			title = WaGetForumTopicName(waChatId)
		} else {
			title = WaTelegramGroupTopicTitle(threadName)
		}
	} else {
		title = WaGetForumTopicName(waChatId)
	}

	threadId, created, err := TgGetOrMakeThreadFromWa_String(waChatIdString, tgChatId, title)
	if err != nil {
		return 0, false, err
	}
	if TopicMetadataIsWAChatKey(waChatIdString) {
		TgTopicMetadataEnsurePostedForChat(tgChatId, threadId, waChatIdString, waChatId)
	}
	return threadId, created, nil
}

func TgDownloadByFilePath(b *gotgbot.Bot, filePath string) ([]byte, error) {
	if state.State.Config.Telegram.SelfHostedAPI {
		return os.ReadFile(filePath)
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/file/bot%s/%s",
		state.State.Config.Telegram.APIURL, b.Token, filePath), nil)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("received non-200 status code : %s", res.Status)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return bodyBytes, nil
}

func TgReplyTextByContext(b *gotgbot.Bot, c *ext.Context, text string, buttons *gotgbot.InlineKeyboardMarkup, silent bool) (*gotgbot.Message, error) {
	sendOpts := &gotgbot.SendMessageOpts{
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: c.EffectiveMessage.MessageId,
		},
	}
	if tid := TgEffectiveMessageThreadId(c.EffectiveMessage); tid != 0 {
		sendOpts.MessageThreadId = tid
	}
	if buttons != nil {
		sendOpts.ReplyMarkup = buttons
	}

	if silent {
		sendOpts.DisableNotification = true
	}

	msg, err := b.SendMessage(c.EffectiveChat.Id, text, sendOpts)
	return msg, err
}

// TgReplyHTMLByContext is like TgReplyTextByContext but uses ParseMode HTML.
func TgReplyHTMLByContext(b *gotgbot.Bot, c *ext.Context, text string, buttons *gotgbot.InlineKeyboardMarkup, silent bool) (*gotgbot.Message, error) {
	sendOpts := &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: c.EffectiveMessage.MessageId,
		},
	}
	if tid := TgEffectiveMessageThreadId(c.EffectiveMessage); tid != 0 {
		sendOpts.MessageThreadId = tid
	}
	if buttons != nil {
		sendOpts.ReplyMarkup = buttons
	}
	if silent {
		sendOpts.DisableNotification = true
	}
	return b.SendMessage(c.EffectiveChat.Id, text, sendOpts)
}

// ForumMetaOnThreadSendFailure is set by the telegram package to reprovision stale meta forum topics.
var ForumMetaOnThreadSendFailure func(chatID, threadID int64, err error)

// TgNotifyForumMetaSendFailure invokes meta-topic reprovision when a send fails with a dead forum thread.
func TgNotifyForumMetaSendFailure(chatID, threadID int64, err error) {
	if err == nil || threadID == 0 || ForumMetaOnThreadSendFailure == nil {
		return
	}
	if !TgErrForumTopicOrThreadInvalid(err) {
		return
	}
	ForumMetaOnThreadSendFailure(chatID, threadID, err)
}

func TgSendTextById(b *gotgbot.Bot, chatId int64, threadId int64, text string) error {
	_, err := b.SendMessage(chatId, text, &gotgbot.SendMessageOpts{
		MessageThreadId: threadId})
	if err != nil {
		TgNotifyForumMetaSendFailure(chatId, threadId, err)
	}
	return err
}

func TgUpdateIsAuthorized(b *gotgbot.Bot, c *ext.Context) bool {
	var (
		cfg         = state.State.Config
		sender      = c.EffectiveSender.User
		ownerID     = cfg.Telegram.OwnerID
		sudoUsersID = cfg.Telegram.SudoUsersID
	)

	if sender != nil &&
		(slices.Contains(sudoUsersID, sender.Id) || sender.Id == ownerID) {
		return true
	}

	if c.CallbackQuery != nil {
		c.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      "Not authorized to use this bot",
			ShowAlert: true,
			CacheTime: 60,
		})
	}

	return false
}

func TgReplyWithErrorByContext(b *gotgbot.Bot, c *ext.Context, eMessage string, e error) error {
	if c.CallbackQuery != nil {
		_, err := c.CallbackQuery.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      eMessage + ":\n\n" + e.Error(),
			ShowAlert: true,
		})
		return err
	}

	sendOpts := &gotgbot.SendMessageOpts{
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId: c.EffectiveMessage.MessageId,
		},
	}
	if tid := TgEffectiveMessageThreadId(c.EffectiveMessage); tid != 0 {
		sendOpts.MessageThreadId = tid
	}
	_, err := b.SendMessage(c.EffectiveChat.Id,
		fmt.Sprintf("%s:\n\n<code>%s</code>", eMessage, html.EscapeString(e.Error())),
		sendOpts)
	return err
}

func TgSendErrorById(b *gotgbot.Bot, chatId, threadId int64, eMessage string, e error) error {
	_, err := b.SendMessage(
		chatId,
		fmt.Sprintf("%s:\n\n<code>%s</code>", eMessage, html.EscapeString(e.Error())),
		&gotgbot.SendMessageOpts{
			MessageThreadId: threadId,
		},
	)
	if err != nil {
		TgNotifyForumMetaSendFailure(chatId, threadId, err)
	}
	return err
}

func TgSendToWhatsApp(b *gotgbot.Bot, c *ext.Context,
	msgToForward, msgToReplyTo *gotgbot.Message,
	waChatJID waTypes.JID, participant, stanzaId string,
	isReply bool) error {

	var (
		cfg      = state.State.Config
		logger   = state.State.Logger
		waClient = state.State.WhatsAppClient
		mentions = []string{}
	)
	tgThreadForPairs := TgEffectiveMessageThreadId(msgToForward)

	var entities []gotgbot.ParsedMessageEntity
	if len(msgToForward.Entities) > 0 {
		entities = msgToForward.ParseEntities()
	} else if len(msgToForward.CaptionEntities) > 0 {
		entities = msgToForward.ParseCaptionEntities()
	}

	for _, entity := range entities {
		if entity.Type == "mention" {
			username := entity.Text[1:]
			// Check if its a number
			for _, c := range username {
				if !unicode.IsDigit(c) {
					continue
				}
			}

			parsedJID, _ := WaParseJID(username)
			mentions = append(mentions, parsedJID.String())
		}
	}

	if cfg.Telegram.SendMyPresence {
		err := waClient.SendPresence(context.Background(), waTypes.PresenceAvailable)
		if err != nil {
			logger.Warn("failed to send presence",
				zap.Error(err),
				zap.String("presence", string(waTypes.PresenceAvailable)),
			)
		}

		go func() {
			time.Sleep(10 * time.Second)
			err := waClient.SendPresence(context.Background(), waTypes.PresenceUnavailable)
			if err != nil {
				logger.Warn("failed to send presence",
					zap.Error(err),
					zap.String("presence", string(waTypes.PresenceUnavailable)),
				)
			}
		}()
	}

	isEphemeral, ephemeralTimer, ephemeralFound, err := database.GetEphemeralSettings(waChatJID.String())
	if err != nil {
		logger.Info(
			"failed to get ephemeral setttings from database",
			zap.Error(err),
			zap.String("jid", waChatJID.String()),
		)
	}

	if !ephemeralFound && waChatJID.Server == waTypes.GroupServer {
		groupInfo, err := waClient.GetGroupInfo(context.Background(), waChatJID)
		if err != nil {
			logger.Info(
				"failed to get group info from WhatsApp",
				zap.Error(err),
				zap.String("jid", waChatJID.String()),
			)
		} else {
			isEphemeral = groupInfo.IsEphemeral
			ephemeralTimer = groupInfo.DisappearingTimer
			err = database.UpdateEphemeralSettings(waChatJID.String(), isEphemeral, ephemeralTimer)
			if err != nil {
				logger.Info(
					"failed to update group ephemeral setttings in database",
					zap.Error(err),
					zap.String("jid", waChatJID.String()),
				)
			}
		}
	}

	if msgToForward.Photo != nil && len(msgToForward.Photo) > 0 {

		bestPhoto := msgToForward.Photo[0]
		for _, photo := range msgToForward.Photo {
			if photo.Height*photo.Width > bestPhoto.Height*bestPhoto.Width {
				bestPhoto = photo
			}
		}

		if !cfg.Telegram.SelfHostedAPI && bestPhoto.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send photo as it exceeds Telegram size restriction", nil, false)
			return err
		}

		imageFile, err := b.GetFile(bestPhoto.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive image file from Telegram", err)
		}

		imageBytes, err := TgDownloadByFilePath(b, imageFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download image from Telegram", err)
		}

		uploadedImage, err := waClient.Upload(context.Background(), imageBytes, whatsmeow.MediaImage)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload image to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:           proto.String(msgToForward.Caption),
				URL:               proto.String(uploadedImage.URL),
				DirectPath:        proto.String(uploadedImage.DirectPath),
				MediaKey:          uploadedImage.MediaKey,
				MediaKeyTimestamp: proto.Int64(time.Now().Unix()),
				Mimetype:          proto.String(http.DetectContentType(imageBytes)),
				FileEncSHA256:     uploadedImage.FileEncSHA256,
				FileSHA256:        uploadedImage.FileSHA256,
				FileLength:        proto.Uint64(uint64(len(imageBytes))),
				ViewOnce:          proto.Bool(msgToForward.HasProtectedContent || (msgToForward.HasMediaSpoiler && cfg.Telegram.SpoilerViewOnce)),
				Height:            proto.Uint32(uint32(bestPhoto.Height)),
				Width:             proto.Uint32(uint32(bestPhoto.Width)),
				ContextInfo:       &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.ImageMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.ImageMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.ImageMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.ImageMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.ImageMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send image to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}

	} else if msgToForward.Video != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Video.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send video as it exceeds Telegram size restriction", nil, false)
			return err
		}

		videoFile, err := b.GetFile(msgToForward.Video.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive video file from Telegram", err)
		}

		videoBytes, err := TgDownloadByFilePath(b, videoFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download video from Telegram", err)
		}

		uploadedVideo, err := waClient.Upload(context.Background(), videoBytes, whatsmeow.MediaVideo)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload video to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				URL:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(msgToForward.Video.MimeType),
				FileEncSHA256: uploadedVideo.FileEncSHA256,
				FileSHA256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(videoBytes))),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent || (msgToForward.HasMediaSpoiler && cfg.Telegram.SpoilerViewOnce)),
				Seconds:       proto.Uint32(uint32(msgToForward.Video.Duration)),
				GifPlayback:   proto.Bool(false),
				Height:        proto.Uint32(uint32(msgToForward.Video.Height)),
				Width:         proto.Uint32(uint32(msgToForward.Video.Width)),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.VideoMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.VideoMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.VideoMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.VideoMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send video to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.VideoNote != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.VideoNote.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send video note as it exceeds Telegram size restriction", nil, false)
			return err
		}

		videoFile, err := b.GetFile(msgToForward.VideoNote.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive video note file from Telegram", err)
		}

		videoBytes, err := TgDownloadByFilePath(b, videoFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download video note from Telegram", err)
		}

		uploadedVideo, err := waClient.Upload(context.Background(), videoBytes, whatsmeow.MediaVideo)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload video note to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			PtvMessage: &waE2E.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				URL:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(http.DetectContentType(videoBytes)),
				FileEncSHA256: uploadedVideo.FileEncSHA256,
				FileSHA256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(videoBytes))),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent || (msgToForward.HasMediaSpoiler && cfg.Telegram.SpoilerViewOnce)),
				Seconds:       proto.Uint32(uint32(msgToForward.VideoNote.Duration)),
				GifPlayback:   proto.Bool(false),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.PtvMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.PtvMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.PtvMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.PtvMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.PtvMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send video note to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Animation != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Animation.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send animation as it exceeds Telegram size restriction", nil, false)
			return err
		}

		animationFile, err := b.GetFile(msgToForward.Animation.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive animation file from Telegram", err)
		}

		animationBytes, err := TgDownloadByFilePath(b, animationFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download animation from Telegram", err)
		}

		uploadedAnimation, err := waClient.Upload(context.Background(), animationBytes, whatsmeow.MediaVideo)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload animation to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:        proto.String(msgToForward.Caption),
				URL:            proto.String(uploadedAnimation.URL),
				DirectPath:     proto.String(uploadedAnimation.DirectPath),
				MediaKey:       uploadedAnimation.MediaKey,
				Mimetype:       proto.String(msgToForward.Animation.MimeType),
				GifPlayback:    proto.Bool(true),
				FileEncSHA256:  uploadedAnimation.FileEncSHA256,
				FileSHA256:     uploadedAnimation.FileSHA256,
				FileLength:     proto.Uint64(uint64(len(animationBytes))),
				ViewOnce:       proto.Bool(msgToForward.HasProtectedContent || (msgToForward.HasMediaSpoiler && cfg.Telegram.SpoilerViewOnce)),
				Height:         proto.Uint32(uint32(msgToForward.Animation.Height)),
				Width:          proto.Uint32(uint32(msgToForward.Animation.Width)),
				Seconds:        proto.Uint32(uint32(msgToForward.Animation.Duration)),
				GifAttribution: waE2E.VideoMessage_TENOR.Enum(),
				ContextInfo:    &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.VideoMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.VideoMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.VideoMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.VideoMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send animation to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Audio != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Audio.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send audio as it exceeds Telegram size restriction", nil, false)
			return err
		}

		audioFile, err := b.GetFile(msgToForward.Audio.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive audio file from Telegram", err)
		}

		audioBytes, err := TgDownloadByFilePath(b, audioFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download audio from Telegram", err)
		}

		uploadedAudio, err := waClient.Upload(context.Background(), audioBytes, whatsmeow.MediaAudio)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload audio to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploadedAudio.URL),
				DirectPath:    proto.String(uploadedAudio.DirectPath),
				MediaKey:      uploadedAudio.MediaKey,
				Mimetype:      proto.String(msgToForward.Audio.MimeType),
				FileEncSHA256: uploadedAudio.FileEncSHA256,
				FileSHA256:    uploadedAudio.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(audioBytes))),
				Seconds:       proto.Uint32(uint32(msgToForward.Audio.Duration)),
				PTT:           proto.Bool(false),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.AudioMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.AudioMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.AudioMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.AudioMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send audio to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Voice != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Voice.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send voice as it exceeds Telegram size restriction", nil, false)
			return err
		}

		voiceFile, err := b.GetFile(msgToForward.Voice.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive voice file from Telegram", err)
		}

		voiceBytes, err := TgDownloadByFilePath(b, voiceFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download voice from Telegram", err)
		}

		uploadedVoice, err := waClient.Upload(context.Background(), voiceBytes, whatsmeow.MediaAudio)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload voice to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploadedVoice.URL),
				DirectPath:    proto.String(uploadedVoice.DirectPath),
				MediaKey:      uploadedVoice.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSHA256: uploadedVoice.FileEncSHA256,
				FileSHA256:    uploadedVoice.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(voiceBytes))),
				Seconds:       proto.Uint32(uint32(msgToForward.Voice.Duration)),
				PTT:           proto.Bool(true),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.AudioMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.AudioMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.AudioMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.AudioMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send voice to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Document != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Document.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send document as it exceeds Telegram size restriction", nil, false)
			return err
		}

		documentFile, err := b.GetFile(msgToForward.Document.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive document file from Telegram", err)
		}

		documentBytes, err := TgDownloadByFilePath(b, documentFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download document from Telegram", err)
		}

		uploadedDocument, err := waClient.Upload(context.Background(), documentBytes, whatsmeow.MediaDocument)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload document to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				Caption:       proto.String(msgToForward.Caption),
				Title:         proto.String(msgToForward.Document.FileName),
				FileName:      proto.String(msgToForward.Document.FileName),
				URL:           proto.String(uploadedDocument.URL),
				DirectPath:    proto.String(uploadedDocument.DirectPath),
				MediaKey:      uploadedDocument.MediaKey,
				Mimetype:      proto.String(msgToForward.Document.MimeType),
				FileEncSHA256: uploadedDocument.FileEncSHA256,
				FileSHA256:    uploadedDocument.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(documentBytes))),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.DocumentMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.DocumentMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.DocumentMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.DocumentMessage.ContextInfo.MentionedJID = mentions
		}
		if isEphemeral {
			msgToSend.DocumentMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send document to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Sticker != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Sticker.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send sticker as it exceeds Telegram size restriction", nil, false)
			return err
		}

		stickerFile, err := b.GetFile(msgToForward.Sticker.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to retreive sticker file from Telegram", err)
		}

		stickerBytes, err := TgDownloadByFilePath(b, stickerFile.FilePath)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to download sticker from Telegram", err)
		}

		if msgToForward.Sticker.IsAnimated {
			stickerBytes, err = TGSConvertToWebp(stickerBytes, c.UpdateId)
			if err != nil {
				return TgReplyWithErrorByContext(b, c, "Failed to convert TGS sticker to WebP", err)
			}
		} else if msgToForward.Sticker.IsVideo && !cfg.Telegram.SkipVideoStickers {

			var scale, pad string

			if msgToForward.Sticker.Height == 512 && msgToForward.Sticker.Width == 512 {
				scale = "512:512"
				pad = "0:0:0:0"
			} else if msgToForward.Sticker.Height == 512 {
				scale = "-1:512"
				pad = fmt.Sprintf("512:512:%v:0", (512-msgToForward.Sticker.Width)/2)
			} else {
				scale = "512:-1"
				pad = fmt.Sprintf("512:512:0:%v", (512-msgToForward.Sticker.Height)/2)
			}

			stickerBytes, err = WebmConvertToWebp(stickerBytes, scale, pad, c.UpdateId)
			if err != nil {
				return TgReplyWithErrorByContext(b, c, "Failed to convert WEBM sticker to GIF", err)
			}
		} else if !msgToForward.Sticker.IsAnimated || !msgToForward.Sticker.IsVideo {

			var wPad, hPad int

			if msgToForward.Sticker.Height != 512 {
				hPad = int(512 - msgToForward.Sticker.Height)
			}
			if msgToForward.Sticker.Width != 512 {
				wPad = int(512 - msgToForward.Sticker.Width)
			}

			stickerBytes, err = WebpImagePad(stickerBytes, wPad, hPad, c.UpdateId)
			if err != nil {
				return TgReplyWithErrorByContext(b, c, "Failed to pad WEBP sticker to 512x512", err)
			}
		}

		uploadedSticker, err := waClient.Upload(context.Background(), stickerBytes, whatsmeow.MediaImage)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to upload sticker to WhatsApp", err)
		}

		msgToSend := &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String(uploadedSticker.URL),
				DirectPath:    proto.String(uploadedSticker.DirectPath),
				MediaKey:      uploadedSticker.MediaKey,
				IsAnimated:    proto.Bool(msgToForward.Sticker.IsAnimated || msgToForward.Sticker.IsVideo),
				IsAvatar:      proto.Bool(false),
				Height:        proto.Uint32(uint32(msgToForward.Sticker.Height)),
				Width:         proto.Uint32(uint32(msgToForward.Sticker.Width)),
				Mimetype:      proto.String("image/webp"),
				FileEncSHA256: uploadedSticker.FileEncSHA256,
				FileSHA256:    uploadedSticker.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(stickerBytes))),
				StickerSentTS: proto.Int64(time.Now().Unix()),
				ContextInfo:   &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.StickerMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.StickerMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.StickerMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if isEphemeral {
			msgToSend.StickerMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send sticker to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Contact != nil {

		contact := msgToForward.Contact

		var displayName string
		if contact.FirstName != "" {
			displayName = contact.FirstName
			if contact.LastName != "" {
				displayName += (" " + contact.LastName)
			}
		} else {
			displayName = contact.PhoneNumber
		}

		var vcard string
		if contact.Vcard == "" {

			card := goVCard.Card{}
			card.SetName(&goVCard.Name{
				FamilyName: contact.LastName,
				GivenName:  contact.FirstName,
			})
			card.SetValue(goVCard.FieldTelephone, contact.PhoneNumber)
			card.SetValue(goVCard.FieldFormattedName, displayName)
			card.SetValue(goVCard.FieldVersion, "3.0")

			vcardBytes := bytes.NewBuffer([]byte{})
			encoder := goVCard.NewEncoder(vcardBytes)
			encoder.Encode(card)

			vcard = vcardBytes.String()
		} else {
			vcard = contact.Vcard
		}

		msgToSend := &waE2E.Message{
			ContactMessage: &waE2E.ContactMessage{
				DisplayName: &displayName,
				Vcard:       &vcard,
				ContextInfo: &waE2E.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.ContactMessage.ContextInfo.StanzaID = proto.String(stanzaId)
			msgToSend.ContactMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.ContactMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
		}
		if isEphemeral {
			msgToSend.ContactMessage.ContextInfo.Expiration = &ephemeralTimer
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send sticker to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}

	} else if msgToForward.Location != nil {

		location := msgToForward.Location
		isLive := (location.LivePeriod > 0)

		msgToSend := &waE2E.Message{}
		if isLive {
			// TODO: make this live
			msgToSend.LiveLocationMessage = &waE2E.LiveLocationMessage{
				DegreesLatitude:                   &location.Latitude,
				DegreesLongitude:                  &location.Longitude,
				AccuracyInMeters:                  proto.Uint32(uint32(location.HorizontalAccuracy)),
				DegreesClockwiseFromMagneticNorth: proto.Uint32(uint32(location.Heading)),
				ContextInfo:                       &waE2E.ContextInfo{},
			}
			if isReply {
				msgToSend.LiveLocationMessage.ContextInfo.StanzaID = proto.String(stanzaId)
				msgToSend.LiveLocationMessage.ContextInfo.Participant = proto.String(participant)
				msgToSend.LiveLocationMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
			}
			if isEphemeral {
				msgToSend.LiveLocationMessage.ContextInfo.Expiration = &ephemeralTimer
			}
		} else {
			msgToSend.LocationMessage = &waE2E.LocationMessage{
				DegreesLatitude:                   &location.Latitude,
				DegreesLongitude:                  &location.Longitude,
				DegreesClockwiseFromMagneticNorth: proto.Uint32(uint32(location.Heading)),
				AccuracyInMeters:                  proto.Uint32(uint32(location.HorizontalAccuracy)),
				ContextInfo:                       &waE2E.ContextInfo{},
			}
			if isReply {
				msgToSend.LocationMessage.ContextInfo.StanzaID = proto.String(stanzaId)
				msgToSend.LocationMessage.ContextInfo.Participant = proto.String(participant)
				msgToSend.LocationMessage.ContextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
			}
			if isEphemeral {
				msgToSend.LocationMessage.ContextInfo.Expiration = &ephemeralTimer
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send sticker to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}

	} else if msgToForward.Text != "" {

		if emojis := gomoji.CollectAll(msgToForward.Text); isReply && len(emojis) == 1 && gomoji.RemoveEmojis(msgToForward.Text) == "" {
			_, err := waClient.SendMessage(context.Background(), waChatJID, &waE2E.Message{
				ReactionMessage: &waE2E.ReactionMessage{
					Text:              proto.String(msgToForward.Text),
					SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
					Key: &waCommon.MessageKey{
						RemoteJID: proto.String(waChatJID.String()),
						FromMe:    proto.Bool(msgToReplyTo != nil && msgToReplyTo.From.Id != b.Id),
						ID:        proto.String(stanzaId),
					},
				},
			})
			if err != nil {
				return TgReplyWithErrorByContext(b, c, "Failed to send reaction to WhatsApp", err)
			}
			if cfg.Telegram.ConfirmationType != "none" {
				msg, err := TgReplyTextByContext(b, c, "Successfully reacted", nil, cfg.Telegram.SilentConfirmation)

				if err == nil {
					go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
						time.Sleep(15 * time.Second)
						_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
					}(b, msg)
				}
				return err
			}
			return err
		}

		msgToSend := &waE2E.Message{}
		if isReply || len(mentions) > 0 || isEphemeral {
			msgToSend.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
				Text: proto.String(msgToForward.Text),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:      proto.String(stanzaId),
					Participant:   proto.String(participant),
					QuotedMessage: &waE2E.Message{Conversation: proto.String("")},
				},
			}
			if len(mentions) > 0 {
				msgToSend.ExtendedTextMessage.ContextInfo.MentionedJID = mentions
			}
			if isEphemeral {
				msgToSend.ExtendedTextMessage.ContextInfo.Expiration = &ephemeralTimer
			}
		} else {
			msgToSend.Conversation = proto.String(msgToForward.Text)
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send message to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		SendMessageConfirmation(b, c, cfg, msgToForward, revokeKeyboard)

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, tgThreadForPairs)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}

		{
			textSplit := strings.Fields(strings.ToLower(msgToForward.Text))
			if slices.Contains(textSplit, "@all") || slices.Contains(textSplit, "@everyone") {
				WaTagAll(waChatJID, msgToSend, sentMsg.ID, waClient.Store.ID.String(), true)
			}
		}

	}

	if cfg.Telegram.SendMyReadReceipts {
		unreadMsgs, err := database.MsgIdGetUnread(waChatJID.String())
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Message sent but failed to get unread messages to mark them read", err)
		}

		for sender, msgIds := range unreadMsgs {
			senderJID, _ := WaParseJID(sender)
			err := waClient.MarkRead(context.Background(), msgIds, time.Now(), waChatJID, senderJID)
			if err != nil {
				logger.Warn(
					"failed to mark messages as read",
					zap.String("chat_id", waChatJID.String()),
					zap.Any("msg_ids", msgIds),
					zap.String("sender", senderJID.String()),
				)
			} else {
				for _, msgId := range msgIds {
					database.MsgIdMarkRead(waChatJID.String(), msgId)
				}
			}
		}

		// waClient.MarkRead(unreadMsgs, time.Now(), waChatJID, )
	}

	return nil
}

func TgMakeRevokeKeyboard(msgId, chatId string, confirm bool) *gotgbot.InlineKeyboardMarkup {

	if confirm {
		return &gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{{
					Text:         "No, go back",
					CallbackData: "revoke_" + msgId + "_" + chatId + "_n",
				}},
				{{
					Text:         "Yes, I am sure",
					CallbackData: "revoke_" + msgId + "_" + chatId + "_y",
				}},
			},
		}
	}

	return &gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{{
			Text:         "Revoke",
			CallbackData: "revoke_" + msgId + "_" + chatId,
		}}},
	}
}

func TgBuildUrlButton(text, url string) gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{
		InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{{
			Text: text,
			Url:  url,
		}}},
	}
}

func SendMessageConfirmation(
	b *gotgbot.Bot,
	c *ext.Context,
	cfg *state.Config,
	msgToForward *gotgbot.Message,
	revokeKeyboard *gotgbot.InlineKeyboardMarkup,
) {
	// Fixed default emoji (non-configurable).
	_, _ = b.SetMessageReaction(msgToForward.Chat.Id, msgToForward.MessageId, &gotgbot.SetMessageReactionOpts{
		Reaction: []gotgbot.ReactionType{gotgbot.ReactionTypeEmoji{Emoji: "👍"}},
	})
}
