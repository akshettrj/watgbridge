package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"reflect"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	goVCard "github.com/emersion/go-vcard"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

func WhatsAppEventHandler(evt interface{}) {

	var (
		cfg    = state.State.Config
		logger = state.State.Logger
	)
	defer logger.Sync()

	switch v := evt.(type) {

	case *events.PushName:
		PushNameEventHandler(v)

	case *events.CallOffer:
		CallOfferEventHandler(v)

	case *events.Message:

		logger.Debug("new Message event",
			zap.String("event_id", v.Info.ID),
		)

		if v.Info.Timestamp.UTC().Before(state.State.StartTime) {
			// Old events
			logger.Debug("returning due to message being older than bot start time",
				zap.String("event_id", v.Info.ID),
				zap.String("message_timestamp",
					v.Info.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)),
				zap.String("bot_start_timestamp",
					state.State.StartTime.In(state.State.LocalLocation).Format(cfg.TimeFormat)),
				zap.String("chat_jid", v.Info.Chat.String()),
				zap.String("sender_jid", v.Info.MessageSource.Sender.String()),
			)
			return
		}

		if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil &&
			protoMsg.GetType() == waProto.ProtocolMessage_REVOKE {
			logger.Debug("new revoked message",
				zap.String("event_id", v.Info.ID),
			)
			RevokedMessageEventHandler(v)
			return
		}

		text := ""
		if extendedMessageText := v.Message.GetExtendedTextMessage().GetText(); extendedMessageText != "" {
			text = extendedMessageText
			logger.Debug("took text from ExtendedTextMessage",
				zap.String("event_id", v.Info.ID),
				zap.String("text", text),
			)
		} else {
			text = v.Message.GetConversation()
			logger.Debug("took text from Conversation",
				zap.String("event_id", v.Info.ID),
				zap.String("text", text),
			)
		}

		if v.Info.IsFromMe {
			logger.Debug("new message from your account",
				zap.String("event_id", v.Info.ID),
			)
			MessageFromMeEventHandler(text, v)
		} else {
			logger.Debug("new message from others",
				zap.String("event_id", v.Info.ID),
			)
			MessageFromOthersEventHandler(text, v)
		}

	default:
		logger.Debug("new unhandled whatsapp event type",
			zap.Any("event_type", reflect.TypeOf(evt)),
		)
	}

}

func MessageFromMeEventHandler(text string, v *events.Message) {
	logger := state.State.Logger
	defer logger.Sync()

	// Get ID of the current chat
	if text == ".id" {
		logger.Debug("identified .id command",
			zap.String("event_id", v.Info.ID),
		)
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
			logger.Error("failed to reply to .id command",
				zap.String("event_id", v.Info.ID),
				zap.Error(err),
			)
		}
	}

	// Tag everyone in the group
	textSplit := strings.Fields(strings.ToLower(text))
	if v.Info.IsGroup &&
		(slices.Contains(textSplit, "@all") || slices.Contains(textSplit, "@everyone")) {
		logger.Debug("identified usage of @all/@everyone in an allowed group",
			zap.String("event_id", v.Info.ID),
			zap.String("group_jid", v.Info.Chat.String()),
		)
		utils.WaTagAll(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), true)
	}

	if state.State.Config.WhatsApp.SendMyMessagesFromOtherDevices {
		MessageFromOthersEventHandler(text, v)
	}
}

func MessageFromOthersEventHandler(text string, v *events.Message) {
	var (
		cfg      = state.State.Config
		logger   = state.State.Logger
		tgBot    = state.State.TelegramBot
		waClient = state.State.WhatsAppClient
	)
	defer logger.Sync()

	{
		// Return if duplicate event is emitted
		tgChatId, _, _, _ := database.MsgIdGetTgFromWa(v.Info.ID, v.Info.Chat.String())
		if tgChatId == cfg.Telegram.TargetChatID {
			logger.Debug("returning because duplicate event id emitted",
				zap.String("event_id", v.Info.ID),
				zap.String("chat_jid", v.Info.Chat.String()),
			)
			return
		}
	}

	if !v.Info.IsFromMe {
		// Return if status is from ignored chat
		if v.Info.Chat.String() == "status@broadcast" && slices.Contains(cfg.WhatsApp.StatusIgnoredChats, v.Info.MessageSource.Sender.User) {
			logger.Debug("returning because status from a ignored chat",
				zap.String("event_id", v.Info.ID),
				zap.String("chat_jid", v.Info.Chat.String()),
			)
			return
		} else if slices.Contains(cfg.WhatsApp.IgnoreChats, v.Info.Chat.User) {
			logger.Debug("returning because message from an ignored chat",
				zap.String("event_id", v.Info.ID),
				zap.String("chat_jid", v.Info.Chat.String()),
			)
			return
		}
	}

	if lowercaseText := strings.ToLower(text); !v.Info.IsFromMe && v.Info.IsGroup && slices.Contains(cfg.WhatsApp.TagAllAllowedGroups, v.Info.Chat.User) &&
		(strings.Contains(lowercaseText, "@all") || strings.Contains(lowercaseText, "@everyone")) {
		logger.Debug("usage of @all/@everyone command from your account",
			zap.String("event_id", v.Info.ID),
			zap.String("chat_jid", v.Info.Chat.String()),
		)
		utils.WaTagAll(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), false)
	}

	var bridgedText string
	if cfg.WhatsApp.SkipChatDetails {
		logger.Debug("skipping to add chat details as configured",
			zap.String("event_id", v.Info.ID),
		)
		if v.Info.IsIncomingBroadcast() {
			bridgedText += "👥: <b>(Broadcast)</b>\n"
		} else if v.Info.IsGroup {
			if v.Info.IsFromMe {
				bridgedText += fmt.Sprintf("🧑: <b>You [other device]</b>\n")
			} else {
				bridgedText += fmt.Sprintf("🧑: <b>%s</b>\n", html.EscapeString(utils.WaGetContactName(v.Info.MessageSource.Sender)))
			}
		} else if v.Info.IsFromMe {
			bridgedText += fmt.Sprintf("🧑: <b>You [other device]</b>\n")
		}

	} else {

		if v.Info.IsFromMe {
			bridgedText += fmt.Sprintf("🧑: <b>You [other device]</b>\n")
		} else {
			bridgedText += fmt.Sprintf("🧑: <b>%s</b>\n", html.EscapeString(utils.WaGetContactName(v.Info.MessageSource.Sender)))
		}
		if v.Info.IsIncomingBroadcast() {
			bridgedText += "👥: <b>(Broadcast)</b>\n"
		} else if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("👥: <b>%s</b>\n", html.EscapeString(utils.WaGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "👥: <b>(PVT)</b>\n"
		}

	}

	if time.Since(v.Info.Timestamp).Seconds() > 60 {
		bridgedText += fmt.Sprintf("🕛: <b>%s</b>\n",
			html.EscapeString(v.Info.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))
	}

	var (
		replyToMsgId  int64
		threadId      int64
		threadIdFound bool
	)

	logger.Debug("trying to retrieve context info from Message",
		zap.String("event_id", v.Info.ID),
	)
	var contextInfo *waProto.ContextInfo = nil
	if v.Message.GetExtendedTextMessage().GetContextInfo() != nil {
		logger.Debug("taking context info from ExtendedTextMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetExtendedTextMessage().GetContextInfo()
	} else if v.Message.GetImageMessage() != nil {
		logger.Debug("taking context info from ImageMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetImageMessage().GetContextInfo()
	} else if v.Message.GetVideoMessage() != nil {
		logger.Debug("taking context info from VideoMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetVideoMessage().GetContextInfo()
	} else if v.Message.GetAudioMessage() != nil {
		logger.Debug("taking context info from AudioMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetAudioMessage().GetContextInfo()
	} else if v.Message.GetDocumentMessage() != nil {
		logger.Debug("taking context info from DocumentMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetDocumentMessage().GetContextInfo()
	} else if v.Message.GetStickerMessage() != nil {
		logger.Debug("taking context info from StickerMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetStickerMessage().GetContextInfo()
	} else if v.Message.GetContactMessage() != nil {
		logger.Debug("taking context info from ContactMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetContactMessage().GetContextInfo()
	} else if v.Message.GetContactsArrayMessage() != nil {
		logger.Debug("taking context info from ContactsArrayMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetContactsArrayMessage().GetContextInfo()
	} else if v.Message.GetLocationMessage() != nil {
		logger.Debug("taking context info from LocationMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetLocationMessage().GetContextInfo()
	} else if v.Message.GetLiveLocationMessage() != nil {
		logger.Debug("taking context info from LiveLocationMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetLiveLocationMessage().GetContextInfo()
	} else if v.Message.GetPollCreationMessage() != nil {
		logger.Debug("taking context info from PollCreationMessage",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetPollCreationMessage().GetContextInfo()
	} else if v.Message.GetPollCreationMessageV2() != nil {
		logger.Debug("taking context info from PollCreationMessageV2",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetPollCreationMessageV2().GetContextInfo()
	} else if v.Message.GetPollCreationMessageV3() != nil {
		logger.Debug("taking context info from PollCreationMessageV3",
			zap.String("event_id", v.Info.ID),
		)
		contextInfo = v.Message.GetPollCreationMessageV3().GetContextInfo()
	} else {
		logger.Debug("no context info found in any kind of messages",
			zap.String("event_id", v.Info.ID),
		)
	}

	if contextInfo != nil {

		if contextInfo.GetIsForwarded() {
			bridgedText += fmt.Sprintf("⏩: Forwarded %v times\n", contextInfo.GetForwardingScore())
		}

		// Telegram will automatically trim the string
		bridgedText += "\n"

		logger.Debug("checking if your account is mentioned in the message",
			zap.String("event_id", v.Info.ID),
		)
		if mentioned := contextInfo.GetMentionedJid(); v.Info.IsGroup && mentioned != nil {
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

		logger.Debug("trying to retrieve mapped Message in Telegram",
			zap.String("event_id", v.Info.ID),
		)
		stanzaId := contextInfo.GetStanzaId()
		tgChatId, tgThreadId, tgMsgId, err := database.MsgIdGetTgFromWa(stanzaId, v.Info.Chat.String())
		if err == nil && tgChatId == cfg.Telegram.TargetChatID {
			replyToMsgId = tgMsgId
			threadId = tgThreadId
			threadIdFound = true
		}
	} else {
		// Telegram will automatically trim the string
		bridgedText += "\n"
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
			var target_chat_jid waTypes.JID
			if v.Info.IsFromMe {
				target_chat_jid = v.Info.Chat
			} else {
				target_chat_jid = v.Info.Chat
			}

			threadId, err = utils.TgGetOrMakeThreadFromWa(target_chat_jid.ToNonAD().String(), cfg.Telegram.TargetChatID,
				utils.WaGetContactName(target_chat_jid))
			if err != nil {
				utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, fmt.Sprintf("failed to create/find thread id for '%s'",
					target_chat_jid.ToNonAD().String()), err)
				return
			}
		}
	}

	if v.Message.GetImageMessage() != nil {

		imageMsg := v.Message.GetImageMessage()
		if imageMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipImages {
			bridgedText += "\n<i>Skipping image because 'skip_images' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && imageMsg.GetFileLength() > utils.UploadSizeLimit {
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
				if len(caption) > 1020 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
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

	} else if v.Message.GetVideoMessage() != nil && v.Message.GetVideoMessage().GetGifPlayback() {

		gifMsg := v.Message.GetVideoMessage()
		if gifMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipGIFs {
			bridgedText += "\n<i>Skipping GIF because 'skip_gifs' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && gifMsg.GetFileLength() > utils.UploadSizeLimit {
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
				if len(caption) > 1020 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
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

	} else if v.Message.GetVideoMessage() != nil {

		videoMsg := v.Message.GetVideoMessage()
		if videoMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipVideos {
			bridgedText += "\n<i>Skipping video because 'skip_videos' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && videoMsg.GetFileLength() > utils.UploadSizeLimit {
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
				if len(caption) > 1020 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
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

	} else if v.Message.GetAudioMessage() != nil && v.Message.GetAudioMessage().GetPtt() {

		audioMsg := v.Message.GetAudioMessage()
		if audioMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipVoiceNotes {
			bridgedText += "\n<i>Skipping voice note because 'skip_voice_notes' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
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

	} else if v.Message.GetAudioMessage() != nil {

		audioMsg := v.Message.GetAudioMessage()
		if audioMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipAudios {
			bridgedText += "\n<i>Skipping audio because 'skip_audios' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
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

	} else if v.Message.GetDocumentMessage() != nil {

		documentMsg := v.Message.GetDocumentMessage()
		if documentMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipDocuments {
			bridgedText += "\n<i>Skipping document because 'skip_documents' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && documentMsg.GetFileLength() > utils.UploadSizeLimit {
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
				if len(caption) > 1020 {
					bridgedText += html.EscapeString(utils.SubString(caption, 0, 1020)) + "..."
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

	} else if v.Message.GetStickerMessage() != nil {

		stickerMsg := v.Message.GetStickerMessage()
		if stickerMsg.GetUrl() == "" {
			return
		}

		if cfg.WhatsApp.SkipStickers {
			bridgedText += "\n<i>Skipping sticker because 'skip_stickers' set in config file</i>"
			sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
				MessageThreadId:  threadId,
			})
			if sentMsg.MessageId != 0 {
				database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
					cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
			}
			return
		} else if !cfg.Telegram.SelfHostedAPI && stickerMsg.GetFileLength() > utils.UploadSizeLimit {
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

			if stickerMsg.GetIsAnimated() || stickerMsg.GetIsAvatar() {
				gifBytes, err := utils.AnimatedWebpConvertToGif(stickerBytes, v.Info.ID)
				if err != nil {
					bridgedText += "\n<i>It was an animated sticker, here is the first frame</i>"
					goto WEBP_TO_GIF_FAILED
				}
				bridgedText += "<i>It was an animated sticker, here it is converted to GIF</i>"

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

			} else {
				bridgedText += "\n<i>It was the following sticker</i>"
			}

		WEBP_TO_GIF_FAILED:
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

	} else if v.Message.GetContactMessage() != nil {

		contactMsg := v.Message.GetContactMessage()

		if cfg.WhatsApp.SkipContacts {
			bridgedText += "\n<i>Skipping contact because 'skip_contacts' set in config file</i>"
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

	} else if v.Message.GetContactsArrayMessage() != nil {

		contactsMsg := v.Message.GetContactsArrayMessage()

		if cfg.WhatsApp.SkipContacts {
			bridgedText += "\n<i>Skipping contact array because 'skip_contacts' set in config file</i>"
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

	} else if v.Message.GetLocationMessage() != nil {

		locationMsg := v.Message.GetLocationMessage()

		if cfg.WhatsApp.SkipLocations {
			bridgedText += "\n<i>Skipping location because 'skip_locations' set in config file</i>"
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

	} else if v.Message.GetLiveLocationMessage() != nil {

		bridgedText += "\n<i>Shared their live location with you</i>"

		if cfg.WhatsApp.SkipLocations {
			bridgedText += "\n<i>Skipping live location because 'skip_locations' set in config file</i>"
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

		sentMsg, _ := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText, &gotgbot.SendMessageOpts{
			ReplyToMessageId: replyToMsgId,
			MessageThreadId:  threadId,
		})
		if sentMsg.MessageId != 0 {
			database.MsgIdAddNewPair(v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
				cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
		}
		return

	} else if v.Message.GetPollCreationMessage() != nil || v.Message.GetPollCreationMessageV2() != nil || v.Message.GetPollCreationMessageV3() != nil {

		var pollMsg *waProto.PollCreationMessage
		if i := v.Message.GetPollCreationMessage(); i != nil {
			pollMsg = i
		} else if i := v.Message.GetPollCreationMessageV2(); i != nil {
			pollMsg = i
		} else if i := v.Message.GetPollCreationMessageV3(); i != nil {
			pollMsg = i
		}

		bridgedText += "\n<i>It was the following poll:</i>\n\n"
		bridgedText += fmt.Sprintf("<b>%s</b>: (%v options selectable)\n\n",
			html.EscapeString(pollMsg.GetName()), pollMsg.GetSelectableOptionsCount())
		for optionNum, option := range pollMsg.GetOptions() {
			if len(bridgedText) > 4000 {
				bridgedText += "\n... <i>Plus some other options</i>"
				break
			}
			bridgedText += fmt.Sprintf("%v. %s\n", optionNum+1, html.EscapeString(option.GetOptionName()))
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

	} else {

		if text == "" {
			return
		}

		if len(text) > 4000 {
			bridgedText += html.EscapeString(utils.SubString(text, 0, 4000)) + "..."
		} else {
			bridgedText += html.EscapeString(text)
		}

		if mentioned := v.Message.GetExtendedTextMessage().GetContextInfo().GetMentionedJid(); mentioned != nil {
			for _, jid := range mentioned {
				parsedJid, _ := utils.WaParseJID(jid)
				name := utils.WaGetContactName(parsedJid)
				// text = strings.ReplaceAll(text, "@"+parsedJid.User, "@("+html.EscapeString(name)+")")
				bridgedText = strings.ReplaceAll(
					bridgedText, "@"+parsedJid.User,
					fmt.Sprintf(
						"<a href=\"https://wa.me/%s?chat_id=%s\">@%s</a>",
						parsedJid.User, v.Info.Chat.String(), html.EscapeString(name),
					),
				)
			}
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
	logger := state.State.Logger
	defer logger.Sync()

	logger.Debug("new push_name update",
		zap.String("jid", v.JID.String()),
		zap.String("old_push_name", v.OldPushName),
		zap.String("new_push_name", v.NewPushName),
	)

	database.ContactUpdatePushName(v.JID.User, v.NewPushName)
}

func RevokedMessageEventHandler(v *events.Message) {
	var (
		cfg         = state.State.Config
		tgBot       = state.State.TelegramBot
		protocolMsg = v.Message.GetProtocolMessage()
		waMsgId     = protocolMsg.GetKey().GetId()
		waChatId    = v.Info.Chat.String()
	)

	if !cfg.WhatsApp.SendRevokedMessageUpdates {
		return
	}

	deleter := v.Info.MessageSource.Sender

	var deleterName string
	if v.Info.IsFromMe {
		deleterName = "you"
	} else {
		deleterName = utils.WaGetContactName(deleter)
	}

	tgChatId, tgThreadId, tgMsgId, err := database.MsgIdGetTgFromWa(waMsgId, waChatId)
	if err != nil || tgChatId == 0 || tgThreadId == 0 || tgMsgId == 0 {
		return
	}

	tgBot.SendMessage(tgChatId, fmt.Sprintf(
		"<i>This message was revoked by %s</i>",
		html.EscapeString(deleterName),
	), &gotgbot.SendMessageOpts{
		MessageThreadId:  tgThreadId,
		ReplyToMessageId: tgMsgId,
	})
}
