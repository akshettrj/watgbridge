package database

import (
	"database/sql"

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

// Tag is a label for contacts (Telegram-level only). Name is stored normalized (lowercase, trimmed).
type Tag struct {
	ID   uint   `gorm:"primaryKey;autoIncrement"`
	Name string `gorm:"uniqueIndex;size:191;not null"`
}

// ContactTag links a WA contact (private chat JID) to a tag.
type ContactTag struct {
	WaContactId string `gorm:"primaryKey;size:191;not null"` // WA chat JID (private)
	TagId       uint   `gorm:"primaryKey;not null"`
}

func AutoMigrate() error {
	db := state.State.Database
	return db.AutoMigrate(
		&MsgIdPair{},
		&ChatThreadPair{},
		&ContactName{},
		&ChatEphemeralSettings{},
		&Tag{},
		&ContactTag{},
	)
}
