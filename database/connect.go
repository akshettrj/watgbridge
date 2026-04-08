package database

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"watgbridge/crypto/sqlitekey"
	"watgbridge/internal/gormsqlcipher"
	"watgbridge/state"

	_ "github.com/mutecomm/go-sqlcipher/v4"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func hasKeys(p_map *map[string]string, keys ...string) (missingKeys []string) {
	m := *p_map
	for _, key := range keys {
		_, exists := m[key]
		if !exists {
			missingKeys = append(missingKeys, key)
		}
	}
	return missingKeys
}

func Connect() (*gorm.DB, error) {
	dbConfig := state.State.Config.Database
	dbType, exists := dbConfig["type"]
	if !exists {
		return nil, fmt.Errorf("Error: key 'type' not found in database config")
	}

	// Never log full SQL by default: INSERT/UPDATE bodies contain WA contact PII (names, JIDs).
	// Errors still surface via returned errors; use Silent so routine queries are not printed.
	gormConfig := gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	}

	switch dbType {

	case "postgres":

		if missingKeys := hasKeys(&state.State.Config.Database,
			"host", "user", "password", "dbname", "port", "time_zone",
		); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		var dns string
		dns += "host=" + dbConfig["host"]
		dns += " user=" + dbConfig["user"]
		dns += " password=" + dbConfig["password"]
		dns += " dbname=" + dbConfig["dbname"]
		dns += " port=" + dbConfig["port"]
		dns += " TimeZone=" + dbConfig["time_zone"]
		if _, found := dbConfig["ssl"]; !found {
			dns += " sslmode=disable"
		}

		return gorm.Open(postgres.Open(dns), &gormConfig)

	case "sqlite":

		if missingKeys := hasKeys(&state.State.Config.Database, "path"); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		sqlDB, err := sql.Open("sqlite3", dbConfig["path"])
		if err != nil {
			return nil, err
		}
		if hexKey, ok := sqlitekey.DerivedHexFromEnv(); ok {
			if err := sqlitekey.ApplyToDB(sqlDB, hexKey); err != nil {
				_ = sqlDB.Close()
				return nil, fmt.Errorf("sqlcipher: %w", err)
			}
		}
		return gorm.Open(gormsqlcipher.New(gormsqlcipher.Config{Conn: sqlDB}), &gormConfig)

	case "mysql":

		if missingKeys := hasKeys(&state.State.Config.Database,
			"user", "password", "host", "port", "dbname",
		); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		dns := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			dbConfig["user"],
			dbConfig["password"],
			dbConfig["host"],
			dbConfig["port"],
			dbConfig["dbname"],
		)

		return gorm.Open(mysql.Open(dns), &gormConfig)
	}

	return nil, fmt.Errorf("Database of type '%s' is not supported", dbType)
}

// OpenRegistrySQLiteForProvision opens the multi-mode registry SQLite (SQLCipher) using the
// watgbridge-v1/registry key derivation. Bridge children use this so BridgeProvisionSet updates the
// same file the main process uses, independent of the per-bridge database file.
//
// Key resolution: WATG_REGISTRY_SQLCIPHER_KEY_HEX (64 hex, same derivation as main) first — set by
// the multi-mode parent when spawning children so they need not have WATG_SQLITE_MASTER_KEY; else
// WATG_SQLITE_MASTER_KEY + derive watgbridge-v1/registry.
func OpenRegistrySQLiteForProvision(absPath string) (*gorm.DB, error) {
	gormConfig := gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	}
	sqlDB, err := sql.Open("sqlite3", absPath)
	if err != nil {
		return nil, err
	}
	var k string
	if regHex := strings.TrimSpace(os.Getenv(sqlitekey.EnvRegistryDerived)); regHex != "" {
		k = regHex
	} else {
		master, hasMaster, err := sqlitekey.MasterKeyBytesFromEnv()
		if err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		if !hasMaster {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("set %s or WATG_SQLITE_MASTER_KEY for registry SQLCipher", sqlitekey.EnvRegistryDerived)
		}
		k, err = sqlitekey.DeriveKeyHex(master, "watgbridge-v1/registry")
		if err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}
	if err := sqlitekey.ApplyToDB(sqlDB, k); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("sqlcipher registry: %w", err)
	}
	return gorm.Open(gormsqlcipher.New(gormsqlcipher.Config{Conn: sqlDB}), &gormConfig)
}
