package database

import (
	"watgbridge/state"

	"go.mau.fi/whatsmeow/types"
)

func MsgIdAddNewPair(waMsgId, participantId, waChatId string, tgChatId, tgMsgId, tgThreadId int64) error {

	db := state.State.Database

	var bridgePair MsgIdPair
	res := db.Where("id = ? AND wa_chat_id = ?", waMsgId, waChatId).Find(&bridgePair)
	if res.Error != nil {
		return res.Error
	}

	if bridgePair.ID == waMsgId {
		bridgePair.ParticipantId = participantId
		bridgePair.WaChatId = waChatId
		bridgePair.TgChatId = tgChatId
		bridgePair.TgMsgId = tgMsgId
		bridgePair.TgThreadId = tgThreadId
		res = db.Save(&bridgePair)
		return res.Error
	}
	// else
	res = db.Create(&MsgIdPair{
		ID:            waMsgId,
		ParticipantId: participantId,
		WaChatId:      waChatId,
		TgChatId:      tgChatId,
		TgMsgId:       tgMsgId,
		TgThreadId:    tgThreadId,
	})
	return res.Error
}

func MsgIdGetTgFromWa(waMsgId, waChatId string) (int64, int64, int64, error) {

	db := state.State.Database

	var bridgePair MsgIdPair
	res := db.Where("id = ? AND wa_chat_id = ?", waMsgId, waChatId).Find(&bridgePair)

	return bridgePair.TgChatId, bridgePair.TgThreadId, bridgePair.TgMsgId, res.Error
}

func MsgIdGetWaFromTg(tgChatId, tgMsgId, tgThreadId int64) (msgId, participantId, chatId string, err error) {

	db := state.State.Database

	var bridgePair MsgIdPair
	res := db.Where("tg_chat_id = ? AND tg_msg_id = ? AND tg_thread_id = ?", tgChatId, tgMsgId, tgThreadId).Find(&bridgePair)

	return bridgePair.ID, bridgePair.ParticipantId, bridgePair.WaChatId, res.Error
}

func MsgIdDeletePair(tgChatId, tgMsgId int64) error {

	db := state.State.Database
	res := db.Where("tg_chat_id = ? AND tg_msg_id = ?", tgChatId, tgMsgId).Delete(&MsgIdPair{})

	return res.Error
}

func MsgIdDropAllPairs() error {

	db := state.State.Database
	res := db.Where("1 = 1").Delete(&MsgIdPair{})

	return res.Error
}

func ChatThreadAddNewPair(waChatId string, tgChatId, tgThreadId int64) error {

	db := state.State.Database

	var chatPair ChatThreadPair
	res := db.Where("id = ? AND tg_chat_id = ?", waChatId, tgChatId).Find(&chatPair)
	if res.Error != nil {
		return res.Error
	}

	if chatPair.ID == waChatId {
		chatPair.ID = waChatId
		chatPair.TgChatId = tgChatId
		chatPair.TgThreadId = tgThreadId
		res = db.Save(&chatPair)
		return res.Error
	}
	// else
	res = db.Create(&ChatThreadPair{
		ID:         waChatId,
		TgChatId:   tgChatId,
		TgThreadId: tgThreadId,
	})
	return res.Error
}

func ChatThreadGetTgFromWa(waChatId string, tgChatId int64) (int64, bool, error) {

	db := state.State.Database

	var chatPair ChatThreadPair
	res := db.Where("id = ? AND tg_chat_id = ?", waChatId, tgChatId).Find(&chatPair)

	found := (chatPair.ID == waChatId && chatPair.TgChatId == tgChatId)
	return chatPair.TgThreadId, found, res.Error
}

func ChatThreadGetWaFromTg(tgChatId, tgThreadId int64) (string, error) {

	db := state.State.Database

	var chatPair ChatThreadPair
	res := db.Where("tg_chat_id = ? AND tg_thread_id = ?", tgChatId, tgThreadId).Find(&chatPair)

	return chatPair.ID, res.Error
}

func ChatThreadDropAllPairs() error {

	db := state.State.Database
	res := db.Where("1 = 1").Delete(&ChatThreadPair{})

	return res.Error
}

func ContactNameAddNew(waUserId, firstName, fullName, pushName, businessName string) error {
	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ?", waUserId).Find(&contact)
	if res.Error != nil {
		return res.Error
	}

	if contact.ID == waUserId {
		contact.FirstName = firstName
		contact.FullName = fullName
		contact.PushName = pushName
		contact.BusinessName = businessName
		res = db.Save(&contact)
		return res.Error
	}
	// else
	res = db.Create(&ContactName{
		ID:           waUserId,
		FirstName:    firstName,
		FullName:     fullName,
		PushName:     pushName,
		BusinessName: businessName,
	})
	return res.Error
}

func ContactNameBulkAddOrUpdate(contacts map[types.JID]types.ContactInfo) error {

	db := state.State.Database

	var (
		contactNamesMap = make(map[string]*ContactName)
		contactNames    []ContactName
		toAdd           []ContactName
		toUpdate        []ContactName
	)

	res := db.Limit(-1).Find(&contactNames)
	if res.Error != nil {
		return res.Error
	}

	for i := 0; i < len(contactNames); i++ {
		contactNamesMap[contactNames[i].ID] = &contactNames[i]
	}

	for jid, contact := range contacts {
		if c := contactNamesMap[jid.User]; c != nil {
			c.FirstName = contact.FirstName
			c.PushName = contact.PushName
			c.BusinessName = contact.BusinessName
			c.FullName = contact.FullName
			toUpdate = append(toUpdate, *c)
		} else {
			toAdd = append(toAdd, ContactName{
				ID:           jid.User,
				FirstName:    contact.FirstName,
				PushName:     contact.PushName,
				BusinessName: contact.BusinessName,
				FullName:     contact.FullName,
			})
		}
	}

	if len(toAdd) > 0 {
		db.Create(&toAdd)
	}
	if len(toUpdate) > 0 {
		db.Save(&toUpdate)
	}

	return nil
}

func ContactNameGet(waUserId string) (string, string, string, string, error) {

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ?", waUserId).Find(&contact)

	return contact.FirstName, contact.FullName, contact.PushName, contact.BusinessName, res.Error
}

func ContactGetAll() (map[string]ContactName, error) {

	db := state.State.Database

	var contacts []ContactName
	res := db.Where("1 = 1").Limit(-1).Find(&contacts)

	results := make(map[string]ContactName)
	for _, contact := range contacts {
		results[contact.ID] = contact
	}
	return results, res.Error
}

func ContactUpdatePushName(waUserId, pushName string) error {
	if pushName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ?", waUserId).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, "", "", pushName, "")
	}

	contact.PushName = pushName
	res = db.Save(&contact)

	return res.Error
}

func ContactUpdateFullName(waUserId, fullName string) error {
	if fullName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ?", waUserId).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, "", fullName, "", "")
	}

	contact.FullName = fullName
	res = db.Save(&contact)

	return res.Error
}

func ContactUpdateBusinessName(waUserId, businessName string) error {
	if businessName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ?", waUserId).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, "", "", "", businessName)
	}

	contact.BusinessName = businessName
	res = db.Save(&contact)

	return res.Error
}
