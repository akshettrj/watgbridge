package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	goVCard "github.com/emersion/go-vcard"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
)

// ============================================================
// Top-level event dispatcher
// ============================================================

func WhatsAppEventHandler(evt interface{}) {
	cfg := state.State.Config

	switch v := evt.(type) {
	case *events.LoggedOut:
		LogoutHandler(v)

	case *events.Receipt:
		ReceiptEventHandler(v)

	case *events.Picture:
		if !cfg.WhatsApp.SkipProfilePictureUpdates {
			PictureEventHandler(v)
		}

	case *events.GroupInfo:
		if !cfg.WhatsApp.SkipGroupSettingsUpdates {
			GroupInfoEventHandler(v)
		}

	case *events.PushName:
		PushNameEventHandler(v)

	case *events.UserAbout:
		UserAboutEventHandler(v)

	case *events.CallOffer:
		CallOfferEventHandler(v)

	case *events.UndecryptableMessage:
		UndecryptableMessageEventHandler(v)

	case *events.Message:
		handleMessageEvent(cfg, v)
	}
}

// handleMessageEvent processes an incoming *events.Message, checking for
// edits, revokes and ephemeral settings before delegating to the
// from-me / from-others handlers.
// hoistAndExtractInteractive inspects a waE2E.Message to check if it contains
// interactive/button structures, extracts their plain text representations (including button options),
// and hoists any nested media (images, videos, documents, locations) to the root level of the message.
func hoistAndExtractInteractive(msg *waE2E.Message) (string, bool) {
	if msg == nil {
		return "", false
	}

	var parts []string
	var isDoc bool

	// 1. ButtonsMessage
	if bm := msg.GetButtonsMessage(); bm != nil {
		if bm.GetText() != "" {
			parts = append(parts, bm.GetText())
		}
		if bm.GetContentText() != "" {
			parts = append(parts, bm.GetContentText())
		}
		if bm.GetFooterText() != "" {
			parts = append(parts, "<i>"+bm.GetFooterText()+"</i>")
		}

		var btns []string
		for _, btn := range bm.GetButtons() {
			if btn.GetButtonText() != nil {
				btns = append(btns, "🔘 "+btn.GetButtonText().GetDisplayText())
			}
		}
		if len(btns) > 0 {
			parts = append(parts, "\n"+strings.Join(btns, "\n"))
		}

		formattedText := strings.Join(parts, "\n")

		// Hoist media from header
		if bm.GetImageMessage() != nil {
			msg.ImageMessage = bm.GetImageMessage()
			msg.ImageMessage.Caption = proto.String(formattedText)
		} else if bm.GetVideoMessage() != nil {
			msg.VideoMessage = bm.GetVideoMessage()
			msg.VideoMessage.Caption = proto.String(formattedText)
		} else if bm.GetDocumentMessage() != nil {
			msg.DocumentMessage = bm.GetDocumentMessage()
			msg.DocumentMessage.Caption = proto.String(formattedText)
			isDoc = true
		} else if bm.GetLocationMessage() != nil {
			msg.LocationMessage = bm.GetLocationMessage()
		}

		return formattedText, isDoc
	}

	// 2. TemplateMessage
	if tm := msg.GetTemplateMessage(); tm != nil {
		if hydrated := tm.GetHydratedTemplate(); hydrated != nil {
			if titleText := hydrated.GetHydratedTitleText(); titleText != "" {
				parts = append(parts, "<b>"+titleText+"</b>")
			}

			if hydrated.GetHydratedContentText() != "" {
				parts = append(parts, hydrated.GetHydratedContentText())
			}
			if hydrated.GetHydratedFooterText() != "" {
				parts = append(parts, "<i>"+hydrated.GetHydratedFooterText()+"</i>")
			}

			var btns []string
			for _, btn := range hydrated.GetHydratedButtons() {
				if q := btn.GetQuickReplyButton(); q != nil {
					btns = append(btns, "🔘 "+q.GetDisplayText())
				} else if u := btn.GetUrlButton(); u != nil {
					btns = append(btns, fmt.Sprintf("🔗 <a href=\"%s\">%s</a>", u.GetURL(), u.GetDisplayText()))
				} else if c := btn.GetCallButton(); c != nil {
					btns = append(btns, fmt.Sprintf("📞 %s (%s)", c.GetDisplayText(), c.GetPhoneNumber()))
				}
			}
			if len(btns) > 0 {
				parts = append(parts, "\n"+strings.Join(btns, "\n"))
			}

			formattedText := strings.Join(parts, "\n")

			// Hoist media from hydrated template
			if hydrated.GetImageMessage() != nil {
				msg.ImageMessage = hydrated.GetImageMessage()
				msg.ImageMessage.Caption = proto.String(formattedText)
			} else if hydrated.GetVideoMessage() != nil {
				msg.VideoMessage = hydrated.GetVideoMessage()
				msg.VideoMessage.Caption = proto.String(formattedText)
			} else if hydrated.GetDocumentMessage() != nil {
				msg.DocumentMessage = hydrated.GetDocumentMessage()
				msg.DocumentMessage.Caption = proto.String(formattedText)
				isDoc = true
			} else if hydrated.GetLocationMessage() != nil {
				msg.LocationMessage = hydrated.GetLocationMessage()
			}

			return formattedText, isDoc
		}
	}

	// 3. InteractiveMessage
	if im := msg.GetInteractiveMessage(); im != nil {
		if header := im.GetHeader(); header != nil {
			if header.GetTitle() != "" {
				parts = append(parts, "<b>"+header.GetTitle()+"</b>")
			}
			if header.GetSubtitle() != "" {
				parts = append(parts, "<i>"+header.GetSubtitle()+"</i>")
			}
		}

		if im.GetBody() != nil && im.GetBody().GetText() != "" {
			parts = append(parts, im.GetBody().GetText())
		}
		if im.GetFooter() != nil && im.GetFooter().GetText() != "" {
			parts = append(parts, "<i>"+im.GetFooter().GetText()+"</i>")
		}

		// Native flow buttons
		if nf := im.GetNativeFlowMessage(); nf != nil {
			var btns []string
			for _, btn := range nf.GetButtons() {
				label := btn.GetName()
				if btn.GetButtonParamsJSON() != "" {
					var params struct {
						DisplayText string `json:"display_text"`
						Title       string `json:"title"`
					}
					if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &params); err == nil {
						if params.DisplayText != "" {
							label = params.DisplayText
						} else if params.Title != "" {
							label = params.Title
						}
					}
				}
				btns = append(btns, "🔘 "+label)
			}
			if len(btns) > 0 {
				parts = append(parts, "\n"+strings.Join(btns, "\n"))
			}
		}

		formattedText := strings.Join(parts, "\n")

		if header := im.GetHeader(); header != nil {
			// Hoist media from header
			if header.GetImageMessage() != nil {
				msg.ImageMessage = header.GetImageMessage()
				msg.ImageMessage.Caption = proto.String(formattedText)
			} else if header.GetVideoMessage() != nil {
				msg.VideoMessage = header.GetVideoMessage()
				msg.VideoMessage.Caption = proto.String(formattedText)
			} else if header.GetDocumentMessage() != nil {
				msg.DocumentMessage = header.GetDocumentMessage()
				msg.DocumentMessage.Caption = proto.String(formattedText)
				isDoc = true
			} else if header.GetLocationMessage() != nil {
				msg.LocationMessage = header.GetLocationMessage()
			}
		}

		return formattedText, isDoc
	}

	// 4. ListMessage
	if lm := msg.GetListMessage(); lm != nil {
		if lm.GetTitle() != "" {
			parts = append(parts, "<b>"+lm.GetTitle()+"</b>")
		}
		if lm.GetDescription() != "" {
			parts = append(parts, lm.GetDescription())
		}
		if lm.GetFooterText() != "" {
			parts = append(parts, "<i>"+lm.GetFooterText()+"</i>")
		}

		var sections []string
		for _, sect := range lm.GetSections() {
			sectText := ""
			if sect.GetTitle() != "" {
				sectText += "📝 <b>" + sect.GetTitle() + "</b>\n"
			} else {
				sectText += "📝 <b>Options</b>\n"
			}
			var rows []string
			for _, row := range sect.GetRows() {
				rowText := "• " + row.GetTitle()
				if row.GetDescription() != "" {
					rowText += " (" + row.GetDescription() + ")"
				}
				rows = append(rows, rowText)
			}
			sectText += strings.Join(rows, "\n")
			sections = append(sections, sectText)
		}
		if len(sections) > 0 {
			parts = append(parts, "\n"+strings.Join(sections, "\n\n"))
		}

		return strings.Join(parts, "\n"), false
	}

	return "", false
}

func getPinInChatMessage(msg *waE2E.Message) *waE2E.PinInChatMessage {
	if msg == nil {
		return nil
	}
	if pin := msg.GetPinInChatMessage(); pin != nil {
		return pin
	}
	if eph := msg.GetEphemeralMessage(); eph != nil {
		if pin := eph.GetMessage().GetPinInChatMessage(); pin != nil {
			return pin
		}
	}
	if vo := msg.GetViewOnceMessage(); vo != nil {
		if pin := vo.GetMessage().GetPinInChatMessage(); pin != nil {
			return pin
		}
	}
	if vo2 := msg.GetViewOnceMessageV2(); vo2 != nil {
		if pin := vo2.GetMessage().GetPinInChatMessage(); pin != nil {
			return pin
		}
	}
	return nil
}

func handlePinInChatMessageEvent(cfg *state.Config, v *events.Message, pinMsg *waE2E.PinInChatMessage) {
	logger := state.State.Logger
	defer logger.Sync()

	key := pinMsg.GetKey()
	if key == nil {
		return
	}

	targetWaMsgId := key.GetID()
	if targetWaMsgId == "" {
		return
	}

	waChatId := key.GetRemoteJID()
	if waChatId == "" {
		waChatId = v.Info.Chat.String()
	}

	tgChatId, _, tgMsgId, err := database.MsgIdGetTgFromWa(targetWaMsgId, waChatId)
	if err != nil || tgMsgId == 0 {
		logger.Warn("could not find Telegram message for pinned WhatsApp message",
			zap.String("wa_msg_id", targetWaMsgId),
			zap.String("wa_chat_id", waChatId),
		)
		return
	}

	tgBot := state.State.TelegramBot

	switch pinMsg.GetType() {
	case waE2E.PinInChatMessage_PIN_FOR_ALL:
		_, err = tgBot.PinChatMessage(tgChatId, tgMsgId, &gotgbot.PinChatMessageOpts{})
		if err != nil {
			logger.Error("failed to pin Telegram message",
				zap.Int64("tg_chat_id", tgChatId),
				zap.Int64("tg_msg_id", tgMsgId),
				zap.Error(err),
			)
		} else {
			logger.Info("successfully synced pinned message to Telegram",
				zap.String("wa_msg_id", targetWaMsgId),
				zap.Int64("tg_msg_id", tgMsgId),
			)
		}
	case waE2E.PinInChatMessage_UNPIN_FOR_ALL:
		_, err = tgBot.UnpinChatMessage(tgChatId, &gotgbot.UnpinChatMessageOpts{
			MessageId: &tgMsgId,
		})
		if err != nil {
			logger.Error("failed to unpin Telegram message",
				zap.Int64("tg_chat_id", tgChatId),
				zap.Int64("tg_msg_id", tgMsgId),
				zap.Error(err),
			)
		} else {
			logger.Info("successfully synced unpinned message to Telegram",
				zap.String("wa_msg_id", targetWaMsgId),
				zap.Int64("tg_msg_id", tgMsgId),
			)
		}
	}
}

// handleMessageEvent processes an incoming *events.Message, checking for
// edits, revokes and ephemeral settings before delegating to the
// from-me / from-others handlers.
func handleMessageEvent(cfg *state.Config, v *events.Message) {
	if pinMsg := getPinInChatMessage(v.Message); pinMsg != nil {
		if !cfg.WhatsApp.SkipPinnedMessages {
			handlePinInChatMessageEvent(cfg, v, pinMsg)
		}
		return
	}

	isEdited := false
	if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil &&
		protoMsg.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		isEdited = true
	}

	if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil &&
		protoMsg.GetType() == waE2E.ProtocolMessage_REVOKE {
		RevokedMessageEventHandler(v)
		return
	}

	if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil &&
		protoMsg.GetType() == waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		if protoMsg.GetEphemeralExpiration() == 0 {
			database.UpdateEphemeralSettings(v.Info.Chat.ToNonAD().String(), false, 0)
		} else {
			database.UpdateEphemeralSettings(v.Info.Chat.ToNonAD().String(), true, protoMsg.GetEphemeralExpiration())
		}
		return
	}

	// Extract the plain-text body
	text := ""
	isDocument := false

	// First, hoist and extract interactive components from the root message and any edited message
	interactiveText, isInteractiveDoc := hoistAndExtractInteractive(v.Message)
	if interactiveText != "" {
		text = interactiveText
	}
	if isInteractiveDoc {
		isDocument = true
	}

	if isEdited {
		msg := v.Message.GetProtocolMessage().GetEditedMessage()
		if msg != nil {
			editedInteractiveText, isEditedInteractiveDoc := hoistAndExtractInteractive(msg)
			if editedInteractiveText != "" {
				text = editedInteractiveText
			}
			if isEditedInteractiveDoc {
				isDocument = true
			}

			if msg.GetImageMessage() != nil {
				text = msg.GetImageMessage().GetCaption()
				isDocument = true
			} else if msg.GetVideoMessage() != nil {
				text = msg.GetVideoMessage().GetCaption()
				isDocument = true
			} else if msg.GetDocumentMessage() != nil {
				text = msg.GetDocumentMessage().GetFileName()
				isDocument = true
			} else if extText := msg.GetExtendedTextMessage().GetText(); extText != "" {
				if text == "" {
					text = extText
				}
			} else {
				if text == "" {
					text = msg.GetConversation()
				}
			}
		}
	} else {
		if v.Message.GetImageMessage() != nil {
			if text == "" {
				text = v.Message.GetImageMessage().GetCaption()
			}
		} else if v.Message.GetVideoMessage() != nil {
			if text == "" {
				text = v.Message.GetVideoMessage().GetCaption()
			}
		} else if v.Message.GetDocumentMessage() != nil {
			if text == "" {
				text = v.Message.GetDocumentMessage().GetFileName()
			}
			isDocument = true
		} else if extText := v.Message.GetExtendedTextMessage().GetText(); extText != "" {
			if text == "" {
				text = extText
			}
		} else {
			if text == "" {
				text = v.Message.GetConversation()
			}
		}
	}

	if v.Info.IsFromMe {
		MessageFromMeEventHandler(text, v, isEdited, isDocument)
	} else {
		MessageFromOthersEventHandler(text, v, isEdited, isDocument)
	}
}

// ============================================================
// Messages from the bot owner's own account
// ============================================================

func MessageFromMeEventHandler(text string, v *events.Message, isEdited bool, isDocument bool) {
	logger := state.State.Logger
	defer logger.Sync()

	var msgId string
	if isEdited {
		msgId = v.Message.GetProtocolMessage().GetKey().GetID()
	} else {
		msgId = v.Info.ID
	}

	// Reply with chat ID when ".id" is sent
	if text == ".id" {
		waClient := state.State.WhatsAppClient
		_, err := waClient.SendMessage(context.Background(), v.Info.Chat, &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(fmt.Sprintf(
					"The ID of the current chat is:\n\n```%s```", v.Info.Chat.String())),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:      proto.String(msgId),
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

	// Tag everyone when @all / @everyone is used
	if !isEdited {
		textSplit := strings.Fields(strings.ToLower(text))
		if v.Info.IsGroup &&
			(slices.Contains(textSplit, "@all") || slices.Contains(textSplit, "@everyone")) {
			utils.WaTagAll(v.Info.Chat, v.Message, msgId, v.Info.MessageSource.Sender.String(), true)
		}
	}

	if state.State.Config.WhatsApp.SendMyMessagesFromOtherDevices {
		MessageFromOthersEventHandler(text, v, isEdited, isDocument)
	}
}

// ============================================================
// Messages from other people (main bridge path)
// ============================================================

func MessageFromOthersEventHandler(text string, v *events.Message, isEdited bool, isDocument bool) {
	var (
		cfg      = state.State.Config
		logger   = state.State.Logger
		tgBot    = state.State.TelegramBot
		waClient = state.State.WhatsAppClient
	)
	defer logger.Sync()

	// Determine message ID
	var msgId string
	if isEdited {
		msgId = v.Message.GetProtocolMessage().GetKey().GetID()
	} else {
		msgId = v.Info.ID
	}

	// Skip duplicate events
	if !isEdited {
		tgChatId, _, _, _ := database.MsgIdGetTgFromWa(msgId, v.Info.Chat.String())
		if tgChatId == cfg.Telegram.TargetChatID {
			logger.Debug("returning because duplicate event id emitted",
				zap.String("event_id", v.Info.ID),
				zap.String("chat_jid", v.Info.Chat.String()),
			)
			return
		}
	}

	// Skip status from ignored chats or fully-ignored chats
	if v.Info.Chat.String() == "status@broadcast" &&
		(cfg.WhatsApp.SkipStatus ||
			slices.Contains(cfg.WhatsApp.StatusIgnoredChats, v.Info.MessageSource.Sender.User)) {
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

	replyMarkup := utils.TgBuildUrlButton(
		utils.WaGetContactName(v.Info.Sender),
		fmt.Sprintf("https://wa.me/%s", v.Info.MessageSource.Sender.ToNonAD().User),
	)

	// Handle @all / @everyone from allowed groups
	if !isEdited {
		if lowercaseText := strings.ToLower(text); !v.Info.IsFromMe && v.Info.IsGroup &&
			slices.Contains(cfg.WhatsApp.TagAllAllowedGroups, v.Info.Chat.User) &&
			(strings.Contains(lowercaseText, "@all") || strings.Contains(lowercaseText, "@everyone")) {
			utils.WaTagAll(v.Info.Chat, v.Message, msgId, v.Info.MessageSource.Sender.String(), false)
		}
	}

	// Build header (sender, group, timestamp)
	bridgedText := buildBridgedHeader(v.Info, cfg, isEdited)

	// Resolve reply-to mapping and thread ID
	var (
		replyToMsgId  int64
		threadId      int64
		threadIdFound bool
	)

	if isEdited {
		tgChatId, tgThreadId, tgMsgId, err := database.MsgIdGetTgFromWa(
			v.Message.GetProtocolMessage().GetKey().GetID(),
			v.Info.Chat.String(),
		)
		if err == nil && tgChatId == cfg.Telegram.TargetChatID {
			replyToMsgId = tgMsgId
			threadId = tgThreadId
			threadIdFound = true
		}
	} else {
		contextInfo := getContextInfo(v.Message)
		if contextInfo != nil {
			if contextInfo.GetIsForwarded() {
				bridgedText += fmt.Sprintf("⏩: Forwarded %v times\n", contextInfo.GetForwardingScore())
			}

			// Notify when the bot owner is mentioned
			if mentioned := contextInfo.GetMentionedJID(); v.Info.IsGroup && mentioned != nil {
				for _, jid := range mentioned {
					parsedJid, _ := utils.WaParseJID(jid)
					if parsedJid.User == waClient.Store.ID.User {
						tagInfoText := "#mentions\n\n" + bridgedText +
							fmt.Sprintf("\n<i>You were tagged in %s</i>",
								html.EscapeString(utils.WaGetGroupName(v.Info.Chat)))

						mentionThreadId, err := utils.TgGetOrMakeThreadFromWa_String(
							"mentions", cfg.Telegram.TargetChatID, "Mentions")
						if err != nil {
							utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0,
								"failed to create/find thread id for 'mentions'", err)
						} else {
							tgBot.SendMessage(cfg.Telegram.TargetChatID, tagInfoText, &gotgbot.SendMessageOpts{
								MessageThreadId: mentionThreadId,
								ReplyMarkup:     replyMarkup,
							})
						}
						break
					}
				}
			}

			// Resolve the quoted-message mapping
			stanzaId := contextInfo.GetStanzaID()
			tgChatId, tgThreadId, tgMsgId, err := database.MsgIdGetTgFromWa(stanzaId, v.Info.Chat.String())
			if err == nil && tgChatId == cfg.Telegram.TargetChatID {
				replyToMsgId = tgMsgId
				threadId = tgThreadId
				threadIdFound = true
			}
		}
	}

	if !strings.HasSuffix(bridgedText, "\n\n") {
		bridgedText += "\n"
	}

	// Resolve thread if not found from reply context
	if !threadIdFound {
		var err error
		threadId, err = resolveThreadId(v.Info, cfg, tgBot)
		if err != nil {
			utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0,
				fmt.Sprintf("failed to create/find thread id for '%s'", v.Info.Chat.String()), err)
			return
		}
	}

	// Build bridge context for media handlers
	bc := &bridgeContext{
		cfg:          cfg,
		logger:       logger,
		tgBot:        tgBot,
		waClient:     waClient,
		bridgedText:  bridgedText,
		replyToMsgId: replyToMsgId,
		threadId:     threadId,
		msgId:        msgId,
		senderStr:    v.Info.MessageSource.Sender.String(),
		chatStr:      v.Info.Chat.String(),
		replyMarkup:  replyMarkup,
	}

	// Dispatch to the appropriate media-type handler
	switch {
	case v.Message.GetImageMessage() != nil:
		bc.handleImageMessage(v)
	case v.Message.GetVideoMessage() != nil && v.Message.GetVideoMessage().GetGifPlayback():
		bc.handleGifMessage(v)
	case v.Message.GetVideoMessage() != nil || v.Message.GetPtvMessage() != nil:
		bc.handleVideoMessage(v)
	case v.Message.GetAudioMessage() != nil && v.Message.GetAudioMessage().GetPTT():
		bc.handleVoiceNoteMessage(v)
	case v.Message.GetAudioMessage() != nil:
		bc.handleAudioMessage(v)
	case v.Message.GetDocumentMessage() != nil:
		bc.handleDocumentMessage(v)
	case v.Message.GetStickerMessage() != nil:
		bc.handleStickerMessage(v)
	case v.Message.GetContactMessage() != nil:
		bc.handleContactMessage(v)
	case v.Message.GetContactsArrayMessage() != nil:
		bc.handleContactsArrayMessage(v)
	case v.Message.GetLocationMessage() != nil:
		bc.handleLocationMessage(v)
	case v.Message.GetLiveLocationMessage() != nil:
		bc.handleLiveLocationMessage(v)
	case v.Message.GetPollCreationMessage() != nil ||
		v.Message.GetPollCreationMessageV2() != nil ||
		v.Message.GetPollCreationMessageV3() != nil:
		bc.handlePollMessage(v)
	case v.Message.GetEventMessage() != nil:
		bc.handleEventMessage(v)
	default:
		bc.handleTextOrReaction(text, v, isEdited, isDocument)
	}
}

// ============================================================
// Individual media-type handlers (methods on bridgeContext)
// ============================================================

func (bc *bridgeContext) handleImageMessage(v *events.Message) {
	imageMsg := v.Message.GetImageMessage()
	if imageMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipImages {
		bc.sendFallbackText("\n<i>Skipping image because 'skip_images' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && imageMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the photo as it exceeds Telegram size restrictions.</i>")
		return
	}

	imageBytes, err := bc.waClient.Download(context.Background(), imageMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the photo due to some errors</i>")
		return
	}

	addCaption(&bc.bridgedText, imageMsg.GetCaption())

	if bc.cfg.Telegram.SendImagesAsFile {
		fileName := "image." + strings.Split(http.DetectContentType(imageBytes), "/")[1]
		sentMsg, _ := bc.tgBot.SendDocument(bc.cfg.Telegram.TargetChatID,
			&gotgbot.FileReader{Name: fileName, Data: bytes.NewReader(imageBytes)},
			&gotgbot.SendDocumentOpts{
				Caption:         bc.bridgedText,
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				MessageThreadId: bc.threadId,
			})
		bc.savePair(sentMsg)
		return
	}

	sentMsg, _ := bc.tgBot.SendPhoto(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Data: bytes.NewReader(imageBytes)},
		&gotgbot.SendPhotoOpts{
			Caption:         bc.bridgedText,
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			HasSpoiler:      imageMsg.GetViewOnce(),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleGifMessage(v *events.Message) {
	gifMsg := v.Message.GetVideoMessage()
	if gifMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipGIFs {
		bc.sendFallbackText("\n<i>Skipping GIF because 'skip_gifs' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && gifMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the GIF as it exceeds Telegram size restrictions.</i>")
		return
	}

	gifBytes, err := bc.waClient.Download(context.Background(), gifMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the GIF due to some errors</i>")
		return
	}

	addCaption(&bc.bridgedText, gifMsg.GetCaption())

	sentMsg, _ := bc.tgBot.SendAnimation(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Name: "animation.gif", Data: bytes.NewReader(gifBytes)},
		&gotgbot.SendAnimationOpts{
			Caption:         bc.bridgedText,
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleVideoMessage(v *events.Message) {
	var videoMsg *waE2E.VideoMessage
	isPTV := false
	if v.Message.GetVideoMessage() != nil {
		videoMsg = v.Message.GetVideoMessage()
	} else {
		videoMsg = v.Message.GetPtvMessage()
		isPTV = true
	}

	if videoMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipVideos {
		bc.sendFallbackText("\n<i>Skipping video because 'skip_videos' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && videoMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the video as it exceeds Telegram size restrictions.</i>")
		return
	}

	videoBytes, err := bc.waClient.Download(context.Background(), videoMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the video due to some errors</i>")
		return
	}

	addCaption(&bc.bridgedText, videoMsg.GetCaption())

	fileToSend := gotgbot.FileReader{
		Name: "video." + strings.Split(videoMsg.GetMimetype(), "/")[1],
		Data: bytes.NewReader(videoBytes),
	}

	var sentMsg *gotgbot.Message
	if isPTV {
		sentMsg, _ = bc.tgBot.SendVideoNote(bc.cfg.Telegram.TargetChatID, &fileToSend,
			&gotgbot.SendVideoNoteOpts{
				ReplyMarkup:     bc.replyMarkup,
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				MessageThreadId: bc.threadId,
			})
	} else {
		sentMsg, _ = bc.tgBot.SendVideo(bc.cfg.Telegram.TargetChatID, &fileToSend,
			&gotgbot.SendVideoOpts{
				Caption:         bc.bridgedText,
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				HasSpoiler:      videoMsg.GetViewOnce(),
				MessageThreadId: bc.threadId,
			})
	}
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleVoiceNoteMessage(v *events.Message) {
	audioMsg := v.Message.GetAudioMessage()
	if audioMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipVoiceNotes {
		bc.sendFallbackText("\n<i>Skipping voice note because 'skip_voice_notes' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the audio as it exceeds Telegram size restrictions.</i>")
		return
	}

	audioBytes, err := bc.waClient.Download(context.Background(), audioMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the audio due to some errors</i>")
		return
	}

	sentMsg, _ := bc.tgBot.SendAudio(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Name: "audio.ogg", Data: bytes.NewReader(audioBytes)},
		&gotgbot.SendAudioOpts{
			Caption:         bc.bridgedText,
			Duration:        int64(audioMsg.GetSeconds()),
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleAudioMessage(v *events.Message) {
	audioMsg := v.Message.GetAudioMessage()
	if audioMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipAudios {
		bc.sendFallbackText("\n<i>Skipping audio because 'skip_audios' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && audioMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the audio as it exceeds Telegram size restrictions.</i>")
		return
	}

	audioBytes, err := bc.waClient.Download(context.Background(), audioMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the audio due to some errors</i>")
		return
	}

	sentMsg, _ := bc.tgBot.SendAudio(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Name: "audio.m4a", Data: bytes.NewReader(audioBytes)},
		&gotgbot.SendAudioOpts{
			Caption:         bc.bridgedText,
			Duration:        int64(audioMsg.GetSeconds()),
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleDocumentMessage(v *events.Message) {
	documentMsg := v.Message.GetDocumentMessage()
	if documentMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipDocuments {
		bc.sendFallbackText("\n<i>Skipping document because 'skip_documents' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && documentMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the document as it exceeds Telegram size restrictions.</i>")
		return
	}

	documentBytes, err := bc.waClient.Download(context.Background(), documentMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the document due to some errors</i>")
		return
	}

	addCaption(&bc.bridgedText, documentMsg.GetCaption())

	sentMsg, _ := bc.tgBot.SendDocument(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Name: documentMsg.GetFileName(), Data: bytes.NewReader(documentBytes)},
		&gotgbot.SendDocumentOpts{
			Caption:         bc.bridgedText,
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleStickerMessage(v *events.Message) {
	stickerMsg := v.Message.GetStickerMessage()
	if stickerMsg.GetURL() == "" {
		return
	}

	if bc.cfg.WhatsApp.SkipStickers {
		bc.sendFallbackText("\n<i>Skipping sticker because 'skip_stickers' set in config file</i>")
		return
	}
	if !bc.cfg.Telegram.SelfHostedAPI && stickerMsg.GetFileLength() > utils.UploadSizeLimit {
		bc.sendFallbackText("\n<i>Couldn't send the sticker as it exceeds Telegram size restrictions.</i>")
		return
	}

	stickerBytes, err := bc.waClient.Download(context.Background(), stickerMsg)
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't download the sticker due to some errors</i>")
		return
	}

	// Send as file if configured
	if bc.cfg.Telegram.SendStickersAsFile {
		stickerExt := "webp"
		if mimeType := stickerMsg.GetMimetype(); mimeType != "" {
			if _, ext, ok := strings.Cut(mimeType, "/"); ok && ext != "" {
				stickerExt = ext
			}
		}
		sentMsg, _ := bc.tgBot.SendDocument(bc.cfg.Telegram.TargetChatID,
			&gotgbot.FileReader{Name: "sticker." + stickerExt, Data: bytes.NewReader(stickerBytes)},
			&gotgbot.SendDocumentOpts{
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				MessageThreadId: bc.threadId,
				ReplyMarkup:     bc.replyMarkup,
			})
		bc.savePair(sentMsg)
		return
	}

	// Animated / avatar sticker → try WEBM, then GIF, then raw
	if stickerMsg.GetIsAnimated() || stickerMsg.GetIsAvatar() {
		// Try WEBM conversion (preferred for animated stickers)
		if webmBytes, err := utils.AnimatedWebpConvertToWebm(stickerBytes, v.Info.ID); err == nil {
			sentMsg, _ := bc.tgBot.SendSticker(bc.cfg.Telegram.TargetChatID,
				&gotgbot.FileReader{Name: "sticker.webm", Data: bytes.NewReader(webmBytes)},
				&gotgbot.SendStickerOpts{
					ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
					MessageThreadId: bc.threadId,
					ReplyMarkup:     bc.replyMarkup,
				})
			bc.savePair(sentMsg)
			return
		}

		// Fallback: try GIF conversion
		if gifBytes, err := utils.AnimatedWebpConvertToGif(stickerBytes, v.Info.ID); err == nil {
			sentMsg, _ := bc.tgBot.SendAnimation(bc.cfg.Telegram.TargetChatID,
				&gotgbot.FileReader{Name: "animation.gif", Data: bytes.NewReader(gifBytes)},
				&gotgbot.SendAnimationOpts{
					Caption:         bc.bridgedText,
					ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
					MessageThreadId: bc.threadId,
					ReplyMarkup:     bc.replyMarkup,
				})
			bc.savePair(sentMsg)
			return
		}
	}

	// Static sticker or all conversions failed → send raw
	sentMsg, _ := bc.tgBot.SendSticker(bc.cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Data: bytes.NewReader(stickerBytes)},
		&gotgbot.SendStickerOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
			ReplyMarkup:     bc.replyMarkup,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleContactMessage(v *events.Message) {
	contactMsg := v.Message.GetContactMessage()

	if bc.cfg.WhatsApp.SkipContacts {
		bc.sendFallbackText("\n<i>Skipping contact because 'skip_contacts' set in config file</i>")
		return
	}

	decoder := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
	card, err := decoder.Decode()
	if err != nil {
		bc.sendFallbackText("\n<i>Couldn't send the vCard as failed to parse it</i>")
		return
	}

	sentMsg, _ := bc.tgBot.SendContact(bc.cfg.Telegram.TargetChatID,
		card.PreferredValue(goVCard.FieldTelephone), contactMsg.GetDisplayName(),
		&gotgbot.SendContactOpts{
			Vcard:           contactMsg.GetVcard(),
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
			ReplyMarkup:     bc.replyMarkup,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleContactsArrayMessage(v *events.Message) {
	contactsMsg := v.Message.GetContactsArrayMessage()

	if bc.cfg.WhatsApp.SkipContacts {
		bc.sendFallbackText("\n<i>Skipping contact array because 'skip_contacts' set in config file</i>")
		return
	}

	for _, contactMsg := range contactsMsg.Contacts {
		decoder := goVCard.NewDecoder(bytes.NewReader([]byte(contactMsg.GetVcard())))
		card, err := decoder.Decode()
		if err != nil {
			bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID,
				"Couldn't send the vCard as failed to parse it",
				&gotgbot.SendMessageOpts{
					ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
					MessageThreadId: bc.threadId,
				})
			continue
		}

		sentMsg, _ := bc.tgBot.SendContact(bc.cfg.Telegram.TargetChatID,
			card.PreferredValue(goVCard.FieldTelephone), contactMsg.GetDisplayName(),
			&gotgbot.SendContactOpts{
				Vcard:           contactMsg.GetVcard(),
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				MessageThreadId: bc.threadId,
				ReplyMarkup:     bc.replyMarkup,
			})
		bc.savePair(sentMsg)
	}
}

func (bc *bridgeContext) handleLocationMessage(v *events.Message) {
	locationMsg := v.Message.GetLocationMessage()

	if bc.cfg.WhatsApp.SkipLocations {
		bc.sendFallbackText("\n<i>Skipping location because 'skip_locations' set in config file</i>")
		return
	}

	sentMsg, _ := bc.tgBot.SendLocation(bc.cfg.Telegram.TargetChatID,
		locationMsg.GetDegreesLatitude(), locationMsg.GetDegreesLongitude(),
		&gotgbot.SendLocationOpts{
			HorizontalAccuracy: float64(locationMsg.GetAccuracyInMeters()),
			ReplyParameters:    utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId:    bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleLiveLocationMessage(v *events.Message) {
	bc.bridgedText += "\n<i>Shared their live location with you</i>"

	if bc.cfg.WhatsApp.SkipLocations {
		bc.sendFallbackText("\n<i>Skipping live location because 'skip_locations' set in config file</i>")
		return
	}

	sentMsg, _ := bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID, bc.bridgedText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handlePollMessage(v *events.Message) {
	var pollMsg *waE2E.PollCreationMessage
	if i := v.Message.GetPollCreationMessage(); i != nil {
		pollMsg = i
	} else if i := v.Message.GetPollCreationMessageV2(); i != nil {
		pollMsg = i
	} else if i := v.Message.GetPollCreationMessageV3(); i != nil {
		pollMsg = i
	}

	bc.bridgedText += "\n<i>It was the following poll:</i>\n\n"
	bc.bridgedText += fmt.Sprintf("<b>%s</b>: (%v options selectable)\n\n",
		html.EscapeString(pollMsg.GetName()), pollMsg.GetSelectableOptionsCount())

	for optionNum, option := range pollMsg.GetOptions() {
		if len(bc.bridgedText) > 4000 {
			bc.bridgedText += "\n... <i>Plus some other options</i>"
			break
		}
		bc.bridgedText += fmt.Sprintf("%v. %s\n", optionNum+1, html.EscapeString(option.GetOptionName()))
	}

	sentMsg, _ := bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID, bc.bridgedText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleEventMessage(v *events.Message) {
	bc.bridgedText += "<i>New event created</i>\n\n"
	eventMsg := v.Message.GetEventMessage()
	bc.bridgedText += "Name: " + html.EscapeString(eventMsg.GetName()) + "\n"
	if eventMsg.GetDescription() != "" {
		bc.bridgedText += "Description: " + html.EscapeString(eventMsg.GetDescription()) + "\n"
	}
	bc.bridgedText += "Start: " + time.Unix(eventMsg.GetStartTime(), 0).Format(bc.cfg.TimeFormat)
	if eventMsg.GetEndTime() != 0 {
		bc.bridgedText += "\nEnd: " + time.Unix(eventMsg.GetEndTime(), 0).Format(bc.cfg.TimeFormat)
	}
	if eventMsg.GetLocation() != nil {
		bc.bridgedText += "\nLocation: " + html.EscapeString(*eventMsg.GetLocation().Name) + "\n"
	}
	if eventMsg.GetJoinLink() != "" {
		bc.bridgedText += "Join link: " + html.EscapeString(eventMsg.GetJoinLink()) + "\n"
	}

	sentMsg, _ := bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID, bc.bridgedText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
			MessageThreadId: bc.threadId,
		})
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleTextOrReaction(text string, v *events.Message, isEdited bool, isDocument bool) {
	if text == "" {
		// Handle reactions
		if reactionMsg := v.Message.GetReactionMessage(); bc.cfg.Telegram.Reactions && reactionMsg != nil {
			bc.handleReaction(v, reactionMsg)
		}
		return
	}

	// Truncate very long text
	if len(text) > 4000 {
		bc.bridgedText += html.EscapeString(utils.SubString(text, 0, 4000)) + "..."
	} else {
		bc.bridgedText += html.EscapeString(text)
	}

	// Replace @mentions with links
	if mentioned := v.Message.GetExtendedTextMessage().GetContextInfo().GetMentionedJID(); mentioned != nil {
		for _, jid := range mentioned {
			parsedJid, _ := utils.WaParseJID(jid)
			name := utils.WaGetContactName(parsedJid)
			bc.bridgedText = strings.ReplaceAll(
				bc.bridgedText, "@"+parsedJid.User,
				fmt.Sprintf("<a href=\"https://wa.me/%s\">@%s</a>", parsedJid.User, html.EscapeString(name)),
			)
		}
	}

	// Send (edit-in-place or new message)
	var sentMsg *gotgbot.Message
	var err error

	if isEdited && !bc.cfg.WhatsApp.SendEditedMessageUpdates {
		if isDocument {
			sentMsg, _, err = bc.tgBot.EditMessageCaption(&gotgbot.EditMessageCaptionOpts{
				ChatId:    bc.cfg.Telegram.TargetChatID,
				MessageId: bc.replyToMsgId,
				Caption:   bc.bridgedText,
			})
		} else {
			sentMsg, _, err = bc.tgBot.EditMessageText(bc.bridgedText, &gotgbot.EditMessageTextOpts{
				ChatId:    bc.cfg.Telegram.TargetChatID,
				MessageId: bc.replyToMsgId,
			})
		}
	} else {
		sentMsg, err = bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID, bc.bridgedText,
			&gotgbot.SendMessageOpts{
				ReplyParameters: utils.TgMakeReplyParameters(bc.replyToMsgId, 0),
				MessageThreadId: bc.threadId,
			})
	}

	if err != nil {
		bc.logger.Error("failed to send telegram message",
			zap.String("event_id", v.Info.ID),
			zap.Error(err),
		)
		return
	}
	bc.savePair(sentMsg)
}

func (bc *bridgeContext) handleReaction(v *events.Message, reactionMsg *waE2E.ReactionMessage) {
	// Resolve LID → PN for chats using the new WhatsApp LID system
	waChatIdForLookup := v.Info.Chat.String()
	if v.Info.Chat.Server == waTypes.HiddenUserServer {
		pn, err := bc.waClient.Store.LIDs.GetPNForLID(context.Background(), v.Info.Chat.ToNonAD())
		if err != nil {
			bc.logger.Warn("failed to get PN for LID when handling reaction",
				zap.Error(err),
				zap.String("lid", v.Info.Chat.String()),
			)
		} else {
			waChatIdForLookup = pn.String()
		}
	}

	tgChatId, _, tgMsgId, err := database.MsgIdGetTgFromWa(reactionMsg.Key.GetID(), waChatIdForLookup)
	if err != nil {
		bc.logger.Error("failed to get message ID mapping from database",
			zap.Error(err),
			zap.String("stanza_id", reactionMsg.Key.GetID()),
			zap.String("chat_id", waChatIdForLookup),
		)
		return
	}

	if tgChatId != bc.cfg.Telegram.TargetChatID {
		return
	}

	var reactionText string
	if *reactionMsg.Text != "" {
		reactionText = fmt.Sprintf("<code>Reacted to this message with %s</code>",
			html.EscapeString(*reactionMsg.Text))
	} else {
		reactionText = "<code>Revoked their reaction to this message</code>"
	}
	bc.bridgedText += reactionText

	sentMsg, err := bc.tgBot.SendMessage(bc.cfg.Telegram.TargetChatID, bc.bridgedText,
		&gotgbot.SendMessageOpts{
			ReplyParameters: &gotgbot.ReplyParameters{MessageId: tgMsgId},
			MessageThreadId: bc.threadId,
		})
	if err != nil {
		bc.logger.Error("failed to send telegram reaction message",
			zap.String("event_id", v.Info.ID),
			zap.Error(err),
		)
		return
	}
	if sentMsg.MessageId != 0 {
		database.MsgIdAddNewPair(bc.msgId, bc.senderStr, waChatIdForLookup,
			bc.cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
	}
}

// ============================================================
// Undecryptable / View-Once messages
// ============================================================

func UndecryptableMessageEventHandler(v *events.UndecryptableMessage) {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
		tgBot  = state.State.TelegramBot
		msgId  = v.Info.ID
	)
	defer logger.Sync()

	if v.UnavailableType != events.UnavailableTypeViewOnce {
		return
	}
	if slices.Contains(cfg.WhatsApp.IgnoreChats, v.Info.Chat.User) {
		logger.Debug("returning because message from an ignored chat",
			zap.String("event_id", v.Info.ID),
			zap.String("chat_jid", v.Info.Chat.String()),
		)
		return
	}

	bridgedText := buildBridgedHeader(v.Info, cfg, false)
	bridgedText += "\n<i>It is a View Once message.\nPlease check in your official WhatsApp application</i>"

	threadId, err := resolveThreadId(v.Info, cfg, tgBot)
	if err != nil {
		utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0,
			fmt.Sprintf("failed to create/find thread id for '%s'", v.Info.Chat.String()), err)
		return
	}

	sentMsg, err := tgBot.SendMessage(cfg.Telegram.TargetChatID, bridgedText,
		&gotgbot.SendMessageOpts{MessageThreadId: threadId})
	if err != nil {
		logger.Error("failed to send telegram message for view-once notification",
			zap.String("event_id", v.Info.ID),
			zap.Error(err),
		)
		return
	}
	if sentMsg.MessageId != 0 {
		database.MsgIdAddNewPair(msgId, v.Info.MessageSource.Sender.String(), v.Info.Chat.String(),
			cfg.Telegram.TargetChatID, sentMsg.MessageId, sentMsg.MessageThreadId)
	}
}

// ============================================================
// Call events
// ============================================================

func CallOfferEventHandler(v *events.CallOffer) {
	var (
		cfg   = state.State.Config
		tgBot = state.State.TelegramBot
	)

	callerName := utils.WaGetContactName(v.CallCreator)
	callThreadId, err := utils.TgGetOrMakeThreadFromWa_String("calls", cfg.Telegram.TargetChatID, "Calls")
	if err != nil {
		utils.TgSendErrorById(tgBot, cfg.Telegram.TargetChatID, 0,
			"Failed to create/retreive corresponding thread id for calls", err)
		return
	}

	bridgeText := fmt.Sprintf(
		"#calls\n\n🧑: <b>%s</b>\n🕛: <b>%s</b>\n\n<i>You received a new call</i>",
		html.EscapeString(callerName),
		html.EscapeString(v.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)),
	)
	utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, callThreadId, bridgeText)
}

// ============================================================
// Receipts (delivered / read)
// ============================================================

func ReceiptEventHandler(v *events.Receipt) {
	participantID := v.Sender.ToNonAD().String()
	waChatID := v.Chat.ToNonAD().String()
	cfg := state.State.Config
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot

	for _, msgId := range v.MessageIDs {
		if participantID != "" {
			database.MsgReceiptUpsert(msgId, waChatID, participantID, v.Type, v.Timestamp)
		}
	}

	if v.Type == waTypes.ReceiptTypeReadSelf {
		for _, msgId := range v.MessageIDs {
			database.MsgIdMarkRead(waChatID, msgId)
		}
	}

	if !cfg.Telegram.AutoReactWhenAllRead || waClient == nil || tgBot == nil {
		return
	}
	if v.Type != waTypes.ReceiptTypeRead {
		return
	}
	if !v.IsGroup && participantID == "" {
		return
	}

	for _, msgId := range v.MessageIDs {
		autoReacted, err := database.MsgIdHasAutoReacted(waChatID, msgId)
		if err != nil || autoReacted {
			continue
		}

		tgChatId, _, tgMsgId, err := database.MsgIdGetTgFromWa(msgId, waChatID)
		if err != nil || tgChatId == 0 || tgMsgId == 0 {
			continue
		}

		expectedReaders := 1
		if v.IsGroup {
			groupInfo, err := waClient.GetGroupInfo(context.Background(), v.Chat.ToNonAD())
			if err != nil {
				continue
			}
			expectedReaders = 0
			for _, participant := range groupInfo.Participants {
				if participant.JID.ToNonAD().String() == waClient.Store.ID.ToNonAD().String() {
					continue
				}
				expectedReaders++
			}
		}

		receipts, err := database.MsgReceiptGetByMsg(msgId, waChatID)
		if err != nil {
			continue
		}

		readers := map[string]struct{}{}
		for _, receipt := range receipts {
			if receipt.ReceiptType != string(waTypes.ReceiptTypeRead) {
				continue
			}
			readers[receipt.ParticipantId] = struct{}{}
		}
		if len(readers) < expectedReaders {
			continue
		}

		_, err = tgBot.SetMessageReaction(tgChatId, tgMsgId,
			&gotgbot.SetMessageReactionOpts{
				Reaction: []gotgbot.ReactionType{gotgbot.ReactionTypeEmoji{Emoji: "👀"}},
			})
		if err != nil {
			continue
		}

		if cfg.Telegram.AutoReactRemoveAfter > 0 {
			go func(chatID, messageID, delaySeconds int64) {
				time.Sleep(time.Duration(delaySeconds) * time.Second)
				if state.State.TelegramBot == nil {
					return
				}
				state.State.TelegramBot.SetMessageReaction(chatID, messageID,
					&gotgbot.SetMessageReactionOpts{Reaction: []gotgbot.ReactionType{}})
			}(tgChatId, tgMsgId, cfg.Telegram.AutoReactRemoveAfter)
		}

		database.MsgIdMarkAutoReacted(waChatID, msgId)
	}
}

// ============================================================
// Push name / User about
// ============================================================

func PushNameEventHandler(v *events.PushName) {
	logger := state.State.Logger
	defer logger.Sync()

	logger.Debug("new push_name update",
		zap.String("jid", v.JID.String()),
		zap.String("old_push_name", v.OldPushName),
		zap.String("new_push_name", v.NewPushName),
	)
	database.ContactUpdatePushName(v.JID.User, v.JID.Server, v.NewPushName)
}

func UserAboutEventHandler(v *events.UserAbout) {
	var (
		cfg      = state.State.Config
		logger   = state.State.Logger
		tgBot    = state.State.TelegramBot
		waClient = state.State.WhatsAppClient
	)
	defer logger.Sync()

	logger.Debug("new user_about update",
		zap.String("jid", v.JID.String()),
		zap.String("new_status", v.Status),
		zap.Time("updated_at", v.Timestamp),
	)

	var (
		tgThreadId  int64       = 0
		threadFound bool        = false
		err         error       = nil
		pn          waTypes.JID = waTypes.EmptyJID
	)

	if v.JID.Server == waTypes.HiddenUserServer {
		pn, err = waClient.Store.LIDs.GetPNForLID(context.Background(), v.JID.ToNonAD())
		if err == nil {
			tgThreadId, threadFound, err = database.ChatThreadGetTgFromWa(pn.String(), cfg.Telegram.TargetChatID)
		}
	} else {
		tgThreadId, threadFound, err = database.ChatThreadGetTgFromWa(v.JID.ToNonAD().String(), cfg.Telegram.TargetChatID)
	}
	if err != nil {
		logger.Warn("failed to find thread for a WhatsApp chat (handling UserAbout event)",
			zap.String("chat", v.JID.String()), zap.Error(err))
		return
	}
	if !threadFound || tgThreadId == 0 {
		logger.Warn("no thread found for a WhatsApp chat (handling UserAbout event)",
			zap.String("chat", v.JID.String()))
		if !cfg.WhatsApp.CreateThreadForInfoUpdates {
			return
		}
	}

	tgThreadId, err = utils.TgGetOrMakeThreadFromWa(v.JID.ToNonAD(), cfg.Telegram.TargetChatID,
		utils.WaGetContactName(v.JID.ToNonAD()))
	if err != nil {
		logger.Warn("failed to create a new thread for a WhatsApp chat (handling UserAbout event)",
			zap.String("chat", v.JID.String()), zap.Error(err))
		return
	}

	updateMessageText := "User's about message was updated"
	if time.Since(v.Timestamp).Seconds() > 60 {
		updateMessageText += fmt.Sprintf(" at %s:\n\n",
			html.EscapeString(v.Timestamp.In(state.State.LocalLocation).Format(cfg.TimeFormat)))
	} else {
		updateMessageText += ":\n\n"
	}
	updateMessageText += fmt.Sprintf("<code>%s</code>", html.EscapeString(v.Status))

	tgBot.SendMessage(cfg.Telegram.TargetChatID, updateMessageText,
		&gotgbot.SendMessageOpts{MessageThreadId: tgThreadId})
}

// ============================================================
// Revoked messages
// ============================================================

func RevokedMessageEventHandler(v *events.Message) {
	var (
		cfg         = state.State.Config
		tgBot       = state.State.TelegramBot
		protocolMsg = v.Message.GetProtocolMessage()
		waMsgId     = protocolMsg.GetKey().GetID()
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

	tgBot.SendMessage(tgChatId,
		fmt.Sprintf("<i>This message was revoked by %s</i>", html.EscapeString(deleterName)),
		&gotgbot.SendMessageOpts{
			MessageThreadId: tgThreadId,
			ReplyParameters: &gotgbot.ReplyParameters{MessageId: tgMsgId},
		})
}

// ============================================================
// Profile picture updates
// ============================================================

func PictureEventHandler(v *events.Picture) {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
		tgBot  = state.State.TelegramBot
	)
	defer logger.Sync()

	switch v.JID.Server {
	case waTypes.GroupServer:
		handleGroupPictureEvent(v, cfg, logger, tgBot)
	case waTypes.DefaultUserServer, waTypes.HiddenUserServer:
		handleUserPictureEvent(v, cfg, logger, tgBot)
	default:
		logger.Warn("Received Picture event for unknown JID type",
			zap.String("jid", v.JID.String()))
	}
}

func handleGroupPictureEvent(v *events.Picture, cfg *state.Config, logger *zap.Logger, tgBot *gotgbot.Bot) {
	// Use the concrete client for proper typing
	client := state.State.WhatsAppClient
	tgThreadId, err := utils.TgGetOrMakeThreadFromWa(v.JID.ToNonAD(), cfg.Telegram.TargetChatID,
		utils.WaGetGroupName(v.JID))
	if err != nil {
		logger.Warn("failed to create a new thread for a WhatsApp chat (handling Picture event)",
			zap.String("chat", v.JID.String()), zap.Error(err))
		return
	}

	changer := utils.WaGetContactName(v.Author)

	if v.Remove {
		updateText := fmt.Sprintf("The profile picture was removed by %s", html.EscapeString(changer))
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message to the target chat", zap.Error(err))
		}
		return
	}

	sendUpdatedPicture(client, tgBot, cfg, logger, v.JID, tgThreadId,
		fmt.Sprintf("The profile picture was updated by %s", html.EscapeString(changer)))
}

func handleUserPictureEvent(v *events.Picture, cfg *state.Config, logger *zap.Logger, tgBot *gotgbot.Bot) {
	client := state.State.WhatsAppClient
	targetJID := v.JID.ToNonAD()
	threadName := utils.WaGetContactName(targetJID)

	if v.JID.Server == waTypes.HiddenUserServer {
		pn, pnErr := client.Store.LIDs.GetPNForLID(context.Background(), v.JID.ToNonAD())
		if pnErr == nil && pn.User != "" {
			targetJID = pn.ToNonAD()
			threadName = utils.WaGetContactName(targetJID)
		}
	}
	if threadName == "" {
		threadName = targetJID.String()
	}

	tgThreadId, err := utils.TgGetOrMakeThreadFromWa(targetJID, cfg.Telegram.TargetChatID, threadName)
	if err != nil {
		logger.Warn("failed to create a new thread for a WhatsApp chat (handling Picture event)",
			zap.String("chat", v.JID.String()), zap.Error(err))
		return
	}

	if v.Remove {
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId,
			"The profile picture was removed"); err != nil {
			logger.Error("failed to send message to the target chat", zap.Error(err))
		}
		return
	}

	sendUpdatedPicture(client, tgBot, cfg, logger, v.JID, tgThreadId, "The profile picture was updated")
}

func sendUpdatedPicture(client *whatsmeow.Client, tgBot *gotgbot.Bot, cfg *state.Config, logger *zap.Logger, jid waTypes.JID, tgThreadId int64, caption string) {
	pictureInfo, err := client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		logger.Error("failed to get profile picture info", zap.Error(err), zap.String("jid", jid.String()))
		return
	}
	if pictureInfo == nil {
		logger.Error("failed to get profile picture info, received null", zap.String("jid", jid.String()))
		return
	}

	newPictureBytes, err := utils.DownloadFileBytesByURL(pictureInfo.URL)
	if err != nil {
		logger.Error("failed to download profile picture", zap.Error(err), zap.String("jid", jid.String()))
		return
	}

	_, err = tgBot.SendPhoto(cfg.Telegram.TargetChatID,
		&gotgbot.FileReader{Data: bytes.NewReader(newPictureBytes)},
		&gotgbot.SendPhotoOpts{
			MessageThreadId: tgThreadId,
			Caption:         caption,
		})
	if err != nil {
		logger.Error("failed to send photo message", zap.Error(err))
	}
}

// ============================================================
// Group info changes
// ============================================================

func GroupInfoEventHandler(v *events.GroupInfo) {
	var (
		cfg      = state.State.Config
		logger   = state.State.Logger
		tgBot    = state.State.TelegramBot
		waClient = state.State.WhatsAppClient
	)
	defer logger.Sync()

	// Resolve existing thread
	var (
		tgThreadId  int64       = 0
		threadFound bool        = false
		err         error       = nil
		pn          waTypes.JID = waTypes.EmptyJID
	)

	if v.JID.Server == waTypes.HiddenUserServer {
		pn, err = waClient.Store.LIDs.GetPNForLID(context.Background(), v.JID.ToNonAD())
		if err == nil {
			tgThreadId, threadFound, err = database.ChatThreadGetTgFromWa(pn.String(), cfg.Telegram.TargetChatID)
		}
	} else {
		tgThreadId, threadFound, err = database.ChatThreadGetTgFromWa(v.JID.ToNonAD().String(), cfg.Telegram.TargetChatID)
	}
	if err != nil {
		logger.Warn("failed to find thread for a WhatsApp chat (handling GroupInfo event)",
			zap.String("chat", v.JID.String()), zap.Error(err))
		return
	}
	if !threadFound || tgThreadId == 0 {
		logger.Warn("no thread found for a WhatsApp chat (handling GroupInfo event)",
			zap.String("chat", v.JID.String()))
		if cfg.WhatsApp.CreateThreadForInfoUpdates {
			tgThreadId, err = utils.TgGetOrMakeThreadFromWa(v.JID.ToNonAD(), cfg.Telegram.TargetChatID,
				utils.WaGetGroupName(v.JID))
			if err != nil {
				logger.Warn("failed to create a new thread (handling GroupInfo event)",
					zap.String("chat", v.JID.String()), zap.Error(err))
				return
			}
		} else {
			return
		}
	}

	// Helper to get author display name
	authorName := func() string {
		if v.Sender != nil {
			return utils.WaGetContactName(*v.Sender)
		}
		return ""
	}
	authorSuffix := func() string {
		if name := authorName(); name != "" {
			return fmt.Sprintf(" by %s", html.EscapeString(name))
		}
		return ""
	}

	// Announce setting changed
	if v.Announce != nil {
		var updateText string
		if v.Announce.IsAnnounce {
			updateText = fmt.Sprintf("Group settings have been changed%s, only admins can send messages now", authorSuffix())
		} else {
			updateText = fmt.Sprintf("Group settings have been changed%s, everybody can send messages now", authorSuffix())
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Ephemeral setting changed
	if v.Ephemeral != nil {
		var updateText string
		if v.Ephemeral.IsEphemeral {
			err = database.UpdateEphemeralSettings(v.JID.ToNonAD().String(), true, v.Ephemeral.DisappearingTimer)
			updateText = fmt.Sprintf("Group's auto deletion timer has been turned on%s:\n", authorSuffix())
			updateText += fmt.Sprintf("Timer: %s\n", time.Second*time.Duration(v.Ephemeral.DisappearingTimer))
			if err != nil {
				updateText += fmt.Sprintf("Failed to save to DB: %s", html.EscapeString(err.Error()))
			}
		} else {
			err = database.UpdateEphemeralSettings(v.JID.ToNonAD().String(), false, 0)
			updateText = fmt.Sprintf("Group's auto deletion timer has been disabled%s:\n", authorSuffix())
			if err != nil {
				updateText += fmt.Sprintf("Failed to save to DB: %s", html.EscapeString(err.Error()))
			}
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Group deleted
	if v.Delete != nil {
		updateText := fmt.Sprintf("The group has been deleted%s", authorSuffix())
		if v.Delete.DeleteReason != "" {
			updateText += fmt.Sprintf("\nReason: <code>%s</code>", html.EscapeString(v.Delete.DeleteReason))
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Skip member join/leave for ignored chats
	if slices.Contains(cfg.WhatsApp.IgnoreChats, v.JID.ToNonAD().User) {
		logger.Debug("returning because message from an ignored chat",
			zap.String("chat_jid", v.JID.String()))
		return
	}

	// Members joined
	if len(v.Join) > 0 {
		adderName := authorName()
		var updateText string
		if len(v.Join) == 1 {
			newMemName := utils.WaGetContactName(v.Join[0])
			if v.Sender != nil && *v.Sender != v.Join[0] {
				updateText = fmt.Sprintf("%s was added by %s to the group\n",
					html.EscapeString(newMemName), html.EscapeString(adderName))
			} else {
				updateText = fmt.Sprintf("%s joined the group\n", html.EscapeString(newMemName))
			}
		} else {
			updateText = "The following people joined the group:\n"
			for _, newMem := range v.Join {
				newMemName := utils.WaGetContactName(newMem)
				if v.Sender != nil && *v.Sender != newMem {
					updateText += fmt.Sprintf("- %s (added by %s)\n",
						html.EscapeString(newMemName), html.EscapeString(adderName))
				} else {
					updateText += fmt.Sprintf("- %s\n", html.EscapeString(newMemName))
				}
			}
		}
		if v.JoinReason != "" {
			updateText += fmt.Sprintf("\nReason: %s", html.EscapeString(v.JoinReason))
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Members left
	if len(v.Leave) > 0 {
		removerName := authorName()
		var updateText string
		if len(v.Leave) == 1 {
			oldMemName := utils.WaGetContactName(v.Leave[0])
			if v.Sender != nil && *v.Sender == v.Leave[0] {
				updateText = fmt.Sprintf("%s left the group\n", html.EscapeString(oldMemName))
			} else {
				updateText = fmt.Sprintf("%s was kicked by %s from the group\n",
					html.EscapeString(oldMemName), html.EscapeString(removerName))
			}
		} else {
			updateText = "The following people left the group:\n"
			for _, oldMem := range v.Leave {
				oldMemName := utils.WaGetContactName(oldMem)
				if v.Sender != nil && *v.Sender != oldMem {
					updateText += fmt.Sprintf("- %s (kicked by %s)\n",
						html.EscapeString(oldMemName), html.EscapeString(removerName))
				} else {
					updateText += fmt.Sprintf("- %s\n", html.EscapeString(oldMemName))
				}
			}
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Members demoted
	if len(v.Demote) > 0 {
		demoterName := authorName()
		var updateText string
		if len(v.Demote) == 1 {
			updateText = fmt.Sprintf("%s was demoted in the group", html.EscapeString(utils.WaGetContactName(v.Demote[0])))
			if demoterName != "" {
				updateText += fmt.Sprintf(" by %s", html.EscapeString(demoterName))
			}
			updateText += "\n"
		} else {
			updateText = "The following people were demoted"
			if demoterName != "" {
				updateText += fmt.Sprintf(" by %s", html.EscapeString(demoterName))
			}
			updateText += ":\n"
			for _, demotedMem := range v.Demote {
				updateText += fmt.Sprintf("- %s\n", utils.WaGetContactName(demotedMem))
			}
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Members promoted
	if len(v.Promote) > 0 {
		promoterName := authorName()
		var updateText string
		if len(v.Promote) == 1 {
			updateText = fmt.Sprintf("%s was promoted in the group", html.EscapeString(utils.WaGetContactName(v.Promote[0])))
			if promoterName != "" {
				updateText += fmt.Sprintf(" by %s", html.EscapeString(promoterName))
			}
			updateText += "\n"
		} else {
			updateText = "The following people were promoted"
			if promoterName != "" {
				updateText += fmt.Sprintf(" by %s", html.EscapeString(promoterName))
			}
			updateText += ":\n"
			for _, promotedMem := range v.Promote {
				updateText += fmt.Sprintf("- %s\n", html.EscapeString(utils.WaGetContactName(promotedMem)))
			}
		}
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Group description changed
	if v.Topic != nil {
		changer := utils.WaGetContactName(v.Topic.TopicSetBy)
		updateText := fmt.Sprintf(
			"The group description was changed by <b>%s</b>:\n\n<code>%s</code>",
			html.EscapeString(changer), html.EscapeString(v.Topic.Topic))
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}

	// Group name changed
	if v.Name != nil {
		_, err = tgBot.EditForumTopic(cfg.Telegram.TargetChatID, tgThreadId,
			&gotgbot.EditForumTopicOpts{Name: v.Name.Name})
		if err != nil {
			logger.Error("failed to change thread name",
				zap.Error(err),
				zap.String("chat", v.JID.String()),
				zap.String("new_name", v.Name.Name))
			return
		}
		changer := utils.WaGetContactName(v.Name.NameSetBy)
		updateText := fmt.Sprintf(
			"The group name was changed by <b>%s</b>:\n\n<code>%s</code>",
			html.EscapeString(changer), html.EscapeString(v.Name.Name))
		if err := utils.TgSendTextById(tgBot, cfg.Telegram.TargetChatID, tgThreadId, updateText); err != nil {
			logger.Error("failed to send message", zap.Error(err))
		}
	}
}

// ============================================================
// Logout
// ============================================================

func LogoutHandler(v *events.LoggedOut) {
	var (
		cfg    = state.State.Config
		logger = state.State.Logger
		tgBot  = state.State.TelegramBot
	)
	defer logger.Sync()

	updateText := "You have been logged out from WhatsApp:\n\n"
	updateText += fmt.Sprintf("<b>Reason:</b> %s", html.EscapeString(v.Reason.String()))
	utils.TgSendTextById(tgBot, cfg.Telegram.OwnerID, 0, updateText)
}
