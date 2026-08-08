package utils

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func WaParseJID(s string) (types.JID, bool) {
	if s[0] == '+' {
		s = SubString(s, 1, len(s)-1)
	}

	if !strings.ContainsRune(s, '@') {
		return types.NewJID(s, types.DefaultUserServer).ToNonAD(), true
	}

	recipient, err := types.ParseJID(s)

	recipient = recipient.ToNonAD()
	if err != nil || recipient.User == "" {
		return recipient, false
	}

	return recipient, true
}

func WaContextInfoReplyChatID(contextInfo *waE2E.ContextInfo, fallback string) string {
	if contextInfo == nil {
		return fallback
	}

	if placeholderKey := contextInfo.GetPlaceholderKey(); placeholderKey != nil && placeholderKey.GetRemoteJID() != "" {
		return placeholderKey.GetRemoteJID()
	}
	if remoteJID := contextInfo.GetRemoteJID(); remoteJID != "" {
		return remoteJID
	}
	if parentGroupJID := contextInfo.GetParentGroupJID(); parentGroupJID != "" {
		return parentGroupJID
	}
	return fallback
}

func WaContextInfoReplyMessageID(contextInfo *waE2E.ContextInfo) string {
	if contextInfo == nil {
		return ""
	}

	if stanzaID := contextInfo.GetStanzaID(); stanzaID != "" {
		return stanzaID
	}
	if placeholderKey := contextInfo.GetPlaceholderKey(); placeholderKey != nil {
		return placeholderKey.GetID()
	}
	return ""
}

func WaSetReplyContext(contextInfo *waE2E.ContextInfo, stanzaID, participant, remoteJID string) {
	contextInfo.StanzaID = proto.String(stanzaID)
	contextInfo.Participant = proto.String(participant)
	contextInfo.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
	if remoteJID != "" {
		contextInfo.RemoteJID = proto.String(remoteJID)
	}
}

func WaReplyContextAllowed(destinationChatID, quotedChatID, quotedParticipantID string) bool {
	destinationChatID = waNormalizeChatID(destinationChatID)
	quotedChatID = waNormalizeChatID(quotedChatID)
	quotedParticipantID = waNormalizeChatID(quotedParticipantID)

	if quotedChatID == "" || destinationChatID == quotedChatID {
		return true
	}

	if quotedChatID == "status@broadcast" {
		return destinationChatID == quotedParticipantID
	}

	quotedChatJID, ok := WaParseJID(quotedChatID)
	if !ok || quotedChatJID.Server != types.GroupServer {
		return false
	}
	return destinationChatID == quotedParticipantID
}

func waNormalizeChatID(chatID string) string {
	if chatID == "" {
		return ""
	}

	jid, ok := WaParseJID(chatID)
	if !ok {
		return chatID
	}
	if jid.Server == types.HiddenUserServer {
		pn, err := state.State.WhatsAppClient.Store.LIDs.GetPNForLID(context.Background(), jid)
		if err == nil {
			jid = pn
		}
	}
	return jid.ToNonAD().String()
}

func WaFuzzyFindContacts(query string) (map[string]string, int, error) {
	var (
		results      = make(map[string]string)
		resultsCount = 0
	)

	contacts, err := database.ContactGetAll()
	if err != nil {
		return nil, 0, err
	}

	var searchSpace []string
	for _, contact := range contacts {
		jid := contact.ID
		if contact.FirstName != "" {
			searchSpace = append(searchSpace, jid+"||"+strings.ToLower(contact.FirstName))
		}
		if contact.FullName != "" {
			searchSpace = append(searchSpace, jid+"||"+strings.ToLower(contact.FullName))
		}
		if contact.PushName != "" {
			searchSpace = append(searchSpace, jid+"||"+strings.ToLower(contact.PushName))
		}
		if contact.BusinessName != "" {
			searchSpace = append(searchSpace, jid+"||"+strings.ToLower(contact.BusinessName))
		}
	}

	fuzzyResults := fuzzy.Find(strings.ToLower(query), searchSpace)
	for _, res := range fuzzyResults {
		info := strings.SplitN(res, "||", 2)

		contact := contacts[info[0]]
		if _, exists := results[info[0]]; exists {
			continue
		}

		resultsCount += 1
		name := ""
		if contact.FullName != "" {
			name += (contact.FullName + " (s)")
		}
		if contact.BusinessName != "" {
			if name != "" {
				name += ", "
			}
			name += (contact.BusinessName + " (b)")
		}
		if contact.PushName != "" {
			if name != "" {
				name += ", "
			}
			name += (contact.PushName + " (p)")
		}
		results[contact.ID] = name
	}

	return results, resultsCount, nil
}

func WaGetGroupName(jid types.JID) string {
	waClient := state.State.WhatsAppClient

	groupInfo, err := waClient.GetGroupInfo(context.Background(), jid)
	if err != nil {
		return jid.User
	}
	return groupInfo.Name
}

func WaGetContactName(jid types.JID) string {
	if jid.ToNonAD() == state.State.WhatsAppClient.Store.ID.ToNonAD() {
		return "You"
	}

	var name string
	waClient := state.State.WhatsAppClient

	var (
		pn           types.JID
		firstName    string
		fullName     string
		pushName     string
		businessName string
		found        bool
		err          error
	)

	if jid.Server == types.HiddenUserServer {
		pn, err = waClient.Store.LIDs.GetPNForLID(context.Background(), jid)
		if err == nil {
			firstName, fullName, pushName, businessName, found, err = database.ContactNameGet(pn.User, pn.Server)
		}
	}

	if !found {
		firstName, fullName, pushName, businessName, found, err = database.ContactNameGet(jid.User, jid.Server)
	}

	if err == nil && found {
		if fullName != "" {
			name = fullName
		} else if businessName != "" {
			name = businessName + " (" + jid.User + ")"
		} else if pushName != "" {
			name = pushName + " (" + jid.User + ")"
		} else if firstName != "" {
			name = firstName + " (" + jid.User + ")"
		}
	} else {
		contact, err := waClient.Store.Contacts.GetContact(context.Background(), jid)
		if err == nil && contact.Found {
			if contact.FullName != "" {
				name = contact.FullName
			} else if contact.BusinessName != "" {
				name = contact.BusinessName + " (" + jid.User + ")"
			} else if contact.PushName != "" {
				name = contact.PushName + " (" + jid.User + ")"
			} else if contact.FirstName != "" {
				name = contact.FirstName + " (" + jid.User + ")"
			}
		}
	}

	if name == "" {
		name = jid.User
	}

	return name
}

func WaTagAll(group types.JID, msg *waE2E.Message, msgId, msgSender string, msgIsFromMe bool) {
	var (
		cfg      = state.State.Config
		waClient = state.State.WhatsAppClient
		tgBot    = state.State.TelegramBot
	)

	groupInfo, err := waClient.GetGroupInfo(context.Background(), group)
	if err != nil {
		log.Printf("[whatsapp] failed to get group info of '%s': %s\n", group.String(), err)
		return
	}

	var (
		replyText = ""
		mentioned = []string{}
	)

	for _, participant := range groupInfo.Participants {
		if participant.JID.User == waClient.Store.ID.User {
			continue
		}

		replyText += fmt.Sprintf("@%s ", participant.JID.User)
		mentioned = append(mentioned, participant.JID.String())
	}

	contextInfo := &waE2E.ContextInfo{
		StanzaID:      proto.String(msgId),
		Participant:   proto.String(msgSender),
		QuotedMessage: msg,
		MentionedJID:  mentioned,
	}

	// Apply ephemeral settings if the chat has disappearing messages enabled
	isEphemeral, ephemeralTimer, _, err := database.GetEphemeralSettings(group.String())
	if err == nil && isEphemeral && ephemeralTimer > 0 {
		contextInfo.Expiration = &ephemeralTimer
	}

	_, err = waClient.SendMessage(context.Background(), group, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(replyText),
			ContextInfo: contextInfo,
		},
	})
	if err != nil {
		log.Printf("[whatsapp] failed to reply to '@all/@everyone': %s\n", err)
		return
	}

	if !msgIsFromMe {
		tagsThreadId, err := TgGetOrMakeThreadFromWa_String("mentions", cfg.Telegram.TargetChatID, "Mentions")
		if err != nil {
			TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0, "Failed to create/retreive corresponding thread id for status/calls/tags", err)
			return
		}

		bridgedText := fmt.Sprintf("#tagall\n\nEveryone was mentioned in a group\n\n👥: <i>%s</i>",
			html.EscapeString(groupInfo.Name))

		TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tagsThreadId, bridgedText)
	}
}

func WaSendText(chat types.JID, text, stanzaId, participantId string, quotedMsg *waE2E.Message, isReply bool) (whatsmeow.SendResponse, error) {
	waClient := state.State.WhatsAppClient

	msgToSend := &waE2E.Message{}
	if isReply {
		msgToSend.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(stanzaId),
				Participant:   proto.String(participantId),
				QuotedMessage: quotedMsg,
			},
		}
	} else {
		msgToSend.Conversation = proto.String(text)
	}

	return waClient.SendMessage(context.Background(), chat, msgToSend)
}

func WaSendStatus(b *gotgbot.Bot, msg *gotgbot.Message, statusText string, updateId int64) (whatsmeow.SendResponse, error) {
	waClient := state.State.WhatsAppClient
	cfg := state.State.Config
	target := types.StatusBroadcastJID

	if msg.Photo != nil && len(msg.Photo) > 0 {
		bestPhoto := msg.Photo[0]
		for _, photo := range msg.Photo {
			if photo.Height*photo.Width > bestPhoto.Height*bestPhoto.Width {
				bestPhoto = photo
			}
		}

		if !cfg.Telegram.SelfHostedAPI && bestPhoto.FileSize > DownloadSizeLimit {
			return whatsmeow.SendResponse{}, fmt.Errorf("photo exceeds the Telegram size restriction")
		}

		imageFile, err := b.GetFile(bestPhoto.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to retrieve image file from Telegram: %w", err)
		}

		imageBytes, err := TgDownloadByFilePath(b, imageFile.FilePath)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to download image from Telegram: %w", err)
		}

		uploadedImage, err := waClient.Upload(context.Background(), imageBytes, whatsmeow.MediaImage)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to upload image to WhatsApp: %w", err)
		}

		msgToSend := &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:           proto.String(statusText),
				URL:               proto.String(uploadedImage.URL),
				DirectPath:        proto.String(uploadedImage.DirectPath),
				MediaKey:          uploadedImage.MediaKey,
				MediaKeyTimestamp: proto.Int64(time.Now().Unix()),
				Mimetype:          proto.String(http.DetectContentType(imageBytes)),
				FileEncSHA256:     uploadedImage.FileEncSHA256,
				FileSHA256:        uploadedImage.FileSHA256,
				FileLength:        proto.Uint64(uint64(len(imageBytes))),
				Height:            proto.Uint32(uint32(bestPhoto.Height)),
				Width:             proto.Uint32(uint32(bestPhoto.Width)),
			},
		}
		return waClient.SendMessage(context.Background(), target, msgToSend)
	}

	if msg.Video != nil {
		if !cfg.Telegram.SelfHostedAPI && msg.Video.FileSize > DownloadSizeLimit {
			return whatsmeow.SendResponse{}, fmt.Errorf("video exceeds the Telegram size restriction")
		}

		videoFile, err := b.GetFile(msg.Video.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to retrieve video file from Telegram: %w", err)
		}

		videoBytes, err := TgDownloadByFilePath(b, videoFile.FilePath)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to download video from Telegram: %w", err)
		}

		uploadedVideo, err := waClient.Upload(context.Background(), videoBytes, whatsmeow.MediaVideo)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to upload video to WhatsApp: %w", err)
		}

		msgToSend := &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:       proto.String(statusText),
				URL:           proto.String(uploadedVideo.URL),
				DirectPath:    proto.String(uploadedVideo.DirectPath),
				MediaKey:      uploadedVideo.MediaKey,
				Mimetype:      proto.String(msg.Video.MimeType),
				FileEncSHA256: uploadedVideo.FileEncSHA256,
				FileSHA256:    uploadedVideo.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(videoBytes))),
				Seconds:       proto.Uint32(uint32(msg.Video.Duration)),
				GifPlayback:   proto.Bool(false),
				Height:        proto.Uint32(uint32(msg.Video.Height)),
				Width:         proto.Uint32(uint32(msg.Video.Width)),
			},
		}
		return waClient.SendMessage(context.Background(), target, msgToSend)
	}

	if msg.Voice != nil {
		if !cfg.Telegram.SelfHostedAPI && msg.Voice.FileSize > DownloadSizeLimit {
			return whatsmeow.SendResponse{}, fmt.Errorf("voice note exceeds the Telegram size restriction")
		}

		voiceFile, err := b.GetFile(msg.Voice.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to retrieve voice note from Telegram: %w", err)
		}

		voiceBytes, err := TgDownloadByFilePath(b, voiceFile.FilePath)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to download voice note from Telegram: %w", err)
		}

		convertedVoiceBytes, err := ConvertAudioToWhatsAppFormat(voiceBytes, updateId)
		if err != nil {
			log.Printf("[whatsapp] failed to convert voice note to WhatsApp format, using original: %s\n", err)
			convertedVoiceBytes = voiceBytes
		}

		uploadedVoice, err := waClient.Upload(context.Background(), convertedVoiceBytes, whatsmeow.MediaAudio)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to upload voice note to WhatsApp: %w", err)
		}

		msgToSend := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploadedVoice.URL),
				DirectPath:    proto.String(uploadedVoice.DirectPath),
				MediaKey:      uploadedVoice.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSHA256: uploadedVoice.FileEncSHA256,
				FileSHA256:    uploadedVoice.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(convertedVoiceBytes))),
				Seconds:       proto.Uint32(uint32(msg.Voice.Duration)),
				PTT:           proto.Bool(true),
			},
		}
		return waClient.SendMessage(context.Background(), target, msgToSend)
	}

	if msg.Audio != nil {
		if !cfg.Telegram.SelfHostedAPI && msg.Audio.FileSize > DownloadSizeLimit {
			return whatsmeow.SendResponse{}, fmt.Errorf("audio exceeds the Telegram size restriction")
		}

		audioFile, err := b.GetFile(msg.Audio.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: -1,
			},
		})
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to retrieve audio file from Telegram: %w", err)
		}

		audioBytes, err := TgDownloadByFilePath(b, audioFile.FilePath)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to download audio from Telegram: %w", err)
		}

		convertedAudioBytes, err := ConvertAudioToWhatsAppFormat(audioBytes, updateId)
		if err != nil {
			log.Printf("[whatsapp] failed to convert audio to WhatsApp format, using original: %s\n", err)
			convertedAudioBytes = audioBytes
		}

		uploadedAudio, err := waClient.Upload(context.Background(), convertedAudioBytes, whatsmeow.MediaAudio)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to upload audio to WhatsApp: %w", err)
		}

		msgToSend := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(uploadedAudio.URL),
				DirectPath:    proto.String(uploadedAudio.DirectPath),
				MediaKey:      uploadedAudio.MediaKey,
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				FileEncSHA256: uploadedAudio.FileEncSHA256,
				FileSHA256:    uploadedAudio.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(convertedAudioBytes))),
				Seconds:       proto.Uint32(uint32(msg.Audio.Duration)),
				PTT:           proto.Bool(false),
			},
		}
		return waClient.SendMessage(context.Background(), target, msgToSend)
	}

	if msg.Text != "" {
		backgroundColor, err := waParseArgbColor(cfg.WhatsApp.StatusBackgroundColor)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("invalid status background color: %w", err)
		}
		font, err := waStatusFontType(cfg.WhatsApp.StatusFont)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("invalid status font: %w", err)
		}
		msgToSend := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:           proto.String(statusText),
				BackgroundArgb: proto.Uint32(backgroundColor),
				Font:           font.Enum(),
			},
		}
		return waClient.SendMessage(context.Background(), target, msgToSend)
	}

	return whatsmeow.SendResponse{}, fmt.Errorf("unsupported message type, send text, an image, a video or an audio")
}

func waParseArgbColor(s string) (uint32, error) {
	hexStr := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "#"), "0x")
	var value uint64
	var err error
	switch len(hexStr) {
	case 6:
		value, err = strconv.ParseUint(hexStr, 16, 32)
		if err != nil {
			return 0, err
		}
		return uint32(value) | 0xFF000000, nil
	case 8:
		value, err = strconv.ParseUint(hexStr, 16, 32)
		if err != nil {
			return 0, err
		}
		return uint32(value), nil
	default:
		return 0, fmt.Errorf("color must be a 6-digit hex (RRGGBB) or 8-digit hex (AARRGGBB)")
	}
}

func waStatusFontType(s string) (waE2E.ExtendedTextMessage_FontType, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SYSTEM":
		return waE2E.ExtendedTextMessage_SYSTEM, nil
	case "SYSTEM_TEXT":
		return waE2E.ExtendedTextMessage_SYSTEM_TEXT, nil
	case "FB_SCRIPT":
		return waE2E.ExtendedTextMessage_FB_SCRIPT, nil
	case "SYSTEM_BOLD":
		return waE2E.ExtendedTextMessage_SYSTEM_BOLD, nil
	case "MORNINGBREEZE_REGULAR":
		return waE2E.ExtendedTextMessage_MORNINGBREEZE_REGULAR, nil
	case "CALISTOGA_REGULAR":
		return waE2E.ExtendedTextMessage_CALISTOGA_REGULAR, nil
	case "EXO2_EXTRABOLD":
		return waE2E.ExtendedTextMessage_EXO2_EXTRABOLD, nil
	case "COURIERPRIME_BOLD":
		return waE2E.ExtendedTextMessage_COURIERPRIME_BOLD, nil
	default:
		return 0, fmt.Errorf("unknown font: %s", s)
	}
}
