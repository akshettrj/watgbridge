package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"watgbridge/state"
)

func HashBridgeToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func BridgeUserEnsure(userID int64) error {
	db := state.State.Database
	var u BridgeUser
	res := db.Where("telegram_user_id = ?", userID).First(&u)
	if res.Error == nil {
		return nil
	}
	return db.Create(&BridgeUser{TelegramUserID: userID, Status: "active"}).Error
}

func BridgeCreate(ownerUserID int64, name, token string, tgTargetChatID int64, waSessionName string, enabled bool) (*Bridge, error) {
	db := state.State.Database
	hash := HashBridgeToken(token)
	bridge := &Bridge{
		OwnerUserID:        ownerUserID,
		Name:               strings.TrimSpace(name),
		BridgeBotToken:     strings.TrimSpace(token),
		BridgeBotTokenHash: hash,
		TelegramTargetChat: tgTargetChatID,
		WaSessionName:      strings.TrimSpace(waSessionName),
		Enabled:            enabled,
	}
	if err := db.Create(bridge).Error; err != nil {
		return nil, err
	}
	if err := db.Create(&BridgeProvisionState{BridgeID: bridge.ID, LastCheckStatus: "created"}).Error; err != nil {
		return nil, err
	}
	return bridge, nil
}

func BridgeGetByID(ownerUserID int64, bridgeID uint) (*Bridge, error) {
	db := state.State.Database
	var bridge Bridge
	res := db.Where("id = ? AND owner_user_id = ?", bridgeID, ownerUserID).First(&bridge)
	if res.Error != nil {
		return nil, res.Error
	}
	return &bridge, nil
}

func BridgeGetByIDAnyOwner(bridgeID uint) (*Bridge, error) {
	db := state.State.Database
	var bridge Bridge
	res := db.Where("id = ?", bridgeID).First(&bridge)
	if res.Error != nil {
		return nil, res.Error
	}
	return &bridge, nil
}

func BridgeListByOwner(ownerUserID int64) ([]Bridge, error) {
	db := state.State.Database
	var bridges []Bridge
	err := db.Where("owner_user_id = ?", ownerUserID).Order("id asc").Find(&bridges).Error
	return bridges, err
}

func BridgeListEnabled() ([]Bridge, error) {
	db := state.State.Database
	var bridges []Bridge
	err := db.Where("enabled = ?", true).Order("id asc").Find(&bridges).Error
	return bridges, err
}

func BridgeSetEnabled(ownerUserID int64, bridgeID uint, enabled bool) error {
	db := state.State.Database
	return db.Model(&Bridge{}).
		Where("id = ? AND owner_user_id = ?", bridgeID, ownerUserID).
		Updates(map[string]interface{}{"enabled": enabled, "updated_at": time.Now()}).Error
}

func BridgeDelete(ownerUserID int64, bridgeID uint) error {
	db := state.State.Database
	if err := db.Where("bridge_id = ?", bridgeID).Delete(&BridgeProvisionState{}).Error; err != nil {
		return err
	}
	if err := db.Where("bridge_id = ?", bridgeID).Delete(&TelegramImportMessage{}).Error; err != nil {
		return err
	}
	return db.Where("id = ? AND owner_user_id = ?", bridgeID, ownerUserID).Delete(&Bridge{}).Error
}

func BridgeProvisionGet(bridgeID uint) (*BridgeProvisionState, error) {
	db := state.State.Database
	var p BridgeProvisionState
	res := db.Where("bridge_id = ?", bridgeID).First(&p)
	if res.Error != nil {
		return nil, res.Error
	}
	return &p, nil
}

func BridgeProvisionSet(bridgeID uint, general, botMeta, calls int64, status, lastErr string) error {
	db := state.State.Database
	status = strings.TrimSpace(status)
	if status == "" {
		status = "ok"
	}
	res := db.Model(&BridgeProvisionState{}).Where("bridge_id = ?", bridgeID).Updates(map[string]interface{}{
		"general_thread_id":   general,
		"bot_meta_thread_id":  botMeta,
		"calls_thread_id":     calls,
		"last_check_status":   status,
		"last_check_error":    lastErr,
		"last_provisioned_at": time.Now(),
		"updated_at":          time.Now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return db.Create(&BridgeProvisionState{
			BridgeID:          bridgeID,
			GeneralThreadID:   general,
			BotMetaThreadID:   botMeta,
			CallsThreadID:     calls,
			LastCheckStatus:   status,
			LastCheckError:    lastErr,
			LastProvisionedAt: time.Now(),
		}).Error
	}
	return nil
}

func BridgeBuildName(ownerID int64, nextIndex int) string {
	baseNames := []string{"chrome", "safari", "firefox", "edge", "opera", "brave"}
	name := baseNames[nextIndex%len(baseNames)]
	return fmt.Sprintf("%s-%d-%d", name, ownerID, nextIndex+1)
}

func BridgeNextName(ownerID int64) (string, error) {
	bridges, err := BridgeListByOwner(ownerID)
	if err != nil {
		return "", err
	}
	return BridgeBuildName(ownerID, len(bridges)), nil
}
