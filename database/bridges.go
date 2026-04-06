package database

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"watgbridge/state"

	"gorm.io/gorm"
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

// BridgeRegistryNotifyUserIDs returns distinct Telegram user ids (bridge_users + bridge owners) for main-bot broadcasts.
func BridgeRegistryNotifyUserIDs() ([]int64, error) {
	db := state.State.Database
	seen := make(map[int64]struct{})
	var out []int64
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	var users []BridgeUser
	if err := db.Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		add(u.TelegramUserID)
	}
	var ownerIDs []int64
	if err := db.Model(&Bridge{}).Distinct("owner_user_id").Pluck("owner_user_id", &ownerIDs).Error; err != nil {
		return nil, err
	}
	for _, id := range ownerIDs {
		add(id)
	}
	return out, nil
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

func BridgeProvisionSet(bridgeID uint, general, botMeta, calls, statusThread int64, lastCheckStatus, lastErr string) error {
	db := state.State.Database
	lastCheckStatus = strings.TrimSpace(lastCheckStatus)
	if lastCheckStatus == "" {
		lastCheckStatus = "ok"
	}
	res := db.Model(&BridgeProvisionState{}).Where("bridge_id = ?", bridgeID).Updates(map[string]interface{}{
		"general_thread_id":   general,
		"bot_meta_thread_id":  botMeta,
		"calls_thread_id":     calls,
		"status_thread_id":    statusThread,
		"last_check_status":   lastCheckStatus,
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
			StatusThreadID:    statusThread,
			LastCheckStatus:   lastCheckStatus,
			LastCheckError:    lastErr,
			LastProvisionedAt: time.Now(),
		}).Error
	}
	return nil
}

func randomPairToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func BridgePendingManagedUpsert(ownerUserID, managedBotUserID int64, token, labelHint string) error {
	db := state.State.Database
	now := time.Now()
	pairTok, err := randomPairToken()
	if err != nil {
		return err
	}
	row := BridgePendingManaged{
		OwnerUserID:      ownerUserID,
		ManagedBotUserID: managedBotUserID,
		BridgeBotToken:   strings.TrimSpace(token),
		PairToken:        pairTok,
		LabelHint:        strings.TrimSpace(labelHint),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return db.Save(&row).Error
}

func BridgePendingManagedGet(ownerUserID int64) (*BridgePendingManaged, error) {
	db := state.State.Database
	var row BridgePendingManaged
	res := db.Where("owner_user_id = ?", ownerUserID).First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &row, nil
}

// BridgePendingManagedGetByPairToken resolves a pending managed bind by /start deep-link payload (WaTgBridge bridge bot).
func BridgePendingManagedGetByPairToken(pairToken string) (*BridgePendingManaged, error) {
	pairToken = strings.TrimSpace(pairToken)
	if pairToken == "" {
		return nil, gorm.ErrRecordNotFound
	}
	db := state.State.Database
	var row BridgePendingManaged
	res := db.Where("pair_token = ?", pairToken).First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &row, nil
}

func BridgePendingManagedDelete(ownerUserID int64) error {
	db := state.State.Database
	return db.Where("owner_user_id = ?", ownerUserID).Delete(&BridgePendingManaged{}).Error
}

func BridgeManagedBotUpsert(ownerUserID, managedBotUserID int64, token, labelHint string) error {
	db := state.State.Database
	now := time.Now()
	token = strings.TrimSpace(token)
	labelHint = strings.TrimSpace(labelHint)
	var row BridgeManagedBot
	err := db.Where("owner_user_id = ? AND managed_bot_user_id = ?", ownerUserID, managedBotUserID).First(&row).Error
	if err == nil {
		return db.Model(&row).Updates(map[string]interface{}{
			"bridge_bot_token": token,
			"label_hint":       labelHint,
			"updated_at":       now,
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&BridgeManagedBot{
		OwnerUserID:      ownerUserID,
		ManagedBotUserID: managedBotUserID,
		BridgeBotToken:   token,
		LabelHint:        labelHint,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error
}

// BridgeManagedBotListUnlinked returns managed bots for this owner that are not used by any active Bridge row.
func BridgeManagedBotListUnlinked(ownerUserID int64) ([]BridgeManagedBot, error) {
	db := state.State.Database
	var rows []BridgeManagedBot
	if err := db.Where("owner_user_id = ?", ownerUserID).Order("updated_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	var bridges []Bridge
	if err := db.Where("owner_user_id = ?", ownerUserID).Find(&bridges).Error; err != nil {
		return nil, err
	}
	inUse := make(map[string]struct{}, len(bridges))
	for _, br := range bridges {
		inUse[HashBridgeToken(br.BridgeBotToken)] = struct{}{}
	}
	var out []BridgeManagedBot
	for _, r := range rows {
		if _, ok := inUse[HashBridgeToken(r.BridgeBotToken)]; !ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func BridgeManagedBotGetByOwnerAndManagedID(ownerUserID, managedBotUserID int64) (*BridgeManagedBot, error) {
	db := state.State.Database
	var row BridgeManagedBot
	res := db.Where("owner_user_id = ? AND managed_bot_user_id = ?", ownerUserID, managedBotUserID).First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &row, nil
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
