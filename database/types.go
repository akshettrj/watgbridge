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
}

type ChatThreadPair struct {
	ID         string `gorm:"primaryKey;"` // WhatsApp Chat ID
	TgChatId   int64  // Telegram Chat ID
	TgThreadId int64  // Telegram Thread ID (Topics)

	// Pinned "topic metadata" card (1:1 and groups only; not calls/status/mentions).
	WaDisplayName     string       `gorm:"size:512"` // WhatsApp-side title; updated on WA sync, independent of Telegram topic title
	TgTopicCreatedAt  sql.NullTime // When the bridge first created this Telegram forum topic
	WaDialogCreatedAt sql.NullTime // Group: WhatsApp group creation; private: usually unknown
	MetadataTgMsgId   int64        // Telegram message id of the pinned metadata message
	// Last forum topic title the bridge applied from WA (create / WA group rename / successful sync edit).
	// Used on /synccontactname and /synctopicnames to detect a user-renamed topic vs WA drift.
	TgForumTitleSyncedFromWA string `gorm:"size:128"`
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

type BridgeUser struct {
	TelegramUserID int64     `gorm:"primaryKey;autoIncrement:false"`
	Status         string    `gorm:"size:32;not null;default:active"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
}

type Bridge struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement"`
	OwnerUserID        int64     `gorm:"index:idx_bridge_owner_name,unique;not null"`
	Name               string    `gorm:"size:191;index:idx_bridge_owner_name,unique;not null"`
	BridgeBotToken     string    `gorm:"type:text;not null"`
	BridgeBotTokenHash string    `gorm:"size:64;uniqueIndex;not null"`
	TelegramTargetChat int64     `gorm:"not null"`
	WaSessionName      string    `gorm:"size:191;uniqueIndex;not null"`
	Enabled            bool      `gorm:"not null;default:true"`
	CreatedAt          time.Time `gorm:"not null"`
	UpdatedAt          time.Time `gorm:"not null"`
}

type BridgeProvisionState struct {
	BridgeID          uint `gorm:"primaryKey;autoIncrement:false"`
	GeneralThreadID   int64
	BotMetaThreadID   int64
	CallsThreadID     int64
	StatusThreadID    int64
	LastCheckStatus   string `gorm:"size:64;not null;default:pending"`
	LastCheckError    string `gorm:"type:text"`
	LastProvisionedAt time.Time
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

// BridgePendingManaged stores a managed-bridge bot token until the owner runs /bridge_bind with a target forum group.
type BridgePendingManaged struct {
	OwnerUserID      int64     `gorm:"primaryKey;autoIncrement:false"`
	ManagedBotUserID int64     `gorm:"not null"`
	BridgeBotToken   string    `gorm:"type:text;not null"`
	LabelHint        string    `gorm:"size:191"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}

// BridgeManagedBot is every managed bridge bot created for an owner (persists after bind/delete for reuse).
type BridgeManagedBot struct {
	ID               uint   `gorm:"primaryKey;autoIncrement"`
	OwnerUserID      int64  `gorm:"uniqueIndex:ux_bridge_managed_owner_bot;not null"`
	ManagedBotUserID int64  `gorm:"uniqueIndex:ux_bridge_managed_owner_bot;not null"`
	BridgeBotToken   string `gorm:"type:text;not null"`
	LabelHint        string `gorm:"size:191"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TelegramImportMessage stores Telegram Desktop JSON export rows for a bridge target chat.
// This is archival metadata only; it does not populate MsgIdPair (WhatsApp ids are not in the export).
type TelegramImportMessage struct {
	ID            uint   `gorm:"primaryKey;autoIncrement"`
	BridgeID      uint   `gorm:"index;uniqueIndex:ux_tg_import_bridge_msg;not null"`
	OwnerUserID   int64  `gorm:"index;not null"`
	TgChatID      int64  `gorm:"not null"`
	ExportChatID  int64  `gorm:"not null"`
	TgMessageID   int64  `gorm:"uniqueIndex:ux_tg_import_bridge_msg;not null"`
	TgThreadID    int64  // 0 if unknown in export
	FromName      string `gorm:"size:512"`
	FromID        string `gorm:"size:256"`
	Text          string `gorm:"type:text"`
	MsgType       string `gorm:"size:32"`
	ServiceAction string `gorm:"size:64"`
	DateUnix      int64
	ImportBatchID string    `gorm:"size:36;index;not null"`
	CreatedAt     time.Time `gorm:"not null"`
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
		&BridgeUser{},
		&Bridge{},
		&BridgeProvisionState{},
		&BridgePendingManaged{},
		&BridgeManagedBot{},
		&TelegramImportMessage{},
	)
}
