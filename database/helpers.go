package database

import (
	"database/sql"

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
		bridgePair.MarkRead = sql.NullBool{Valid: true, Bool: false}
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
		MarkRead:      sql.NullBool{Valid: true, Bool: false},
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

func MsgIdGetUnread(waChatId string) (map[string]([]string), error) {

	db := state.State.Database

	var bridgePairs []MsgIdPair
	res := db.Where("wa_chat_id = ? AND mark_read = false", waChatId).Find(&bridgePairs)

	var msgIds = make(map[string]([]string))

	for _, pair := range bridgePairs {
		if _, found := msgIds[pair.ParticipantId]; !found {
			msgIds[pair.ParticipantId] = []string{}
		}
		msgIds[pair.ParticipantId] = append(msgIds[pair.ParticipantId], pair.ID)
	}

	return msgIds, res.Error
}

func MsgIdMarkRead(waChatId, waMsgId string) error {

	db := state.State.Database

	var bridgePair MsgIdPair
	res := db.Where("id = ? AND wa_chat_id = ?", waMsgId, waChatId).Find(&bridgePair)
	if res.Error != nil {
		return res.Error
	}

	if bridgePair.ID == waMsgId {
		bridgePair.MarkRead = sql.NullBool{Valid: true, Bool: true}
		res = db.Save(&bridgePair)
		return res.Error
	}

	return nil
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

func ChatThreadDropPairByTg(tgChatId, tgThreadId int64) error {

	db := state.State.Database

	res := db.Where("tg_chat_id = ? AND tg_thread_id = ?", tgChatId, tgThreadId).Delete(&ChatThreadPair{})

	return res.Error
}

func ChatThreadGetWaFromTg(tgChatId, tgThreadId int64) (string, error) {

	db := state.State.Database

	var chatPair ChatThreadPair
	res := db.Where("tg_chat_id = ? AND tg_thread_id = ?", tgChatId, tgThreadId).Find(&chatPair)

	return chatPair.ID, res.Error
}

func ChatThreadGetAllPairs(tgChatId int64) ([]ChatThreadPair, error) {

	db := state.State.Database

	var chatPairs []ChatThreadPair
	res := db.Where("tg_chat_id = ?", tgChatId).Find(&chatPairs)

	return chatPairs, res.Error
}

func ChatThreadDropAllPairs() error {

	db := state.State.Database
	res := db.Where("1 = 1").Delete(&ChatThreadPair{})

	return res.Error
}

func ContactNameAddNew(waUserId, waUserServer, firstName, fullName, pushName, businessName string) error {
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
		Server:       waUserServer,
	})
	return res.Error
}

func ContactNameBulkAddOrUpdate(contacts map[types.JID]types.ContactInfo) error {

	var (
		db           = state.State.Database
		contactNames []ContactName
	)

	for k, v := range contacts {
		contactNames = append(contactNames, ContactName{
			ID:           k.User,
			FirstName:    v.FirstName,
			PushName:     v.PushName,
			BusinessName: v.BusinessName,
			FullName:     v.FullName,
			Server:       k.Server,
		})
	}

	res := db.Save(&contactNames)
	if res.Error != nil {
		return res.Error
	}

	return nil
}

func ContactNameGet(waUserId string, waUserServer string) (string, string, string, string, bool, error) {

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ? AND server = ?", waUserId, waUserServer).Find(&contact)

	found := (contact.ID == waUserId && contact.Server == waUserServer)

	return contact.FirstName, contact.FullName, contact.PushName, contact.BusinessName, found, res.Error
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

func ContactUpdatePushName(waUserId, waUserServer, pushName string) error {
	if pushName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ? AND server = ?", waUserId, waUserServer).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, waUserServer, "", "", pushName, "")
	}

	contact.PushName = pushName
	res = db.Save(&contact)

	return res.Error
}

func ContactUpdateFullName(waUserId, waUserServer, fullName string) error {
	if fullName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ? AND server = ?", waUserId, waUserServer).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, waUserServer, "", fullName, "", "")
	}

	contact.FullName = fullName
	res = db.Save(&contact)

	return res.Error
}

func ContactUpdateBusinessName(waUserId, waUserServer, businessName string) error {
	if businessName == "" {
		return nil
	}

	db := state.State.Database

	var contact ContactName
	res := db.Where("id = ? AND server = ?", waUserId, waUserServer).Find(&contact)

	if res.Error != nil {
		return res.Error
	}

	if contact.ID != waUserId {
		return ContactNameAddNew(waUserId, waUserServer, "", "", "", businessName)
	}

	contact.BusinessName = businessName
	res = db.Save(&contact)

	return res.Error
}

func UpdateEphemeralSettings(waChatId string, isEphemeral bool, ephemeralTimer uint32) error {
	db := state.State.Database

	var settings ChatEphemeralSettings
	res := db.Where("id = ?", waChatId).Find(&settings)

	if res.Error != nil {
		return res.Error
	}

	if settings.ID != waChatId {
		res = db.Create(&ChatEphemeralSettings{
			ID:             waChatId,
			IsEphemeral:    isEphemeral,
			EphemeralTimer: ephemeralTimer,
		})
		return res.Error
	}

	settings.IsEphemeral = isEphemeral
	settings.EphemeralTimer = ephemeralTimer

	res = db.Save(&settings)

	return res.Error
}

func GetEphemeralSettings(waChatId string) (bool, uint32, bool, error) {
	db := state.State.Database

	var settings ChatEphemeralSettings
	res := db.Where("id = ?", waChatId).Find(&settings)

	if res.Error != nil {
		return false, 0, false, res.Error
	}

	if settings.ID != waChatId {
		return false, 0, false, nil
	}

	return settings.IsEphemeral, settings.EphemeralTimer, true, nil
}
