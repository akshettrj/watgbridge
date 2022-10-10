package utils

import (
	"strings"
	"wa-tg-bridge/state"

	"go.mau.fi/whatsmeow/types"
)

func WhatsAppParseJID(s string) (types.JID, bool) {
	if s[0] == '+' {
		s = s[1:]
	}

	if !strings.ContainsRune(s, '@') {
		return types.NewJID(s, types.DefaultUserServer), true
	}

	recipient, err := types.ParseJID(s)
	if err != nil {
		return recipient, false
	} else if recipient.User == "" {
		return recipient, false
	}
	return recipient, true
}

func WhatsAppGetContactName(jid types.JID) string {
	waClient := state.State.WhatsAppClient

	contact, err := waClient.Store.Contacts.GetContact(jid)
	if err != nil || !contact.Found {
		return jid.User
	}

	var name string
	if contact.FullName != "" {
		name = contact.FullName
	} else if contact.BusinessName != "" {
		name = contact.BusinessName
	} else if contact.PushName != "" {
		name = contact.PushName
	}

	if name == "" {
		name = jid.User
	} else {
		name += (" [ " + jid.User + " ]")
	}

	return name
}

func WhatsAppGetGroupName(jid types.JID) string {
	waClient := state.State.WhatsAppClient

	groupInfo, err := waClient.GetGroupInfo(jid)
	if err != nil {
		return ""
	}

	return groupInfo.Name
}
