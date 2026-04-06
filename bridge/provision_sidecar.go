package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// provisionSidecar persists forum thread IDs for a bridge child process. The main process reads
// the registry DB for BridgeProvisionState, but child processes use a separate SQLite file, so
// BridgeProvisionSet from a child does not update what writeBridgeConfig reads. Sidecar files live
// next to bridge_N.yaml under the manager baseDir.
type provisionSidecar struct {
	GeneralThreadID  int64 `json:"general_thread_id"`
	BotMetaThreadID  int64 `json:"bot_meta_thread_id"`
	CallsThreadID    int64 `json:"calls_thread_id"`
	StatusThreadID   int64 `json:"status_thread_id"`
}

func ProvisionSidecarPath(baseDir string, bridgeID uint) string {
	return filepath.Join(baseDir, fmt.Sprintf("bridge_%d.provision.json", bridgeID))
}

func WriteProvisionSidecar(baseDir string, bridgeID uint, general, botMeta, calls, status int64) error {
	if bridgeID == 0 || baseDir == "" {
		return nil
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	p := provisionSidecar{
		GeneralThreadID: general,
		BotMetaThreadID: botMeta,
		CallsThreadID:   calls,
		StatusThreadID:  status,
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	final := ProvisionSidecarPath(baseDir, bridgeID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// ReadProvisionSidecar returns thread ids from the sidecar if the file exists and parses. If the file
// is missing, ok is false and err is nil. Corrupt JSON returns err != nil.
func ReadProvisionSidecar(baseDir string, bridgeID uint) (general, botMeta, calls, status int64, ok bool, err error) {
	if bridgeID == 0 || baseDir == "" {
		return 0, 0, 0, 0, false, nil
	}
	path := ProvisionSidecarPath(baseDir, bridgeID)
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, 0, 0, false, nil
		}
		return 0, 0, 0, 0, false, err
	}
	var p provisionSidecar
	if err := json.Unmarshal(body, &p); err != nil {
		return 0, 0, 0, 0, false, err
	}
	return p.GeneralThreadID, p.BotMetaThreadID, p.CallsThreadID, p.StatusThreadID, true, nil
}
