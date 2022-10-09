package whatsapp

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"wa-tg-bridge/state"
	"wa-tg-bridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
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

	// Tag everyone in allowed groups
	if v.Info.IsGroup && slices.Contains(cfg.WhatsApp.TagAllAllowedGroups, v.Info.Chat.User) &&
		(strings.Contains(strings.ToLower(text), "@all") ||
			strings.Contains(strings.ToLower(text), "@everyone")) {
		TagAllHandler(v.Info.Chat, v.Message, v.Info.ID, v.Info.MessageSource.Sender.String(), false)
	}

	switch v.Info.MediaType {
	case "image":

	case "gif":

	case "video":

	case "ptt":
		// Voice Notes

	case "audio":

	case "document":
		// Any document like PDF, image, video etc

	case "vcard":
		// Contact

	case "location":

	default:

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
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot
	cfg := state.State.Config

	// TODO: Check and handle group calls
	var callerName string
	caller, err := waClient.Store.Contacts.GetContact(v.CallCreator)
	if err != nil || !caller.Found {
		callerName = v.CallCreator.String()
	} else {
		callerName = caller.FullName
		if callerName == "" {
			callerName = caller.BusinessName
		}
		if callerName == "" {
			callerName = caller.PushName
		}
		callerName += fmt.Sprintf(" [ %s ]", v.CallCreator.User)
	}

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
