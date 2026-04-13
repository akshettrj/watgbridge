package whatsapp

import (
	"fmt"
	"strings"
	"time"

	"watgbridge/database"
	"watgbridge/state"

	"go.mau.fi/whatsmeow"
	waTypes "go.mau.fi/whatsmeow/types"
)

// singleModeSessionProvisionBridgeID is used when BridgeRegistryID is unset.
const singleModeSessionProvisionBridgeID uint = 1

func ProvisionBridgeID() uint {
	cfg := state.State.Config
	if cfg != nil && cfg.Telegram.BridgeRegistryID != 0 {
		return cfg.Telegram.BridgeRegistryID
	}
	return singleModeSessionProvisionBridgeID
}

func currentSessionFromClient(cli *whatsmeow.Client) (jid waTypes.JID, pushName string, ok bool) {
	if cli == nil || cli.Store == nil {
		return waTypes.JID{}, "", false
	}
	jid = cli.Store.GetJID()
	if jid.IsEmpty() && cli.Store.ID != nil {
		jid = cli.Store.ID.ToNonAD()
	}
	jid = jid.ToNonAD()
	if jid.IsEmpty() {
		return waTypes.JID{}, "", false
	}
	return jid, strings.TrimSpace(cli.Store.PushName), true
}

func PersistCurrentSessionActive(cli *whatsmeow.Client) error {
	jid, pushName, ok := currentSessionFromClient(cli)
	if !ok {
		return fmt.Errorf("no linked whatsapp session")
	}
	return database.BridgeProvisionSetSessionActive(
		ProvisionBridgeID(),
		jid.String(),
		waPhoneDisplay(jid),
		pushName,
		time.Now().UTC(),
	)
}

func PersistCurrentSessionInactive(reason string) error {
	return database.BridgeProvisionMarkSessionInactive(ProvisionBridgeID(), reason, time.Now().UTC())
}

func LoadProvisionSessionState() (*database.BridgeProvisionState, uint, error) {
	bid := ProvisionBridgeID()
	row, err := database.BridgeProvisionGetOptional(bid)
	return row, bid, err
}
