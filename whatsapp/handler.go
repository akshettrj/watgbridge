package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"strings"

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

	switch v.Info.MediaType {
	case "image":

		imgMsg := v.Message.GetImageMessage()
		caption := imgMsg.GetCaption()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		bridgedText += "<b>Caption:</b>\n"
		if len(caption) > 0 {
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:2000]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		tgBot.SendPhoto(
			cfg.Telegram.TargetChatID,
			imageBytes,
			&gotgbot.SendPhotoOpts{
				Caption: bridgedText,
			},
		)

	case "gif":

		gifMsg := v.Message.GetVideoMessage()
		caption := gifMsg.GetCaption()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else if v.Info.IsIncomingBroadcast() {
			bridgedText += "<b>Chat:</b> (Broadcast)\n"
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		bridgedText += "<b>Caption:</b>\n"
		if len(caption) > 0 {
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:2000]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "animation.gif",
			File:     bytes.NewReader(gifBytes),
		}

		tgBot.SendAnimation(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAnimationOpts{
				Caption: bridgedText,
			},
		)

	case "video":

		vidMsg := v.Message.GetVideoMessage()
		caption := vidMsg.GetCaption()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		if len(caption) > 0 {
			bridgedText += "<b>Caption:</b>\n"
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:2000]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: "video." + strings.Split(vidMsg.GetMimetype(), "/")[1],
			File:     bytes.NewReader(vidBytes),
		}

		tgBot.SendVideo(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendVideoOpts{
				Caption: bridgedText,
			},
		)

	case "ptt":
		// Voice Notes

		audioMsg := v.Message.GetAudioMessage()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))

		fileToSend := gotgbot.NamedFile{
			FileName: "audio.ogg",
			File:     bytes.NewReader(audioBytes),
		}

		tgBot.SendAudio(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAudioOpts{
				Caption:  bridgedText,
				Duration: int64(audioMsg.GetSeconds()),
			},
		)

	case "audio":

		audioMsg := v.Message.GetAudioMessage()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))

		fileToSend := gotgbot.NamedFile{
			FileName: "audio.m4a",
			File:     bytes.NewReader(audioBytes),
		}

		tgBot.SendAudio(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendAudioOpts{
				Caption:  bridgedText,
				Duration: int64(audioMsg.GetSeconds()),
			},
		)

	case "document":
		// Any document like PDF, image, video etc

		docMsg := v.Message.GetDocumentMessage()
		caption := docMsg.GetCaption()

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		if len(caption) > 0 {
			bridgedText += "<b>Caption:</b>\n"
			if len(caption) > 500 {
				bridgedText += (html.EscapeString(caption[:2000]) + "...")
			} else {
				bridgedText += html.EscapeString(caption)
			}
		}

		fileToSend := gotgbot.NamedFile{
			FileName: docMsg.GetTitle(),
			File:     bytes.NewReader(docBytes),
		}

		tgBot.SendDocument(
			cfg.Telegram.TargetChatID,
			fileToSend,
			&gotgbot.SendDocumentOpts{
				Caption: bridgedText,
			},
		)

	case "sticker":

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		bridgedText += "\n<i>It was a sticker which is not supported</i>"

		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{},
		)

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

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))

		bridgedText += "\n<i>It was the following vCard</i>\n"

		sentMsg, _ := tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{},
		)

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

	case "location":

	case "":

		if text == "" {
			return
		}

		bridgedText := "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.ID))
		bridgedText += fmt.Sprintf("<code>%s</code>\n", html.EscapeString(v.Info.MessageSource.Sender.String()))
		bridgedText += "<code>------------</code>\n"
		bridgedText += fmt.Sprintf("<b>From:</b> %s\n", html.EscapeString(utils.WhatsAppGetContactName(v.Info.Sender)))
		if v.Info.IsGroup {
			bridgedText += fmt.Sprintf("<b>Chat:</b> %s\n", html.EscapeString(utils.WhatsAppGetGroupName(v.Info.Chat)))
		} else {
			bridgedText += "<b>Chat:</b> (PVT)\n"
		}
		bridgedText += fmt.Sprintf("<b>Time:</b> %s\n", html.EscapeString(v.Info.Timestamp.Local().Format(cfg.TimeFormat)))
		bridgedText += "<b>Body:</b>\n"
		if len(text) > 2000 {
			bridgedText += (html.EscapeString(text[:2000]) + "...")
		} else {
			bridgedText += html.UnescapeString(text)
		}

		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			bridgedText,
			&gotgbot.SendMessageOpts{},
		)

	default:

		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			"Received a new media type: "+v.Info.MediaType,
			&gotgbot.SendMessageOpts{},
		)
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
