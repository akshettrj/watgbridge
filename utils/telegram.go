package utils

import (
	"context"
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
	"github.com/forPelevin/gomoji"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

const (
	DownloadSizeLimit int64  = 20971520
	UploadSizeLimit   uint64 = 52428800
)

func TgRegisterBotCommands(b *gotgbot.Bot, commands ...gotgbot.BotCommand) error {
	_, err := b.SetMyCommands(commands, &gotgbot.SetMyCommandsOpts{
		LanguageCode: "en",
		Scope:        gotgbot.BotCommandScopeDefault{},
	})
	return err
}

func TgGetOrMakeThreadFromWa(waChatId string, tgChatId int64, threadName string) (int64, error) {
	threadId, threadFound, err := database.ChatThreadGetTgFromWa(waChatId, tgChatId)
	if err != nil {
		return 0, err
	}

	if !threadFound {
		tgBot := state.State.TelegramBot
		newForum, err := tgBot.CreateForumTopic(tgChatId, threadName, &gotgbot.CreateForumTopicOpts{})
		if err != nil {
			return 0, err
		}
		err = database.ChatThreadAddNewPair(waChatId, tgChatId, newForum.MessageThreadId)
		if err != nil {
			return newForum.MessageThreadId, err
		}
		return newForum.MessageThreadId, nil
	}

	return threadId, nil
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

func TgReplyTextByContext(b *gotgbot.Bot, c *ext.Context, text string, buttons *gotgbot.InlineKeyboardMarkup) (*gotgbot.Message, error) {
	sendOpts := &gotgbot.SendMessageOpts{
		ReplyToMessageId: c.EffectiveMessage.MessageId,
	}
	if c.EffectiveMessage.IsTopicMessage {
		sendOpts.MessageThreadId = c.EffectiveMessage.MessageThreadId
	}
	if buttons != nil {
		sendOpts.ReplyMarkup = buttons
	}
	msg, err := b.SendMessage(c.EffectiveChat.Id, text, sendOpts)
	return msg, err
}

func TgSendTextById(b *gotgbot.Bot, chatId int64, threadId int64, text string) error {
	_, err := b.SendMessage(chatId, text, &gotgbot.SendMessageOpts{
		MessageThreadId: threadId})
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
		ReplyToMessageId: c.EffectiveMessage.MessageId,
	}
	if c.EffectiveMessage.IsTopicMessage {
		sendOpts.MessageThreadId = c.EffectiveMessage.MessageThreadId
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
		err := waClient.SendPresence(waTypes.PresenceAvailable)
		if err != nil {
			logger.Warn("failed to send presence",
				zap.Error(err),
				zap.String("presence", string(waTypes.PresenceAvailable)),
			)
		}

		go func() {
			time.Sleep(10 * time.Second)
			err := waClient.SendPresence(waTypes.PresenceUnavailable)
			if err != nil {
				logger.Warn("failed to send presence",
					zap.Error(err),
					zap.String("presence", string(waTypes.PresenceUnavailable)),
				)
			}
		}()
	}

	if msgToForward.Photo != nil && len(msgToForward.Photo) > 0 {

		bestPhoto := msgToForward.Photo[0]
		for _, photo := range msgToForward.Photo {
			if photo.Height*photo.Width > bestPhoto.Height*bestPhoto.Width {
				bestPhoto = photo
			}
		}

		if !cfg.Telegram.SelfHostedAPI && bestPhoto.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send photo as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				Caption:           proto.String(msgToForward.Caption),
				Url:               proto.String(uploadedImage.URL),
				DirectPath:        proto.String(uploadedImage.DirectPath),
				MediaKey:          uploadedImage.MediaKey,
				MediaKeyTimestamp: proto.Int64(time.Now().Unix()),
				Mimetype:          proto.String(http.DetectContentType(imageBytes)),
				FileEncSha256:     uploadedImage.FileEncSHA256,
				FileSha256:        uploadedImage.FileSHA256,
				FileLength:        proto.Uint64(uint64(len(imageBytes))),
				ViewOnce:          proto.Bool(msgToForward.HasProtectedContent),
				Height:            proto.Uint32(uint32(bestPhoto.Height)),
				Width:             proto.Uint32(uint32(bestPhoto.Width)),
				ContextInfo:       &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.ImageMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.ImageMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.ImageMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.ImageMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send image to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}

	} else if msgToForward.Video != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Video.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send video as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(msgToForward.Video.MimeType),
				FileEncSha256: uploadedVideo.FileEncSHA256,
				FileSha256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(videoBytes))),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
				Seconds:       proto.Uint32(uint32(msgToForward.Video.Duration)),
				GifPlayback:   proto.Bool(false),
				Height:        proto.Uint32(uint32(msgToForward.Video.Height)),
				Width:         proto.Uint32(uint32(msgToForward.Video.Width)),
				ContextInfo:   &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.VideoMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.VideoMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.VideoMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send video to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.VideoNote != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.VideoNote.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send video note as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(http.DetectContentType(videoBytes)),
				FileEncSha256: uploadedVideo.FileEncSHA256,
				FileSha256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(videoBytes))),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
				Seconds:       proto.Uint32(uint32(msgToForward.VideoNote.Duration)),
				GifPlayback:   proto.Bool(false),
				ContextInfo:   &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.VideoMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.VideoMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.VideoMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send video note to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Animation != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Animation.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send animation as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:        proto.String(msgToForward.Caption),
				Url:            proto.String(uploadedAnimation.URL),
				DirectPath:     proto.String(uploadedAnimation.DirectPath),
				MediaKey:       uploadedAnimation.MediaKey,
				Mimetype:       proto.String(msgToForward.Animation.MimeType),
				GifPlayback:    proto.Bool(true),
				FileEncSha256:  uploadedAnimation.FileEncSHA256,
				FileSha256:     uploadedAnimation.FileSHA256,
				FileLength:     proto.Uint64(uint64(len(animationBytes))),
				ViewOnce:       proto.Bool(msgToForward.HasProtectedContent),
				Height:         proto.Uint32(uint32(msgToForward.Animation.Height)),
				Width:          proto.Uint32(uint32(msgToForward.Animation.Width)),
				Seconds:        proto.Uint32(uint32(msgToForward.Animation.Duration)),
				GifAttribution: waProto.VideoMessage_TENOR.Enum(),
				ContextInfo:    &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.VideoMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.VideoMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.VideoMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send animation to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Audio != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Audio.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send audio as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				Url:           proto.String(uploadedAudio.URL),
				DirectPath:    proto.String(uploadedAudio.DirectPath),
				MediaKey:      uploadedAudio.MediaKey,
				Mimetype:      proto.String(msgToForward.Audio.MimeType),
				FileEncSha256: uploadedAudio.FileEncSHA256,
				FileSha256:    uploadedAudio.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(audioBytes))),
				Seconds:       proto.Uint32(uint32(msgToForward.Audio.Duration)),
				Ptt:           proto.Bool(false),
				ContextInfo:   &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.AudioMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.AudioMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.AudioMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send audio to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Voice != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Voice.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send voice as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				Url:           proto.String(uploadedVoice.URL),
				DirectPath:    proto.String(uploadedVoice.DirectPath),
				MediaKey:      uploadedVoice.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSha256: uploadedVoice.FileEncSHA256,
				FileSha256:    uploadedVoice.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(voiceBytes))),
				Seconds:       proto.Uint32(uint32(msgToForward.Voice.Duration)),
				Ptt:           proto.Bool(true),
				ContextInfo:   &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.AudioMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.AudioMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.AudioMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send voice to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Document != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Document.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send document as it exceeds Telegram size restriction", nil)
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

		splitName := strings.Split(msgToForward.Document.FileName, ".")
		documentFileName := strings.Join(splitName[:len(splitName)-1], ".")

		msgToSend := &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				Caption:       proto.String(msgToForward.Caption),
				Title:         proto.String(documentFileName),
				Url:           proto.String(uploadedDocument.URL),
				DirectPath:    proto.String(uploadedDocument.DirectPath),
				MediaKey:      uploadedDocument.MediaKey,
				Mimetype:      proto.String(msgToForward.Document.MimeType),
				FileEncSha256: uploadedDocument.FileEncSHA256,
				FileSha256:    uploadedDocument.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(documentBytes))),
				ContextInfo:   &waProto.ContextInfo{},
			},
		}
		if isReply {
			msgToSend.DocumentMessage.ContextInfo.StanzaId = proto.String(stanzaId)
			msgToSend.DocumentMessage.ContextInfo.Participant = proto.String(participant)
			msgToSend.DocumentMessage.ContextInfo.QuotedMessage = &waProto.Message{Conversation: proto.String("")}
		}
		if len(mentions) > 0 {
			msgToSend.DocumentMessage.ContextInfo.MentionedJid = mentions
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send document to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Sticker != nil {

		if !cfg.Telegram.SelfHostedAPI && msgToForward.Sticker.FileSize > DownloadSizeLimit {
			_, err := TgReplyTextByContext(b, c, "Unable to send sticker as it exceeds Telegram size restriction", nil)
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

		msgToSend := &waProto.Message{
			StickerMessage: &waProto.StickerMessage{
				Url:           proto.String(uploadedSticker.URL),
				DirectPath:    proto.String(uploadedSticker.DirectPath),
				MediaKey:      uploadedSticker.MediaKey,
				IsAnimated:    proto.Bool(msgToForward.Sticker.IsAnimated || msgToForward.Sticker.IsVideo),
				IsAvatar:      proto.Bool(false),
				Height:        proto.Uint32(uint32(msgToForward.Sticker.Height)),
				Width:         proto.Uint32(uint32(msgToForward.Sticker.Width)),
				Mimetype:      proto.String("image/webp"),
				FileEncSha256: uploadedSticker.FileEncSHA256,
				FileSha256:    uploadedSticker.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(stickerBytes))),
				StickerSentTs: proto.Int64(time.Now().Unix()),
			},
		}
		if isReply {
			msgToSend.StickerMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send sticker to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to add to database", err)
		}
	} else if msgToForward.Text != "" {

		if emojis := gomoji.CollectAll(msgToForward.Text); isReply && len(emojis) == 1 && gomoji.RemoveEmojis(msgToForward.Text) == "" {
			_, err := waClient.SendMessage(context.Background(), waChatJID, &waProto.Message{
				ReactionMessage: &waProto.ReactionMessage{
					Text:              proto.String(msgToForward.Text),
					SenderTimestampMs: proto.Int64(time.Now().UnixMilli()),
					Key: &waProto.MessageKey{
						RemoteJid: proto.String(waChatJID.String()),
						FromMe:    proto.Bool(msgToReplyTo != nil && msgToReplyTo.From.Id != b.Id),
						Id:        proto.String(stanzaId),
					},
				},
			})
			if err != nil {
				return TgReplyWithErrorByContext(b, c, "Failed to send reaction to WhatsApp", err)
			}
			msg, err := TgReplyTextByContext(b, c, "Successfully reacted", nil)
			if err == nil {
				go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
					time.Sleep(15 * time.Second)
					_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
				}(b, msg)
			}
			return err
		}

		msgToSend := &waProto.Message{}
		if isReply || len(mentions) > 0 {
			msgToSend.ExtendedTextMessage = &waProto.ExtendedTextMessage{
				Text: proto.String(msgToForward.Text),
				ContextInfo: &waProto.ContextInfo{
					StanzaId:      proto.String(stanzaId),
					Participant:   proto.String(participant),
					QuotedMessage: &waProto.Message{Conversation: proto.String("")},
				},
			}
			if len(mentions) > 0 {
				msgToSend.ExtendedTextMessage.ContextInfo.MentionedJid = mentions
			}
		} else {
			msgToSend.Conversation = proto.String(msgToForward.Text)
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, msgToSend)
		if err != nil {
			return TgReplyWithErrorByContext(b, c, "Failed to send message to WhatsApp", err)
		}
		revokeKeyboard := TgMakeRevokeKeyboard(sentMsg.ID, waChatJID.String(), false)
		msg, err := TgReplyTextByContext(b, c, "Successfully sent", revokeKeyboard)
		if err == nil {
			go func(_b *gotgbot.Bot, _m *gotgbot.Message) {
				time.Sleep(15 * time.Second)
				_b.DeleteMessage(_m.Chat.Id, _m.MessageId, &gotgbot.DeleteMessageOpts{})
			}(b, msg)
		}

		err = database.MsgIdAddNewPair(sentMsg.ID, waClient.Store.ID.String(), waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId, msgToForward.MessageThreadId)
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
			err := waClient.MarkRead(msgIds, time.Now(), waChatJID, senderJID)
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
