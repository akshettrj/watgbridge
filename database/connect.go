package database

import (
	"fmt"

	"watgbridge/state"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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

	gormConfig := gorm.Config{}

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

		return gorm.Open(sqlite.Open(dbConfig["path"]), &gormConfig)

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
