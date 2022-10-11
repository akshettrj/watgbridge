package utils

import (
	"fmt"
	"strings"

	"wa-tg-bridge/state"

	"github.com/lithammer/fuzzysearch/fuzzy"
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
		return jid.String()
	}

	return groupInfo.Name
}

func WhatsAppFindContact(query string) (map[string]string, error) {
	waClient := state.State.WhatsAppClient

	var results = make(map[string]string)

	contacts, err := waClient.Store.Contacts.GetAllContacts()
	if err != nil {
		return nil, err
	}

	var contactsInfo []string
	for jid, contact := range contacts {
		contactsInfo = append(contactsInfo, fmt.Sprintf("%s||%s||%s||%s",
			jid.String(), contact.FullName, contact.BusinessName, contact.FirstName))
	}

	fuzzyResults := fuzzy.Find(query, contactsInfo)
	for _, res := range fuzzyResults {
		info := strings.Split(res, "||")
		name := ""
		if len(info[1]) != 0 {
			name += info[1]
		}
		if len(info[2]) != 0 {
			if len(name) == 0 {
				name += info[2]
			} else {
				name += (", " + info[2])
			}
		}
		if len(info[3]) != 0 {
			if len(name) == 0 {
				name += info[3]
			} else {
				name += (", " + info[3])
			}
		}
		results[info[0]] = name
	}

	return results, nil
}
