package database

import "wa-tg-bridge/state"

func AddNewWaToTgPair(waMsgId, participant, waChat string, tgChatId, tgMsgId int64) error {
	db := state.State.Database
	var bridgePair MsgIdPairs
	db.Where("id = ? AND wa_chat = ?", waMsgId, waChat).Find(&bridgePair)
	if bridgePair.ID == waMsgId {
		bridgePair.Participant = participant
		bridgePair.WaChat = waChat
		bridgePair.TgChatId = tgChatId
		bridgePair.TgMsgId = tgMsgId
		res := db.Save(&bridgePair)
		return res.Error
	}
	res := db.Create(&MsgIdPairs{
		ID:          waMsgId,
		Participant: participant,
		WaChat:      waChat,
		TgChatId:    tgChatId,
		TgMsgId:     tgMsgId,
	})
	return res.Error
}

func GetTgFromWa(waMsgId, waChat string) (int64, int64) {
	db := state.State.Database
	var bridgePair MsgIdPairs
	db.Where("id = ? AND wa_chat = ?", waMsgId, waChat).Find(&bridgePair)
	return bridgePair.TgChatId, bridgePair.TgMsgId
}

func DropAllPairs() error {
	db := state.State.Database
	res := db.Where("1 = 1").Delete(&MsgIdPairs{})
	return res.Error
}

func GetWaFromTg(chatId, msgId int64) (string, string, string, error) {
	db := state.State.Database
	var bridgePair MsgIdPairs
	res := db.Where("tg_chat_id = ? AND tg_msg_id = ?", chatId, msgId).Find(&bridgePair)
	if res.Error != nil {
		return "", "", "", res.Error
	}
	return bridgePair.ID, bridgePair.Participant, bridgePair.WaChat, nil
}
