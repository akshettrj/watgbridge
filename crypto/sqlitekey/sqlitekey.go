package sqlitekey

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	EnvMaster          = "WATG_SQLITE_MASTER_KEY"
	EnvDerived         = "WATG_SQLCIPHER_KEY_HEX"
	EnvRegistryDerived = "WATG_REGISTRY_SQLCIPHER_KEY_HEX"
)

// MasterKeyBytesFromEnv returns the 32-byte master key from WATG_SQLITE_MASTER_KEY (64 hex chars).
func MasterKeyBytesFromEnv() (key []byte, ok bool, err error) {
	v := strings.TrimSpace(os.Getenv(EnvMaster))
	if v == "" {
		return nil, false, nil
	}
	b, err := hex.DecodeString(v)
	if err != nil {
		return nil, false, fmt.Errorf("%s: invalid hex: %w", EnvMaster, err)
	}
	if len(b) != 32 {
		return nil, false, fmt.Errorf("%s: want 32 bytes (64 hex chars), got %d", EnvMaster, len(b))
	}
	return b, true, nil
}

// DeriveKeyHex returns 64 hex chars (32 bytes) for SQLCipher PRAGMA key = "x'<hex>'".
func DeriveKeyHex(master []byte, info string) (string, error) {
	r := hkdf.New(sha256.New, master, nil, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

// DerivedHexFromEnv returns WATG_SQLCIPHER_KEY_HEX if set.
func DerivedHexFromEnv() (hexKey string, ok bool) {
	v := strings.TrimSpace(os.Getenv(EnvDerived))
	if v == "" {
		return "", false
	}
	return v, true
}

// ApplyToDB sets the SQLCipher key; hexKey must decode to 32 bytes.
func ApplyToDB(db *sql.DB, hexKey string) error {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("%s: want 64 hex chars (32 bytes)", EnvDerived)
	}
	_, err = db.Exec(`PRAGMA key = "x'` + hex.EncodeToString(raw) + `'"`)
	return err
}
