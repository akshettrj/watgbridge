package whatsapp

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"wa-tg-bridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func WhatsAppEventHandler(evt interface{}) {
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot
	cfg := state.State.Config

	switch v := evt.(type) {

	case *events.TemporaryBan:
		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			fmt.Sprintf(
				"#bans\n\nTemporarily Banned from WhatsApp: <code>%s</code>",
				html.EscapeString(v.String()),
			),
			&gotgbot.SendMessageOpts{},
		)

	case *events.CallOffer:
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

	case *events.Message:
		if v.Info.IsFromMe {
			text := ""
			if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
				text = *v.Message.ExtendedTextMessage.Text
			} else {
				text = v.Message.GetConversation()
			}

			// Get ID of the current chat
			if text == ".id" {
				_, err := waClient.SendMessage(context.Background(), v.Info.Chat, "",
					&waProto.Message{
						ExtendedTextMessage: &waProto.ExtendedTextMessage{
							Text: proto.String(fmt.Sprintf(
								"The ID of current chat is : ```%s```", v.Info.Chat.String(),
							)),
							ContextInfo: &waProto.ContextInfo{
								StanzaId:      proto.String(v.Info.ID),
								Participant:   proto.String(v.Info.MessageSource.Sender.String()),
								QuotedMessage: v.Message,
							},
						},
					})
				if err != nil {
					log.Printf("[whatsapp] failed to reply to '.id' : %s", err)
				}
			}

			// Tag everyone in the group
			if strings.Contains(strings.ToLower(text), "@all") && v.Info.IsGroup {
				groupInfo, err := waClient.GetGroupInfo(v.Info.Chat)
				if err != nil {
					log.Printf("[whatsapp] failed to get group info of '%s' : %s", v.Info.Chat.String(), err)
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

				_, err = waClient.SendMessage(context.Background(), v.Info.Chat, "",
					&waProto.Message{
						ExtendedTextMessage: &waProto.ExtendedTextMessage{
							Text: proto.String(replyText),
							ContextInfo: &waProto.ContextInfo{
								StanzaId:      proto.String(v.Info.ID),
								Participant:   proto.String(v.Info.MessageSource.Sender.String()),
								QuotedMessage: v.Message,
								MentionedJid:  mentionedParticipants,
							},
						},
					},
				)
				if err != nil {
					log.Printf("[whatsapp] failed to reply to '@all' : %s", err)
				}
			}

			return
		}
	}
}
