package database

import (
	"sync"

	"watgbridge/state"

	"gorm.io/gorm"
)

var (
	provisionStateMu     sync.Mutex
	provisionStateDBConn *gorm.DB
)

// SetProvisionStateDB makes BridgeProvisionGet/Set use the given DB (multi-mode registry SQLite).
// Per-bridge child processes call this so provision state is stored where the parent reads it.
func SetProvisionStateDB(db *gorm.DB) {
	provisionStateMu.Lock()
	defer provisionStateMu.Unlock()
	provisionStateDBConn = db
}

func provisionStateDBOrDefault() *gorm.DB {
	provisionStateMu.Lock()
	defer provisionStateMu.Unlock()
	if provisionStateDBConn != nil {
		return provisionStateDBConn
	}
	return state.State.Database
}
