package mainbot

import (
	"encoding/json"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

// managedRelayDispatcher peels off managed_bot updates before gotgbot unmarshals them (the library's Update type omits this field).
type managedRelayDispatcher struct {
	inner *ext.Dispatcher
}

func newManagedRelayDispatcher(inner *ext.Dispatcher) ext.UpdateDispatcher {
	return &managedRelayDispatcher{inner: inner}
}

func (m *managedRelayDispatcher) Start(b *gotgbot.Bot, updates <-chan json.RawMessage) {
	relay := make(chan json.RawMessage, 64)
	go func() {
		defer close(relay)
		for raw := range updates {
			var probe struct {
				ManagedBot *managedBotUpdated `json:"managed_bot,omitempty"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				relay <- raw
				continue
			}
			if probe.ManagedBot != nil {
				_ = handleManagedBotUpdate(b, probe.ManagedBot)
				continue
			}
			relay <- raw
		}
	}()
	m.inner.Start(b, relay)
}

func (m *managedRelayDispatcher) Stop() {
	m.inner.Stop()
}
