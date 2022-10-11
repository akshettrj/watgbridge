package database

import "wa-tg-bridge/state"

type MsgIdPairs struct {
	ID          string `gorm:"primaryKey;"`
	Participant string
	WaChat      string
	TgChatId    int64
	TgMsgId     int64
}

func AutoMigrate() error {
	db := state.State.Database
	return db.AutoMigrate(MsgIdPairs{})
}
