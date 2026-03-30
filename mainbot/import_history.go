package mainbot

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"watgbridge/database"
	"watgbridge/utils"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

const (
	importPendingTTL    = 15 * time.Minute
	maxImportMessages   = 200000
	importFileSizeLimit = 20 << 20 // Telegram Bot API file limit
)

type importPendingState struct {
	bridgeID uint
	until    time.Time
}

var (
	importPendingMu     sync.Mutex
	importPendingByUser = map[int64]importPendingState{}
)

func importPendingSet(userID int64, bridgeID uint) {
	importPendingMu.Lock()
	defer importPendingMu.Unlock()
	importPendingByUser[userID] = importPendingState{
		bridgeID: bridgeID,
		until:    time.Now().Add(importPendingTTL),
	}
}

func importPendingClear(userID int64) {
	importPendingMu.Lock()
	defer importPendingMu.Unlock()
	delete(importPendingByUser, userID)
}

func importPendingPeek(userID int64) (bridgeID uint, ok bool) {
	importPendingMu.Lock()
	defer importPendingMu.Unlock()
	s, exists := importPendingByUser[userID]
	if !exists || time.Now().After(s.until) {
		if exists {
			delete(importPendingByUser, userID)
		}
		return 0, false
	}
	return s.bridgeID, true
}

func importHistoryPendingDocumentFilter(m *gotgbot.Message) bool {
	if m == nil || m.Document == nil || m.From == nil {
		return false
	}
	_, ok := importPendingPeek(m.From.Id)
	return ok
}

func importHistoryCommandHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		args := c.Args()
		user := c.EffectiveSender.User
		if user == nil {
			return nil
		}
		if len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[1]), "cancel") {
			importPendingClear(user.Id)
			_, err := b.SendMessage(c.EffectiveChat.Id, "Import cancelled.", nil)
			return err
		}
		if len(args) < 2 {
			_, err := b.SendMessage(c.EffectiveChat.Id,
				"Usage: /import_history <bridge_id> — then within 15 minutes send your Telegram Desktop export as a document: zip the folder, or send result.json.\n"+
					"The export must include the group that matches this bridge's target chat id (see /bridge_list).\n"+
					"/import_history cancel — abort waiting for a file.", nil)
			return err
		}
		id64, err := strconv.ParseUint(strings.TrimSpace(args[1]), 10, 64)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Invalid bridge_id", nil)
			return sendErr
		}
		rec, err := database.BridgeGetByID(user.Id, uint(id64))
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Bridge not found", nil)
			return sendErr
		}
		importPendingSet(user.Id, rec.ID)
		msg := fmt.Sprintf(
			"Waiting for export file for bridge %d (target chat id <code>%d</code>).\n"+
				"Send <b>result.json</b> or a <b>.zip</b> of the Telegram Desktop export folder (must contain result.json). Max size %d MB. Timeout %d min.\n"+
				"This stores Telegram-side history in the registry DB for search/audit; it does not create WhatsApp message id mappings.",
			rec.ID, rec.TelegramTargetChat, importFileSizeLimit/(1<<20), int(importPendingTTL.Minutes()))
		_, err = b.SendMessage(c.EffectiveChat.Id, msg, &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
		return err
	}
}

func importHistoryDocumentHandler() handlers.Response {
	return func(b *gotgbot.Bot, c *ext.Context) error {
		msg := c.EffectiveMessage
		if msg == nil || msg.Document == nil || msg.From == nil {
			return nil
		}
		bridgeID, ok := importPendingPeek(msg.From.Id)
		if !ok {
			return nil
		}
		if msg.Document.FileSize > importFileSizeLimit {
			_, err := b.SendMessage(c.EffectiveChat.Id, fmt.Sprintf("File too large (max %d MB)", importFileSizeLimit/(1<<20)), nil)
			return err
		}
		userID := msg.From.Id
		rec, err := database.BridgeGetByID(userID, bridgeID)
		if err != nil {
			importPendingClear(userID)
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Bridge not found", nil)
			return sendErr
		}

		file, err := b.GetFile(msg.Document.FileId, &gotgbot.GetFileOpts{
			RequestOpts: &gotgbot.RequestOpts{Timeout: -1},
		})
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Could not get file from Telegram: "+err.Error(), nil)
			return sendErr
		}
		raw, err := utils.TgDownloadByFilePath(b, file.FilePath)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Download failed: "+err.Error(), nil)
			return sendErr
		}
		if len(raw) > importFileSizeLimit {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, fmt.Sprintf("Downloaded file too large (max %d MB)", importFileSizeLimit/(1<<20)), nil)
			return sendErr
		}

		jsonData, err := extractJSONFromUpload(raw, msg.Document.FileName)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Could not read export: "+err.Error(), nil)
			return sendErr
		}

		exportChatID, rows, err := ParseTelegramExportForChat(jsonData, rec.TelegramTargetChat)
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Parse failed: "+err.Error(), nil)
			return sendErr
		}
		if len(rows) > maxImportMessages {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, fmt.Sprintf("Too many messages (%d); max %d", len(rows), maxImportMessages), nil)
			return sendErr
		}

		batchID, err := randomBatchID()
		if err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Internal error (batch id)", nil)
			return sendErr
		}
		if err := database.TelegramImportReplace(userID, rec.ID, rec.TelegramTargetChat, exportChatID, batchID, rows); err != nil {
			_, sendErr := b.SendMessage(c.EffectiveChat.Id, "Database error: "+err.Error(), nil)
			return sendErr
		}
		importPendingClear(userID)
		_, err = b.SendMessage(c.EffectiveChat.Id, fmt.Sprintf("Imported %d messages for bridge %d (batch %s).", len(rows), rec.ID, batchID), nil)
		return err
	}
}

func extractJSONFromUpload(raw []byte, fileName string) ([]byte, error) {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(fileName)))
	switch {
	case strings.HasSuffix(name, ".zip"):
		return ExtractResultJSONFromZIP(raw)
	case strings.HasSuffix(name, ".json") || name == "result.json":
		return raw, nil
	default:
		// try json first (some clients omit .json)
		if len(raw) > 0 && raw[0] == '{' {
			return raw, nil
		}
		z, zerr := ExtractResultJSONFromZIP(raw)
		if zerr == nil {
			return z, nil
		}
		return nil, fmt.Errorf("send result.json or a .zip of the export folder (got %q)", fileName)
	}
}

func randomBatchID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
