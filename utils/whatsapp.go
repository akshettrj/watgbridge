package utils

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log"
	"strings"

	"watgbridge/database"
	"watgbridge/state"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// ChatThreadLookupByWaJID finds chat_thread_pairs for a WA chat in the target Telegram supergroup.
// Rows may be keyed by phone JID or by LID (@lid); tries the native key, LID→PN, and PN→LID.
// When found, waDbKey is the primary key stored in the DB (for metadata updates).
func ChatThreadLookupByWaJID(ctx context.Context, waChat types.JID, tgTargetChatId int64) (tgThreadId int64, waDbKey string, err error) {
	waChat = waChat.ToNonAD()
	try := func(key string) (int64, bool, error) {
		tid, ok, e := database.ChatThreadGetTgFromWa(key, tgTargetChatId)
		if e != nil {
			return 0, false, e
		}
		if ok && tid != 0 {
			return tid, true, nil
		}
		return 0, false, nil
	}

	if tid, ok, e := try(waChat.String()); e != nil {
		return 0, "", e
	} else if ok {
		return tid, waChat.String(), nil
	}

	waClient := state.State.WhatsAppClient
	if waClient == nil {
		return 0, "", nil
	}

	if waChat.Server == types.HiddenUserServer {
		if pn, e2 := waClient.Store.LIDs.GetPNForLID(ctx, waChat); e2 == nil && !pn.IsEmpty() {
			pn = pn.ToNonAD()
			if tid, ok, e := try(pn.String()); e != nil {
				return 0, "", e
			} else if ok {
				return tid, pn.String(), nil
			}
		}
	}

	if waChat.Server == types.DefaultUserServer || waChat.Server == types.LegacyUserServer {
		if lid, e2 := waClient.Store.LIDs.GetLIDForPN(ctx, waChat); e2 == nil && !lid.IsEmpty() {
			lid = lid.ToNonAD()
			if tid, ok, e := try(lid.String()); e != nil {
				return 0, "", e
			} else if ok {
				return tid, lid.String(), nil
			}
		}
	}

	return 0, "", nil
}

// WaBestJIDForOutgoingChat prefers the phone JID for 1:1 sends when the mapped chat is @lid.
func WaBestJIDForOutgoingChat(ctx context.Context, chat types.JID) types.JID {
	chat = chat.ToNonAD()
	if chat.Server != types.HiddenUserServer {
		return chat
	}
	waClient := state.State.WhatsAppClient
	if waClient == nil {
		return chat
	}
	pn, err := waClient.Store.LIDs.GetPNForLID(ctx, chat)
	if err != nil || pn.IsEmpty() {
		return chat
	}
	return pn.ToNonAD()
}

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

// WaTelegramGroupTopicPrefix is prepended to WhatsApp group names in Telegram forum topic titles.
const WaTelegramGroupTopicPrefix = "GROUP: "

// WaTelegramGroupTopicTitle formats a WhatsApp group's display name for a Telegram forum topic.
func WaTelegramGroupTopicTitle(groupName string) string {
	name := strings.TrimSpace(groupName)
	if name == "" {
		name = "Unknown"
	}
	return WaTelegramGroupTopicPrefix + name
}

// WaSourceDisplayNameForMetadata is the WhatsApp-side title shown in the pinned topic card
// (not the Telegram forum topic title; updates when you sync from WA).
func WaSourceDisplayNameForMetadata(jid types.JID) string {
	jid = jid.ToNonAD()
	if jid.Server == types.GroupServer {
		return WaGetGroupName(jid)
	}
	return WaGetForumTopicName(jid)
}

// WaChatDialogCreatedAt returns the WhatsApp chat creation time when known (groups only).
func WaChatDialogCreatedAt(ctx context.Context, jid types.JID) sql.NullTime {
	jid = jid.ToNonAD()
	if jid.Server != types.GroupServer {
		return sql.NullTime{}
	}
	waClient := state.State.WhatsAppClient
	gi, err := waClient.GetGroupInfo(ctx, jid)
	if err != nil || gi.GroupCreated.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: gi.GroupCreated.UTC(), Valid: true}
}

// TopicMetadataIsWAChatKey is true for 1:1 and group chats that get a metadata pin (not system pseudo-topics).
func TopicMetadataIsWAChatKey(waChatKey string) bool {
	if waChatKey == "calls" || waChatKey == "mentions" || waChatKey == "status@broadcast" {
		return false
	}
	j, ok := WaParseJID(waChatKey)
	if !ok || j.IsEmpty() {
		return false
	}
	switch j.Server {
	case types.GroupServer, types.DefaultUserServer, types.LegacyUserServer:
		return true
	default:
		return false
	}
}

// WaGetPhoneForDisplay returns the phone number string for listing (e.g. +77001234567).
// Resolves LID to phone number when contact is stored with HiddenUserServer.
// Uses Redis cache when configured to avoid spamming WhatsApp.
func WaGetPhoneForDisplay(id, server string) string {
	if server != types.HiddenUserServer {
		return "+" + id
	}
	ctx := context.Background()
	rdb := state.State.RedisClient
	if rdb != nil {
		key := state.LIDToPhoneKeyPrefix + id
		phone, err := rdb.Get(ctx, key).Result()
		if err == nil {
			return "+" + phone
		}
	}
	waClient := state.State.WhatsAppClient
	jid := types.NewJID(id, server)
	pn, err := waClient.Store.LIDs.GetPNForLID(ctx, jid)
	if err != nil {
		return "+" + id
	}
	if rdb != nil {
		key := state.LIDToPhoneKeyPrefix + id
		_ = rdb.Set(ctx, key, pn.User, 0).Err()
	}
	return "+" + pn.User
}

// waContactBaseLabel returns a human-readable name without appending raw WA user id
// (avoids "Bruh (52352010694781)"-style titles when that id is an opaque LID).
func waContactBaseLabel(jid types.JID) string {
	if jid.ToNonAD() == state.State.WhatsAppClient.Store.ID.ToNonAD() {
		return "You"
	}
	waClient := state.State.WhatsAppClient
	lookupJID := jid.ToNonAD()
	var firstName, fullName, pushName, businessName string
	var found bool
	var err error
	if jid.Server == types.HiddenUserServer {
		pn, e := waClient.Store.LIDs.GetPNForLID(context.Background(), jid)
		if e != nil {
			return ""
		}
		lookupJID = pn.ToNonAD()
		firstName, fullName, pushName, businessName, found, err = database.ContactNameGet(pn.User, pn.Server)
	} else {
		firstName, fullName, pushName, businessName, found, err = database.ContactNameGet(jid.User, jid.Server)
	}
	if err == nil && found {
		if fullName != "" {
			return fullName
		}
		if businessName != "" {
			return businessName
		}
		if pushName != "" {
			return pushName
		}
		if firstName != "" {
			return firstName
		}
	}
	contact, err := waClient.Store.Contacts.GetContact(context.Background(), lookupJID)
	if err == nil && contact.Found {
		if contact.FullName != "" {
			return contact.FullName
		}
		if contact.BusinessName != "" {
			return contact.BusinessName
		}
		if contact.PushName != "" {
			return contact.PushName
		}
		if contact.FirstName != "" {
			return contact.FirstName
		}
	}
	return ""
}

// WaSenderHTMLBlock returns a line like 👤 Name (+phone) for group/status headers.
func WaSenderHTMLBlock(jid types.JID) string {
	j := jid.ToNonAD()
	name := html.EscapeString(WaGetContactName(j))
	phone := html.EscapeString(WaGetPhoneForDisplay(j.User, j.Server))
	return fmt.Sprintf("👤 %s (%s)", name, phone)
}

// WaGetForumTopicName is used for Telegram forum topic titles: private chats get
// "DisplayName (+phone)" (LID→PN when possible); groups get "GROUP: <fetched name>".
func WaGetForumTopicName(jid types.JID) string {
	if jid.Server == types.GroupServer {
		return WaTelegramGroupTopicTitle(WaGetGroupName(jid))
	}
	if jid.ToNonAD() == state.State.WhatsAppClient.Store.ID.ToNonAD() {
		return "You"
	}
	phone := WaGetPhoneForDisplay(jid.User, jid.Server)
	base := waContactBaseLabel(jid)
	if base == "" {
		return phone
	}
	if strings.TrimPrefix(phone, "+") == base || phone == base {
		return phone
	}
	return fmt.Sprintf("%s (%s)", base, phone)
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

	_, err = waClient.SendMessage(context.Background(), group, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(replyText),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(msgId),
				Participant:   proto.String(msgSender),
				QuotedMessage: msg,
				MentionedJID:  mentioned,
			},
		},
	})
	if err != nil {
		log.Printf("[whatsapp] failed to reply to '@all/@everyone': %s\n", err)
		return
	}

	if !msgIsFromMe {
		tagsThreadId, _, err := TgGetOrMakeThreadFromWa_String("mentions", cfg.Telegram.TargetChatID, "Mentions")
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
