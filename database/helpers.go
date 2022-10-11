package database

import "wa-tg-bridge/state"

func AddNewWaToTgPair(waMsgId, participant string, tgChatId, tgMsgId int64) error {
	db := state.State.Database
	var bridgePair MsgIdPairs
	db.Where("id = ?", waMsgId).Find(&bridgePair)
	if bridgePair.ID == waMsgId {
		bridgePair.TgChatId = tgChatId
		bridgePair.TgMsgId = tgMsgId
		bridgePair.Participant = participant
		res := db.Save(&bridgePair)
		return res.Error
	}
	res := db.Create(&MsgIdPairs{
		ID:          waMsgId,
		TgChatId:    tgChatId,
		TgMsgId:     tgMsgId,
		Participant: participant,
	})
	return res.Error
}

func GetTgFromWa(waMsgId string) (int64, int64) {
	db := state.State.Database
	var bridgePair MsgIdPairs
	db.Where("id = ?", waMsgId).Find(&bridgePair)
	return bridgePair.TgChatId, bridgePair.TgMsgId
}
