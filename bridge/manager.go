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

func childEnviron(bridge *database.Bridge) ([]string, error) {
	base := os.Environ()
	prefix := sqlitekey.EnvDerived + "="
	out := make([]string, 0, len(base)+3)
	for _, e := range base {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	out = append(out,
		fmt.Sprintf("WATG_BRIDGE_ID=%d", bridge.ID),
		fmt.Sprintf("WATG_BRIDGE_OWNER_TELEGRAM_USER_ID=%d", bridge.OwnerUserID),
	)
	master, hasMaster, err := sqlitekey.MasterKeyBytesFromEnv()
	if err != nil {
		return nil, err
	}
	if !hasMaster {
		return out, nil
	}
	k, err := sqlitekey.DeriveKeyHex(master, fmt.Sprintf("watgbridge-v1/bridge/%d", bridge.ID))
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
	if len(bridges) == 0 {
		state.State.Logger.Warn("multi mode: no enabled bridges in registry — only the main bot runs; WhatsApp bridging needs at least one enabled bridge row")
	}
	for _, b := range bridges {
		if err := m.StartBridge(&b); err != nil {
			state.State.Logger.Warn("failed to start enabled bridge",
				zap.Uint("bridge_id", b.ID),
				zap.Int64("bridge_owner_telegram_user_id", b.OwnerUserID),
				zap.Error(err))
		} else {
			state.State.Logger.Info("started bridge child process",
				zap.Uint("bridge_id", b.ID),
				zap.Int64("bridge_owner_telegram_user_id", b.OwnerUserID))
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
	cmd.Env, err = childEnviron(bridge)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.cmds[bridge.ID] = cmd
	go func(bid uint, ownerID int64, process *exec.Cmd) {
		waitErr := process.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.cmds, bid)
		if waitErr != nil {
			state.State.Logger.Warn("bridge child process exited",
				zap.Uint("bridge_id", bid),
				zap.Int64("bridge_owner_telegram_user_id", ownerID),
				zap.Error(waitErr))
		} else {
			state.State.Logger.Info("bridge child process exited",
				zap.Uint("bridge_id", bid),
				zap.Int64("bridge_owner_telegram_user_id", ownerID))
		}
	}(bridge.ID, bridge.OwnerUserID, cmd)
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
	tgMap := map[string]interface{}{
		"bot_token":       bridge.BridgeBotToken,
		"owner_id":        bridge.OwnerUserID,
		"target_chat_id":  bridge.TelegramTargetChat,
		"api_url":         mainCfg.Telegram.APIURL,
		"self_hosted_api": mainCfg.Telegram.SelfHostedAPI,
	}
	if prov, err := database.BridgeProvisionGet(bridge.ID); err == nil && prov != nil {
		if prov.GeneralThreadID != 0 {
			tgMap["general_thread_id"] = prov.GeneralThreadID
		}
		if prov.BotMetaThreadID != 0 {
			tgMap["bot_meta_thread_id"] = prov.BotMetaThreadID
		}
		if prov.CallsThreadID != 0 {
			tgMap["calls_thread_id"] = prov.CallsThreadID
		}
		if prov.StatusThreadID != 0 {
			tgMap["status_thread_id"] = prov.StatusThreadID
		}
	}
	payload := map[string]interface{}{
		"mode":        "single",
		"time_zone":   mainCfg.TimeZone,
		"time_format": mainCfg.TimeFormat,
		"debug_mode":  mainCfg.DebugMode,
		"telegram":    tgMap,
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
