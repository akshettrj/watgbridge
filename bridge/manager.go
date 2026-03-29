package bridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"watgbridge/crypto/sqlitekey"
	"watgbridge/database"
	"watgbridge/state"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type Manager struct {
	mu      sync.Mutex
	baseDir string
	cmds    map[uint]*exec.Cmd
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
		cmds:    make(map[uint]*exec.Cmd),
	}
}

func childEnviron(bridgeID uint) ([]string, error) {
	base := os.Environ()
	prefix := sqlitekey.EnvDerived + "="
	out := make([]string, 0, len(base)+1)
	for _, e := range base {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	master, hasMaster, err := sqlitekey.MasterKeyBytesFromEnv()
	if err != nil {
		return nil, err
	}
	if !hasMaster {
		return out, nil
	}
	k, err := sqlitekey.DeriveKeyHex(master, fmt.Sprintf("watgbridge-v1/bridge/%d", bridgeID))
	if err != nil {
		return nil, err
	}
	return append(out, sqlitekey.EnvDerived+"="+k), nil
}

func (m *Manager) StartEnabled() error {
	bridges, err := database.BridgeListEnabled()
	if err != nil {
		return err
	}
	for _, bridge := range bridges {
		if err := m.StartBridge(&bridge); err != nil {
			state.State.Logger.Warn("failed to start enabled bridge", zap.Uint("bridge_id", bridge.ID), zap.Error(err))
		}
	}
	return nil
}

func (m *Manager) StartBridge(bridge *database.Bridge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cmds[bridge.ID]; ok {
		return nil
	}
	cfgPath, err := m.writeBridgeConfig(bridge)
	if err != nil {
		return err
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(binaryPath, "--mode=single", "--config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env, err = childEnviron(bridge.ID)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.cmds[bridge.ID] = cmd
	go func(bridgeID uint, process *exec.Cmd) {
		_ = process.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.cmds, bridgeID)
	}(bridge.ID, cmd)
	return nil
}

func (m *Manager) StopBridge(bridgeID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd, ok := m.cmds[bridgeID]
	if !ok {
		return nil
	}
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
	}
	delete(m.cmds, bridgeID)
	return nil
}

func (m *Manager) writeBridgeConfig(bridge *database.Bridge) (string, error) {
	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(m.baseDir, fmt.Sprintf("bridge_%d.yaml", bridge.ID))
	mainCfg := state.State.Config
	payload := map[string]interface{}{
		"mode":        "single",
		"time_zone":   mainCfg.TimeZone,
		"time_format": mainCfg.TimeFormat,
		"debug_mode":  mainCfg.DebugMode,
		"telegram": map[string]interface{}{
			"bot_token":       bridge.BridgeBotToken,
			"owner_id":        bridge.OwnerUserID,
			"target_chat_id":  bridge.TelegramTargetChat,
			"api_url":         mainCfg.Telegram.APIURL,
			"self_hosted_api": mainCfg.Telegram.SelfHostedAPI,
		},
		"whatsapp": map[string]interface{}{
			"session_name": bridge.WaSessionName,
			"login_database": map[string]interface{}{
				"type": "sqlite3",
				"url":  "file:" + filepath.Join(m.baseDir, "wawebstore_"+strconv.FormatUint(uint64(bridge.ID), 10)+".db") + "?foreign_keys=on",
			},
		},
		"database": map[string]interface{}{
			"type": "sqlite",
			"path": filepath.Join(m.baseDir, "bridge_"+strconv.FormatUint(uint64(bridge.ID), 10)+".sqlite.db"),
		},
	}
	body, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
