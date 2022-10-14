package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"wa-tg-bridge/database"
	"wa-tg-bridge/state"
	"wa-tg-bridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	goVCard "github.com/emersion/go-vcard"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

func WhatsAppEventHandler(evt interface{}) {
	switch v := evt.(type) {

	case *events.CallOffer:
		NewCallOfferHandler(v)

	case *events.Message:

		if v.Info.Chat.String() == "status@broadcast" {
			return
		}

		text := ""
		if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
			text = *v.Message.ExtendedTextMessage.Text
		} else {
			text = v.Message.GetConversation()
		}

		if v.Info.IsFromMe {
			NewMessageFromMeHandler(text, v)
		} else {
			NewMessageFromOthersHandler(text, v)
		}

	}
}

func NewMessageFromMeHandler(text string, v *events.Message) {
	// Get ID of the current chat
	if text == ".id" {
		GetChatIDHandler(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String())
	}

	// Tag everyone in the group
	if v.Info.IsGroup && (strings.Contains(strings.ToLower(text), "@all") || strings.Contains(strings.ToLower(text), "@everyone")) {
		TagAllHandler(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), true)
	}
}

func NewMessageFromOthersHandler(text string, v *events.Message) {
	cfg := state.State.Config
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot

	// Tag everyone in allowed groups
	if v.Info.IsGroup && slices.Contains(cfg.WhatsApp.TagAllAllowedGroups, v.Info.Chat.User) &&
		(strings.Contains(strings.ToLower(text), "@all") ||
			strings.Contains(strings.ToLower(text), "@everyone")) {
		TagAllHandler(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), false)
	}

	if slices.Contains(cfg.WhatsApp.IgnoreChats, v.Info.Chat.User) {
		return
	}

	{
		// Return if duplicate event is emitted
		tgChatId, _ := database.GetTgFromWa(v.Info.ID, v.Info.Chat.String())
		if tgChatId == cfg.Telegram.TargetChatID {
			return
		}
	}

	bridgedText := fmt.Sprintf("🧑: <b>%s</b>\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
	if v.Info.IsGroup {
		bridgedText += fmt.Sprintf("👥: <b>%s</b>\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
	} else if v.Info.IsIncomingBroadcast() {
		bridgedText += "👥: <b>(Broadcast)</b>\n"
	} else {
		bridgedText += "👥: <b>(PVT)</b>\n"
	}
	bridgedText += fmt.Sprintf("🕛: <b>%s</b>\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))

	var replyToMsgId int64
	if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.ContextInfo != nil {
		stanzaId := v.Message.ExtendedTextMessage.ContextInfo.StanzaId
		tgChatId, tgMsgId := database.GetTgFromWa(*stanzaId, v.Info.Chat.String())
		if tgChatId == cfg.Telegram.TargetChatID {
			replyToMsgId = tgMsgId
		}
	}

	var idToSave int64
	idToSave = 0

	switch v.Info.MediaType {
	case "image":

		imgMsg := v.Message.GetImageMessage()
		caption := imgMsg.GetCaption()

		if imgMsg.GetUrl() == "" {
			return
		}

		imageBytes, err := waClient.Download(imgMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading an image : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		bridgedText += "<b>Caption:</b>\n\n"
		if len(caption) > 0 {
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:500]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		sentMsg, _ := tgBot.SendPhoto(
			cfg.Telegram.TargetChatID,
			imageBytes,
			&gotgbot.SendPhotoOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "gif":

		gifMsg := v.Message.GetVideoMessage()
		caption := gifMsg.GetCaption()

		if gifMsg.GetUrl() == "" {
			return
		}

		gifBytes, err := waClient.Download(gifMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading an gif : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		bridgedText += "<b>Caption:</b>\n\n"
		if len(caption) > 0 {
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:500]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "animation.gif",
			File:     bytes.NewReader(gifBytes),
		}

		sentMsg, _ := tgBot.SendAnimation(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAnimationOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "video":

		vidMsg := v.Message.GetVideoMessage()
		caption := vidMsg.GetCaption()

		if vidMsg.GetUrl() == "" {
			return
		}

		vidBytes, err := waClient.Download(vidMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading a video : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		if len(caption) > 0 {
			bridgedText += "<b>Caption:</b>\n\n"
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:500]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "video." + strings.Split(vidMsg.GetMimetype(), "/")[1],
			File:     bytes.NewReader(vidBytes),
		}

		sentMsg, _ := tgBot.SendVideo(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendVideoOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "ptt":
		// Voice Notes

		audioMsg := v.Message.GetAudioMessage()

		if audioMsg.GetUrl() == "" {
			return
		}

		audioBytes, err := waClient.Download(audioMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading an audio : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "audio.ogg",
			File:     bytes.NewReader(audioBytes),
		}

		sentMsg, _ := tgBot.SendAudio(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAudioOpts{
				Caption:          bridgedText,
				Duration:         int64(audioMsg.GetSeconds()),
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "audio":

		audioMsg := v.Message.GetAudioMessage()

		if audioMsg.GetUrl() == "" {
			return
		}

		audioBytes, err := waClient.Download(audioMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading an audio : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "audio.m4a",
			File:     bytes.NewReader(audioBytes),
		}

		sentMsg, _ := tgBot.SendAudio(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAudioOpts{
				Caption:          bridgedText,
				Duration:         int64(audioMsg.GetSeconds()),
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "document":
		// Any document like PDF, image, video etc

		docMsg := v.Message.GetDocumentMessage()
		caption := docMsg.GetCaption()

		if docMsg.GetUrl() == "" {
			return
		}

		docBytes, err := waClient.Download(docMsg)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error downloading a document : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		if len(caption) > 0 {
			bridgedText += "<b>Caption:</b>\n\n"
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:500]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: docMsg.GetTitle(),
			File:     bytes.NewReader(docBytes),
		}

		sentMsg, _ := tgBot.SendDocument(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendDocumentOpts{
				Caption:          bridgedText,
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "sticker":

		bridgedText += "\n<i>It was a sticker which is not supported</i>"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "vcard":
		// Contact
		contactMsg := v.Message.GetContactMessage()

		dec := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
		card, err := dec.Decode()
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Error parsing a vcard : <code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
			return
		}

		bridgedText += "\n<i>It was the following vCard</i>\n"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

		tgBot.SendContact(
			cfg.Telegram.TargetChatID,
			card.PreferredValue(goVCard.FieldTelephone),
			contactMsg.GetDisplayName(),
			&gotgbot.SendContactOpts{
				Vcard:            contactMsg.GetVcard(),
				ReplyToMessageId: sentMsg.MessageId,
			},
		)

	case "contact_array":
		// Multiple contacts
		contactsMsg := v.Message.GetContactsArrayMessage()

		bridgedText += "\n<i>It was the following vCards</i>\n"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

		for _, contactMsg := range contactsMsg.Contacts {
			dec := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
			card, err := dec.Decode()
			if err != nil {
				tgBot.SendMessage(
					cfg.Telegram.TargetChatID,
					fmt.Sprintf(
						"Error parsing a vcard : <code>%s</code>",
						html.EscapeString(err.Error()),
					),
					&gotgbot.SendMessageOpts{
						ReplyToMessageId: sentMsg.MessageId,
					},
				)
				continue
			}

			tgBot.SendContact(
				cfg.Telegram.TargetChatID,
				card.PreferredValue(goVCard.FieldTelephone),
				contactMsg.GetDisplayName(),
				&gotgbot.SendContactOpts{
					Vcard:            contactMsg.GetVcard(),
					ReplyToMessageId: sentMsg.MessageId,
				},
			)
		}

	case "location":

		locMsg := v.Message.GetLocationMessage()

		bridgedText += "\n<i>It was the following location</i>\n"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

		tgBot.SendLocation(
			cfg.Telegram.TargetChatID,
			locMsg.GetDegreesLatitude(),
			locMsg.GetDegreesLongitude(),
			&gotgbot.SendLocationOpts{
				HorizontalAccuracy: float64(locMsg.GetAccuracyInMeters()),
				ReplyToMessageId:   sentMsg.MessageId,
			},
		)

	case "livelocation":

		bridgedText += "\n<i>User shared their live location with you</i>\n"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	case "", "url":

		if text == "" {
			return
		}

		bridgedText += "<b>Body:</b>\n\n"
		if len(text) > 2000 {
			bridgedText += (html.EscapeString(text[:2000]) + "...")
		} else {
			bridgedText += html.UnescapeString(text)
		}

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyToMessageId: replyToMsgId,
			},
		)
		idToSave = sentMsg.MessageId

	default:

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			"Received an unhandled media type: "+v.Info.MediaType,
			&gotgbot.SendMessageOpts{},
		)
		idToSave = sentMsg.MessageId
	}

	if idToSave != 0 {
		err := database.AddNewWaToTgPair(
			v.Info.ID, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
			cfg.Telegram.TargetChatID, idToSave,
		)
		if err != nil {
			tgBot.SendMessage(
				cfg.Telegram.TargetChatID,
				fmt.Sprintf(
					"Failed to save bridged pair in database:\n\n<code>%s</code>",
					html.EscapeString(err.Error()),
				),
				&gotgbot.SendMessageOpts{},
			)
		}
	}
}

func GetChatIDHandler(chat types.JID, msg *waProto.Message, msgId string, msgSender string) {
	waClient := state.State.WhatsAppClient

	_, err := waClient.SendMessage(context.Background(), chat, "",
		&waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(fmt.Sprintf(
					"The ID of current chat is : ```%s```",
					utils.MarkdownEscapeString(chat.String()),
				)),
				ContextInfo: &waProto.ContextInfo{
					StanzaId:      proto.String(msgId),
					Participant:   proto.String(msgSender),
					QuotedMessage: msg,
				},
			},
		})
	if err != nil {
		log.Printf("[whatsapp] failed to reply to '.id' : %s\n", err)
	}
}

func TagAllHandler(group types.JID, msg *waProto.Message, msgId string, msgSender string, msgIsFromMe bool) {
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot
	cfg := state.State.Config

	groupInfo, err := waClient.GetGroupInfo(group)
	if err != nil {
		log.Printf("[whatsapp] failed to get group info of '%s' : %s\n", group.String(), err)
		return
	}

	replyText := ""
	mentionedParticipants := []string{}

	for _, participant := range groupInfo.Participants {
		if participant.JID.User == waClient.Store.ID.User {
			continue
		}
		replyText += fmt.Sprintf("@%s ", participant.JID.User)
		mentionedParticipants = append(mentionedParticipants, participant.JID.String())
	}

	_, err = waClient.SendMessage(context.Background(), group, "",
		&waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(replyText),
				ContextInfo: &waProto.ContextInfo{
					StanzaId:      proto.String(msgId),
					Participant:   proto.String(msgSender),
					QuotedMessage: msg,
					MentionedJid:  mentionedParticipants,
				},
			},
		},
	)
	if err != nil {
		log.Printf("[whatsapp] failed to reply to '@all' : %s\n", err)
	}

	if !msgIsFromMe {
		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			fmt.Sprintf(
				`#tagall

Everyone was mentioned in a group

👥: <i>%s</i>`,
				html.EscapeString(groupInfo.Name),
			),
			&gotgbot.SendMessageOpts{},
		)
	}
}

func NewCallOfferHandler(v *events.CallOffer) {
	tgBot := state.State.TelegramBot
	cfg := state.State.Config

	// TODO: Check and handle group calls
	callerName := utils.WhatsAppGetContactName(v.CallCreator)

	tgBot.SendMessage(
		cfg.Telegram.TargetChatID,
		fmt.Sprintf(
			`#calls

You received a new call

🧑: <i>%s</i>
🕛: <code>%s</code>`,
			html.EscapeString(callerName),
			html.EscapeString(v.Timestamp.Local().Format(cfg.TimeFormat)),
		),
		&gotgbot.SendMessageOpts{},
	)
}
