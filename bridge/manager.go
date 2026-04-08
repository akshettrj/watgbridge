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

// registrySQLitePathFromMainConfig returns the absolute registry DB path for multi-mode children so
// they can open the same SQLCipher file as the main process for bridge_provision_states updates.
func registrySQLitePathFromMainConfig() string {
	mainCfg := state.State.Config
	if mainCfg.Mode != "multi" {
		return ""
	}
	p, ok := mainCfg.Database["path"]
	if !ok || strings.TrimSpace(p) == "" {
		return ""
	}
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) {
		return p
	}
	if mainCfg.Path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(mainCfg.Path), p)
}

func childEnviron(bridge *database.Bridge, registrySQLitePath string) ([]string, error) {
	// Parent may only have WATG_SQLCIPHER_KEY_HEX (registry key for multi+sqlite main). We strip that
	// below and set per-bridge WATG_SQLCIPHER_KEY_HEX, so pass the registry key explicitly for
	// OpenRegistrySQLiteForProvision.
	regHex := strings.TrimSpace(os.Getenv(sqlitekey.EnvRegistryDerived))
	if regHex == "" {
		regHex = strings.TrimSpace(os.Getenv(sqlitekey.EnvDerived))
	}
	if regHex == "" && registrySQLitePath != "" {
		master, ok, err := sqlitekey.MasterKeyBytesFromEnv()
		if err != nil {
			return nil, err
		}
		if ok {
			var derr error
			regHex, derr = sqlitekey.DeriveKeyHex(master, "watgbridge-v1/registry")
			if derr != nil {
				return nil, derr
			}
		}
	}

	base := os.Environ()
	prefix := sqlitekey.EnvDerived + "="
	out := make([]string, 0, len(base)+6)
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
	if registrySQLitePath != "" {
		out = append(out, "WATG_REGISTRY_SQLITE_PATH="+registrySQLitePath)
		if regHex != "" {
			out = append(out, sqlitekey.EnvRegistryDerived+"="+regHex)
		}
	}
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
	var wg sync.WaitGroup
	for i := range bridges {
		b := bridges[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
	}
	wg.Wait()
	return nil
}

func (m *Manager) StartBridge(bridge *database.Bridge) error {
	m.mu.Lock()
	if _, ok := m.cmds[bridge.ID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

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
	cmd.Env, err = childEnviron(bridge, registrySQLitePathFromMainConfig())
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.mu.Lock()
	if _, ok := m.cmds[bridge.ID]; ok {
		m.mu.Unlock()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}
	m.cmds[bridge.ID] = cmd
	m.mu.Unlock()
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
		"bot_token":          bridge.BridgeBotToken,
		"owner_id":           bridge.OwnerUserID,
		"target_chat_id":     bridge.TelegramTargetChat,
		"api_url":            mainCfg.Telegram.APIURL,
		"self_hosted_api":    mainCfg.Telegram.SelfHostedAPI,
		"bridge_registry_id": bridge.ID,
	}
	if mainCfg.Telegram.MainBotToken != "" {
		tgMap["control_bot_token"] = mainCfg.Telegram.MainBotToken
	}
	var provGeneral, provBotMeta, provCalls, provStatus int64
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
		provGeneral = prov.GeneralThreadID
		provBotMeta = prov.BotMetaThreadID
		provCalls = prov.CallsThreadID
		provStatus = prov.StatusThreadID
	}
	// Fallback: if registry rows are still zero (e.g. before registry-open fix), merge sidecar JSON
	// written by the child. Registry non-zero values win over sidecar.
	if sg, sm, sc, ss, ok, err := ReadProvisionSidecar(m.baseDir, bridge.ID); err != nil {
		state.State.Logger.Warn("bridge provision sidecar read failed",
			zap.Uint("bridge_id", bridge.ID), zap.Error(err))
	} else if ok {
		if sg != 0 && provGeneral == 0 {
			tgMap["general_thread_id"] = sg
		}
		if sm != 0 && provBotMeta == 0 {
			tgMap["bot_meta_thread_id"] = sm
		}
		if sc != 0 && provCalls == 0 {
			tgMap["calls_thread_id"] = sc
		}
		if ss != 0 && provStatus == 0 {
			tgMap["status_thread_id"] = ss
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
