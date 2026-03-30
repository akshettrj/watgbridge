package database

import (
	"time"

	"watgbridge/state"

	"gorm.io/gorm"
)

const telegramImportBatchSize = 500

// TelegramImportReplace deletes prior import rows for the bridge and inserts rows (same transaction).
func TelegramImportReplace(ownerUserID int64, bridgeID uint, tgChatID, exportChatID int64, batchID string, rows []TelegramImportMessage) error {
	db := state.State.Database
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bridge_id = ? AND owner_user_id = ?", bridgeID, ownerUserID).Delete(&TelegramImportMessage{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		now := time.Now()
		for i := range rows {
			rows[i].OwnerUserID = ownerUserID
			rows[i].BridgeID = bridgeID
			rows[i].TgChatID = tgChatID
			rows[i].ExportChatID = exportChatID
			rows[i].ImportBatchID = batchID
			if rows[i].CreatedAt.IsZero() {
				rows[i].CreatedAt = now
			}
		}
		return tx.CreateInBatches(rows, telegramImportBatchSize).Error
	})
}
