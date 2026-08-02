package database

import (
	"database/sql"
	"time"

	"watgbridge/state"
)

type MsgIdPair struct {
	// WhatsApp
	ID            string `gorm:"primaryKey;"` // Message ID
	ParticipantId string // Sender JID
	WaChatId      string // Chat JID

	// Telegram
	TgChatId   int64
	TgThreadId int64
	TgMsgId    int64

	MarkRead sql.NullBool
	AutoReacted bool
}

type ChatThreadPair struct {
	ID         string `gorm:"primaryKey;"` // WhatsApp Chat ID
	TgChatId   int64  // Telegram Chat ID
	TgThreadId int64  // Telegram Thread ID (Topics)
}

type ContactName struct {
	ID           string `gorm:"primaryKey;"` // WhatsApp Contact JID
	FirstName    string
	FullName     string
	PushName     string
	BusinessName string
	Server       string
}

type ChatEphemeralSettings struct {
	ID             string `gorm:"primaryKey;"` // WhatsApp Chat ID
	IsEphemeral    bool
	EphemeralTimer uint32
}

type MessageReceipt struct {
	WaMsgId       string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant"`
	WaChatId      string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant"`
	ParticipantId string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant"`
	ReceiptType   string
	ReceiptTime   time.Time
}

func AutoMigrate() error {
	db := state.State.Database
	return db.AutoMigrate(
		&MsgIdPair{},
		&ChatThreadPair{},
		&ContactName{},
		&ChatEphemeralSettings{},
		&MessageReceipt{},
	)
}
