package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	goVCard "github.com/emersion/go-vcard"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

func WhatsAppEventHandler(evt interface{}) {

	switch v := evt.(type) {

	case *events.PushName:
		PushNameEventHandler(v)

	case *events.CallOffer:
		CallOfferEventHandler(v)

	case *events.Message:
		text := ""
		if extendedMessageText := v.Message.GetExtendedTextMessage().GetText(); extendedMessageText != "" {
			text = extendedMessageText
		} else {
			text = v.Message.GetConversation()
		}

		if v.Info.IsFromMe {
			MessageFromMeEventHandler(text, v)
		} else {
			MessageFromOthersEventHandler(text, v)
		}
	}
}

func MessageFromMeEventHandler(text string, v *events.Message) {
	// Get ID of the current chat
	if text == ".id" {
		waClient := state.State.WhatsAppClient

		_, err := waClient.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(fmt.Sprintf("The ID of the current chat is:\n\n```%s```", v.Info.Chat.String())),
				ContextInfo: &waProto.ContextInfo{
					StanzaId:      proto.String(v.Info.ID),
					Participant:   proto.String(v.Info.MessageSource.Sender.String()),
					QuotedMessage: v.Message,
				},
			},
		})
		if err != nil {
			log.Printf("[whatsapp] failed to reply to '.id' command : %s\n", err)
		}
	}

	// Tag everyone in the group
	if lowercaseText := strings.ToLower(text); v.Info.IsGroup &&
		(strings.Contains(lowercaseText, "@all") || strings.Contains(lowercaseText, "@everyone")) {
		utils.WaTagAll(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), true)
	}
}

func MessageFromOthersEventHandler(text string, v *events.Message) {
	var (
		cfg      = state.State.Config
		tgBot    = state.State.TelegramBot
		waClient = state.State.WhatsAppClient
	)

	{
		// Return if duplicate event is emitted
		tgChatId, _, _, _ := database.MsgIdGetTgFromWa(v.Info.ID, v.Info.Chat.String())
		if tgChatId == cfg.Telegram.TargetChatID {
			return
		}
	}

	{
		// Return if status is from ignored chat
		if v.Info.IsIncomingBroadcast() && v.Info.Chat.User == "status" &&
			slices.Contains(cfg.WhatsApp.StatusIgnoredChats, v.Info.MessageSource.Sender.User) {
			return
		}
	}

	if lowercaseText := strings.ToLower(text); v.Info.IsGroup && slices.Contains(cfg.WhatsApp.TagAllAllowedGroups, v.Info.Chat.User) &&
		(strings.Contains(lowercaseText, "@all") || strings.Contains(lowercaseText, "@everyone")) {
		utils.WaTagAll(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), false)
	}

	bridgedText := fmt.Sprintf("🧑: <b>%s</b>\n", html.EscapeString(utils.WaGetContactName(v.Info.Sender)))
	if v.Info.IsIncomingBroadcast() {
		bridgedText += "👥: <b>(Broadcast)</b>\n"
	} else if v.Info.IsGroup {
		bridgedText += fmt.Sprintf("👥: <b>%s</b>\n", html.EscapeString(utils.WaGetGroupName(v.Info.Chat)))
	} else {
		bridgedText += "👥: <b>(PVT)</b>\n"
	}
	bridgedText += fmt.Sprintf("🕛: <b>%s</b>\n",
		html.EscapeString(v.Info.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))

	if mentioned := v.Message.GetExtendedTextMessage().GetContextInfo().GetMentionedJid(); v.Info.IsGroup && mentioned != nil {
		for _, jid := range mentioned {
			parsedJid, _ := utils.WaParseJID(jid)
			if parsedJid.User == waClient.Store.ID.User {

				tagInfoText := "#mentions\n\n" + bridgedText + fmt.Sprintf("\n<i>You were tagged in %s</i>",
					html.EscapeString(utils.WaGetGroupName(v.Info.Chat)))

				threadId, err := utils.TgGetOrMakeThreadFromWa("status@broadcast", cfg.Telegram.TargetChatID, "Status/Calls/Tags [ status@broadcast ]")
				if err != nil {
					utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, "failed to create/find thread id for 'status@broadcast'", err)
				} else {
					tgBot.SendMessage(cfg.Telegram.TargetChatID, tagInfoText, &gotgbot.SendMessageOpts{
						MessageThreadId: threadId,
					})
				}

				break
			}
		}
	}

	var (
		replyToMsgId  int64
		threadId      int64
		threadIdFound bool
	)

	if contextInfo := v.Message.GetExtendedTextMessage().GetContextInfo(); contextInfo != nil {
		stanzaId := contextInfo.GetStanzaId()
		tgChatId, tgThreadId, tgMsgId, err := database.MsgIdGetTgFromWa(stanzaId, v.Info.Chat.String())
		if err == nil && tgChatId == cfg.Telegram.TargetChatID {
			replyToMsgId = tgMsgId
			threadId = tgThreadId
			threadIdFound = true
		}
	}
	if !threadIdFound {
		var err error
		if v.Info.IsIncomingBroadcast() {
			threadId, err = utils.TgGetOrMakeThreadFromWa(v.Info.MessageSource.Sender.ToNonAD().String(), cfg.Telegram.TargetChatID,
				utils.WaGetContactName(v.Info.MessageSource.Sender))
			if err != nil {
				utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, fmt.Sprintf("failed to create/find thread id for '%s'",
					v.Info.MessageSource.Sender.ToNonAD().String()), err)
				return
			}
		} else if v.Info.IsGroup {
			threadId, err = utils.TgGetOrMakeThreadFromWa(v.Info.Chat.String(), cfg.Telegram.TargetChatID,
				utils.WaGetGroupName(v.Info.Chat))
			if err != nil {
				utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, fmt.Sprintf("failed to create/find thread id for '%s'",
					v.Info.Chat.String()), err)
				return
			}
		} else {
			threadId, err = utils.TgGetOrMakeThreadFromWa(v.Info.MessageSource.Sender.ToNonAD().String(), cfg.Telegram.TargetChatID,
				utils.WaGetContactName(v.Info.MessageSource.Sender))
			if err != nil {
				utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, fmt.Sprintf("failed to create/find thread id for '%s'",
					v.Info.MessageSource.Sender.ToNonAD().String()), err)
				return
			}
		}
	}

MEDIATYPESWITCH:
	switch v.Info.MediaType {

	case "image":
		imageMsg := v.Message.GetImageMessage()
		if imageMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && imageMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the photo as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			imageBytes, err := waClient.Download(imageMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the photo due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			if caption := imageMsg.GetCaption(); caption != "" {
				bridgedText += "<b>Caption:</b>\n\n"
				if len(caption) > 500 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 500)) + "..."
				} else {
					bridgedText += html.EscapeString(caption)
				}
			}

			sentMsg, _ := tgBot.SendPhoto(cfg.Telegram.TargetChatID, imageBytes, &gotgbot.SendPhotoOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "gif":
		gifMsg := v.Message.GetVideoMessage()
		if gifMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && gifMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the GIF as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			gifBytes, err := waClient.Download(gifMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the GIF due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			if caption := gifMsg.GetCaption(); caption != "" {
				bridgedText += "<b>Caption:</b>\n\n"
				if len(caption) > 500 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 500)) + "..."
				} else {
					bridgedText += html.EscapeString(caption)
				}
			}

			fileToSend := gotgbot.NamedFile{
				FileName: "animation.gif",
				File:     bytes.NewReader(gifBytes),
			}

			sentMsg, _ := tgBot.SendAnimation(cfg.Telegram.TargetChatID, fileToSend, &gotgbot.SendAnimationOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "video":
		videoMsg := v.Message.GetVideoMessage()
		if videoMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && videoMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the video as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			videoBytes, err := waClient.Download(videoMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the video due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			if caption := videoMsg.GetCaption(); caption != "" {
				bridgedText += "<b>Caption:</b>\n\n"
				if len(caption) > 500 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 500)) + "..."
				} else {
					bridgedText += html.EscapeString(caption)
				}
			}

			fileToSend := gotgbot.NamedFile{
				FileName: "video." + strings.Split(videoMsg.GetMimetype(), "/")[1],
				File:     bytes.NewReader(videoBytes),
			}

			sentMsg, _ := tgBot.SendVideo(cfg.Telegram.TargetChatID, fileToSend, &gotgbot.SendVideoOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "ptt":
		audioMsg := v.Message.GetAudioMessage()
		if audioMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the audio as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			audioBytes, err := waClient.Download(audioMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the audio due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			fileToSend := gotgbot.NamedFile{
				FileName: "audio.ogg",
				File:     bytes.NewReader(audioBytes),
			}

			sentMsg, _ := tgBot.SendAudio(cfg.Telegram.TargetChatID, fileToSend, &gotgbot.SendAudioOpts{
				Caption:          bridgedText,
				Duration:         int64(audioMsg.GetSeconds()),
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "audio":
		audioMsg := v.Message.GetAudioMessage()
		if audioMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the audio as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			audioBytes, err := waClient.Download(audioMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the audio due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			fileToSend := gotgbot.NamedFile{
				FileName: "audio.m4a",
				File:     bytes.NewReader(audioBytes),
			}

			sentMsg, _ := tgBot.SendAudio(cfg.Telegram.TargetChatID, fileToSend, &gotgbot.SendAudioOpts{
				Caption:          bridgedText,
				Duration:         int64(audioMsg.GetSeconds()),
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "document":
		documentMsg := v.Message.GetDocumentMessage()
		if documentMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && documentMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the document as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			documentBytes, err := waClient.Download(documentMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the document due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			if caption := documentMsg.GetCaption(); caption != "" {
				bridgedText += "<b>Caption:</b>\n\n"
				if len(caption) > 500 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 500)) + "..."
				} else {
					bridgedText += html.EscapeString(caption)
				}
			}

			fileToSend := gotgbot.NamedFile{
				FileName: documentMsg.GetFileName(),
				File:     bytes.NewReader(documentBytes),
			}

			sentMsg, _ := tgBot.SendDocument(cfg.Telegram.TargetChatID, fileToSend, &gotgbot.SendDocumentOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

	case "sticker":
		stickerMsg := v.Message.GetStickerMessage()
		if stickerMsg.GetUrl() == "" {
			return
		}

		if !cfg.Telegram.SelfHostedAPI && stickerMsg.GetFileLength() > utils.UploadSizeLimit {
			bridgedText += "\n<i>Couldn't send the sticker as it exceeds Telegram size restrictions.</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else {
			stickerBytes, err := waClient.Download(stickerMsg)
			if err != nil {
				bridgedText += "\n<i>Couldn't download the sticker due to some errors</i>"
				sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
					ReplyToMessageId: replyToMsgId,
					MessageThreadId:  threadId,
				})
				if sentMsg.MessageId != 0 {
					database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
						cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
				}
				return
			}

			if stickerMsg.GetIsAnimated() {
				bridgedText += "\n<i>It was an animated sticker, here is the first frame</i>"
			} else {
				bridgedText += "\n<i>It was the following sticker</i>"
			}
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			tgBot.SendSticker(cfg.Telegram.TargetChatID, stickerBytes, &gotgbot.SendStickerOpts{
				ReplyToMessageId: sentMsg.MessageId,
				MessageThreadId:  threadId,
			})
		}

	case "vcard":
		contactMsg := v.Message.GetContactMessage()

		decoder := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
		card, err := decoder.Decode()
		if err != nil {
			bridgedText += "\n<i>Couldn't send the vCard as failed to parse it</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		}

		bridgedText += "\n<i>It was the following vCard</i>"
		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}
		tgBot.SendContact(cfg.Telegram.TargetChatID, card.PreferredValue(goVCard.FieldTelephone), contactMsg.GetDisplayName(),
			&gotgbot.SendContactOpts{
				Vcard:            contactMsg.GetVcard(),
				ReplyToMessageId: sentMsg.MessageId,
				MessageThreadId:  threadId,
			})
		return

	case "contact_array":
		contactsMsg := v.Message.GetContactsArrayMessage()

		bridgedText += "\n<i>It was the following vCards</i>"

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}

		for _, contactMsg := range contactsMsg.Contacts {
			decoder := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
			card, err := decoder.Decode()
			if err != nil {
				tgBot.SendMessage(cfg.Telegram.TargetChatID, "Couldn't send the vCard as failed to parse it",
					&gotgbot.SendMessageOpts{
						ReplyToMessageId: replyToMsgId,
						MessageThreadId:  threadId,
					})
				continue
			}

			tgBot.SendContact(cfg.Telegram.TargetChatID, card.PreferredValue(goVCard.FieldTelephone), contactMsg.GetDisplayName(),
				&gotgbot.SendContactOpts{
					Vcard:            contactMsg.GetVcard(),
					ReplyToMessageId: sentMsg.MessageId,
					MessageThreadId:  threadId,
				})
		}
		return

	case "location":
		locationMsg := v.Message.GetLocationMessage()

		bridgedText += "\n<i>It was the following location</i>"

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}

		tgBot.SendLocation(cfg.Telegram.TargetChatID, locationMsg.GetDegreesLatitude(), locationMsg.GetDegreesLongitude(),
			&gotgbot.SendLocationOpts{
				HorizontalAccuracy: float64(locationMsg.GetAccuracyInMeters()),
				ReplyToMessageId:   sentMsg.MessageId,
				MessageThreadId:    threadId,
			})
		return

	case "livelocation":
		bridgedText += "\n<i>Shared their live location with you</i>"

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}
		return

	case "url", "":
		if text == "" {
			if v.Message.GetImageMessage() != nil {
				v.Info.MediaType = "image"
			} else if videoMsg := v.Message.GetVideoMessage(); videoMsg != nil {
				if videoMsg.GetGifPlayback() {
					v.Info.MediaType = "gif"
				} else {
					v.Info.MediaType = "video"
				}
			} else if audioMsg := v.Message.GetAudioMessage(); audioMsg != nil {
				if audioMsg.GetPtt() {
					v.Info.MediaType = "ptt"
				} else {
					v.Info.MediaType = "audio"
				}
			} else if v.Message.GetDocumentMessage() != nil {
				v.Info.MediaType = "document"
			} else if v.Message.GetStickerMessage() != nil {
				v.Info.MediaType = "sticker"
			} else if v.Message.GetContactMessage() != nil {
				v.Info.MediaType = "vcard"
			} else if v.Message.GetContactsArrayMessage() != nil {
				v.Info.MediaType = "contact_array"
			} else if v.Message.GetLocationMessage() != nil {
				v.Info.MediaType = "location"
			} else if v.Message.GetLiveLocationMessage() != nil {
				v.Info.MediaType = "livelocation"
			}

			if v.Info.MediaType != "" {
				goto MEDIATYPESWITCH
			}
			return
		}

		if mentioned := v.Message.GetExtendedTextMessage().GetContextInfo().GetMentionedJid(); mentioned != nil {
			for _, jid := range mentioned {
				parsedJid, _ := utils.WaParseJID(jid)
				name := utils.WaGetContactName(parsedJid)
				text = strings.ReplaceAll(text, "@"+parsedJid.User, "@("+html.EscapeString(name)+")")
			}
		}

		bridgedText += "<b>Body:</b>\n\n"
		if len(text) > 2000 {
			bridgedText += html.EscapeString(utils.SubString(text, 0, 2000)) + "..."
		} else {
			bridgedText += html.EscapeString(text)
		}

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}
		return

	default:
		bridgedText += "\n<i>It was an unhandled MediaType. Please report to devs.</i>"

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}
		return

	}
}

func CallOfferEventHandler(v *events.CallOffer) {
	var (
		cfg   = state.State.Config
		tgBot = state.State.TelegramBot
	)

	// TODO : Check and handle group calls
	callerName := utils.WaGetContactName(v.CallCreator)

	callThreadId, err := utils.TgGetOrMakeThreadFromWa("status@broadcast", cfg.Telegram.TargetChatID, "Status/Calls/Tags [ status@broadcast ]")
	if err != nil {
		utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, "Failed to create/retreive corresponding thread id for status/calls/tags", err)
		return
	}

	bridgeText := fmt.Sprintf("#calls\n\n🧑: <b>%s</b>\n🕛: <b>%s</b>\n\n<i>You received a new call</i>",
		html.EscapeString(callerName), html.EscapeString(v.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))

	utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, callThreadId, bridgeText)
}

func PushNameEventHandler(v *events.PushName) {
	database.ContactUpdatePushName(v.JID.User, v.NewPushName)
}
