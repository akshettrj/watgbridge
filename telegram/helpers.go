package telegram

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/forPelevin/gomoji"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func sendToWhatsApp(b *gotgbot.Bot, c *ext.Context,
	msgToForward, msgToReplyTo *gotgbot.Message,
	waChatJID waTypes.JID, participant, stanzaId string,
	isReply bool) error {

	cfg := state.State.Config
	waClient := state.State.WhatsAppClient

	if msgToForward.Photo != nil && len(msgToForward.Photo) > 0 {

		bestPhoto := msgToForward.Photo[0]
		for _, photo := range msgToForward.Photo {
			if photo.Height*photo.Width > bestPhoto.Height*bestPhoto.Width {
				bestPhoto = photo
			}
		}

		if !state.State.Config.Telegram.SelfHostedApi && bestPhoto.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send photo as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		imgFile, err := b.GetFile(bestPhoto.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive image file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		imgBytes, err := utils.TelegramDownloadFileByPath(b, imgFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download image:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedImage, err := waClient.Upload(context.Background(), imgBytes, whatsmeow.MediaImage)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload image to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedImage.URL),
				DirectPath:    proto.String(uploadedImage.DirectPath),
				MediaKey:      uploadedImage.MediaKey,
				Mimetype:      proto.String(http.DetectContentType(imgBytes)),
				FileEncSha256: uploadedImage.FileEncSHA256,
				FileSha256:    uploadedImage.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(imgBytes))),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
			},
		}
		if isReply {
			msgToSend.ImageMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send image to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Video != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Video.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send video as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		vidFile, err := b.GetFile(msgToForward.Video.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive video file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		vidBytes, err := utils.TelegramDownloadFileByPath(b, vidFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download video:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedVideo, err := waClient.Upload(context.Background(), vidBytes, whatsmeow.MediaVideo)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload video to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(msgToForward.Video.MimeType),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
				FileEncSha256: uploadedVideo.FileEncSHA256,
				FileSha256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(vidBytes))),
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send video to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.VideoNote != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.VideoNote.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send video note as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		vidFile, err := b.GetFile(msgToForward.VideoNote.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive video file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		vidBytes, err := utils.TelegramDownloadFileByPath(b, vidFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download video:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedVideo, err := waClient.Upload(context.Background(), vidBytes, whatsmeow.MediaVideo)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload video to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(http.DetectContentType(vidBytes)),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
				FileEncSha256: uploadedVideo.FileEncSHA256,
				FileSha256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(vidBytes))),
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send video to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Animation != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Animation.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send animation as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		animationFile, err := b.GetFile(msgToForward.Animation.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive animation file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		animationBytes, err := utils.TelegramDownloadFileByPath(b, animationFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download animation:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedAnimation, err := waClient.Upload(context.Background(), animationBytes, whatsmeow.MediaVideo)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload animation to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(msgToForward.Caption),
				Url:           proto.String(uploadedAnimation.URL),
				DirectPath:    proto.String(uploadedAnimation.DirectPath),
				MediaKey:      uploadedAnimation.MediaKey,
				Mimetype:      proto.String(msgToForward.Animation.MimeType),
				ViewOnce:      proto.Bool(msgToForward.HasProtectedContent),
				GifPlayback:   proto.Bool(true),
				FileEncSha256: uploadedAnimation.FileEncSHA256,
				FileSha256:    uploadedAnimation.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(animationBytes))),
			},
		}
		if isReply {
			msgToSend.VideoMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send animation to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Audio != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Audio.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send audio as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		audioFile, err := b.GetFile(msgToForward.Audio.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive audio file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		audioBytes, err := utils.TelegramDownloadFileByPath(b, audioFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download audio:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedAudio, err := waClient.Upload(context.Background(), audioBytes, whatsmeow.MediaAudio)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload audio to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
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
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send audio to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Voice != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Voice.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send voice note as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		audioFile, err := b.GetFile(msgToForward.Voice.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive audio file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		audioBytes, err := utils.TelegramDownloadFileByPath(b, audioFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download audio:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedAudio, err := waClient.Upload(context.Background(), audioBytes, whatsmeow.MediaAudio)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload audio to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				Url:           proto.String(uploadedAudio.URL),
				DirectPath:    proto.String(uploadedAudio.DirectPath),
				MediaKey:      uploadedAudio.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSha256: uploadedAudio.FileEncSHA256,
				Seconds:       proto.Uint32(uint32(msgToForward.Voice.Duration)),
				Ptt:           proto.Bool(true),
				FileSha256:    uploadedAudio.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(audioBytes))),
			},
		}
		if isReply {
			msgToSend.AudioMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send audio to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Document != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Document.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send document as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		docFile, err := b.GetFile(msgToForward.Document.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive document file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		docBytes, err := utils.TelegramDownloadFileByPath(b, docFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download document:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedAnimation, err := waClient.Upload(context.Background(), docBytes, whatsmeow.MediaDocument)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload document to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		splitName := strings.Split(msgToForward.Document.FileName, ".")
		documentFileName := strings.Join(splitName[:len(splitName)-1], ".")

		msgToSend := &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				Caption:       proto.String(msgToForward.Caption),
				Title:         proto.String(documentFileName),
				Url:           proto.String(uploadedAnimation.URL),
				DirectPath:    proto.String(uploadedAnimation.DirectPath),
				MediaKey:      uploadedAnimation.MediaKey,
				Mimetype:      proto.String(msgToForward.Document.MimeType),
				FileEncSha256: uploadedAnimation.FileEncSHA256,
				FileSha256:    uploadedAnimation.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(docBytes))),
			},
		}
		if isReply {
			msgToSend.DocumentMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send document to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Sticker != nil {

		if !state.State.Config.Telegram.SelfHostedApi && msgToForward.Sticker.FileSize > DOWNLOAD_LIMIT {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Not able to send sticker as it exceeds Telegram size restriction",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		if msgToForward.Sticker.IsAnimated || msgToForward.Sticker.IsVideo {
			_, err := b.SendMessage(
				c.EffectiveChat.Id,
				"Animated/Video stickers are not supported at the moment",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		stickerFile, err := b.GetFile(msgToForward.Sticker.FileId, &gotgbot.GetFileOpts{})
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to retreive sticker file:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}
		stickerBytes, err := utils.TelegramDownloadFileByPath(b, stickerFile.FilePath)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to download sticker:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		uploadedSticker, err := waClient.Upload(context.Background(), stickerBytes, whatsmeow.MediaImage)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to upload sticker to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		msgToSend := &waProto.Message{
			StickerMessage: &waProto.StickerMessage{
				Url:           proto.String(uploadedSticker.URL),
				DirectPath:    proto.String(uploadedSticker.DirectPath),
				MediaKey:      uploadedSticker.MediaKey,
				IsAnimated:    proto.Bool(false),
				IsAvatar:      proto.Bool(false),
				Height:        proto.Uint32(uint32(msgToForward.Sticker.Height)),
				Width:         proto.Uint32(uint32(msgToForward.Sticker.Width)),
				Mimetype:      proto.String("image/webp"),
				FileEncSha256: uploadedSticker.FileEncSHA256,
				FileSha256:    uploadedSticker.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(stickerBytes))),
			},
		}
		if isReply {
			msgToSend.StickerMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send sticker to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	} else if msgToForward.Text != "" {

		if emojis := gomoji.CollectAll(msgToForward.Text); len(emojis) == 1 && emojis[0].Character == msgToForward.Text {
			// react with emoji instead of replying
			_, err := waClient.SendMessage(context.Background(), waChatJID, "", &waProto.Message{
				ReactionMessage: &waProto.ReactionMessage{
					Text:              proto.String(msgToForward.Text),
					SenderTimestampMs: proto.Int64(time.Now().UnixMilli()),
					Key: &waProto.MessageKey{
						RemoteJid: proto.String(waChatJID.String()),
						FromMe:    proto.Bool(msgToReplyTo != nil && msgToReplyTo.From.Id == cfg.Telegram.OwnerID),
						Id:        proto.String(stanzaId),
					},
				},
			})
			if err != nil {
				_, err = b.SendMessage(
					c.EffectiveChat.Id,
					fmt.Sprintf(
						"Failed to send the reaction:\n\n<code>%s</code>",
						html.EscapeString(err.Error()),
					),
					&gotgbot.SendMessageOpts{
						ReplyToMessageId: c.EffectiveMessage.MessageId,
					},
				)
				return err
			}

			b.SendMessage(
				c.EffectiveChat.Id,
				"Successfully reacted",
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)

			return nil
		}

		msgToSend := &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(msgToForward.Text),
			},
		}
		if isReply {
			msgToSend.ExtendedTextMessage.ContextInfo = &waProto.ContextInfo{
				StanzaId:      proto.String(stanzaId),
				Participant:   proto.String(participant),
				QuotedMessage: &waProto.Message{Conversation: proto.String("")},
			}
		}

		sentMsg, err := waClient.SendMessage(context.Background(), waChatJID, "", msgToSend)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to send the message to whatsapp:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

		b.SendMessage(
			c.EffectiveChat.Id,
			"Successfully sent",
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: c.EffectiveMessage.MessageId,
			},
		)

		err = database.AddNewWaToTgPair(
			sentMsg.ID, waClient.Store.ID.User, waChatJID.String(),
			cfg.Telegram.TargetChatID, msgToForward.MessageId,
		)
		if err != nil {
			_, err = b.SendMessage(
				c.EffectiveChat.Id,
				fmt.Sprintf(
					"Failed to add to database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{
					ReplyToMessageId: c.EffectiveMessage.MessageId,
				},
			)
			return err
		}

	}

	return nil
}
