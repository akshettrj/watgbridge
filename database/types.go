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
	WaMsgId       string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant;index:idx_receipt_msg_chat"`
	WaChatId      string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant;index:idx_receipt_msg_chat"`
	ParticipantId string    `gorm:"primaryKey;index:idx_receipt_msg_chat_participant"`
	ReceiptType   string
	ReceiptTime   time.Time
}

func AutoMigrate() error {
	db := state.State.Database
	err := db.AutoMigrate(
		&MsgIdPair{},
		&ChatThreadPair{},
		&ContactName{},
		&ChatEphemeralSettings{},
		&MessageReceipt{},
	)
	if err != nil {
		return err
	}

	// Ensure indexes exist for message_receipts queries
	// MsgReceiptGetByMsg queries by wa_msg_id + wa_chat_id (no participant_id)
	db.Exec("CREATE INDEX IF NOT EXISTS idx_message_receipts_msg_chat ON message_receipts(wa_msg_id, wa_chat_id)")

	// MsgReceiptUpsert queries by wa_msg_id + wa_chat_id + participant_id
	db.Exec("CREATE INDEX IF NOT EXISTS idx_message_receipts_msg_chat_participant ON message_receipts(wa_msg_id, wa_chat_id, participant_id)")

	return nil
}
