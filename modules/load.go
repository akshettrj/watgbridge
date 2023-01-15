package modules

import (
	"sync"

	"watgbridge/state"
	"watgbridge/telegram"

	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"go.mau.fi/whatsmeow"
)

var (
	startingValue    int
	lock             *sync.Mutex
	TelegramHandlers map[int][]ext.Handler
	WhatsAppHandlers []whatsmeow.EventHandler
	Modules          []string
)

func GetNewTelegramHandlerGroup() int {
	lock.Lock()
	defer lock.Unlock()

	newStartingValue := startingValue + 1

	defer func(newValue int) {
		startingValue = newValue
	}(newStartingValue)

	return startingValue
}

func LoadModuleHandlers() {
	for handlerGroup, handlers := range TelegramHandlers {
		for _, handler := range handlers {
			state.State.TelegramDispatcher.AddHandlerToGroup(handler, handlerGroup)
		}
	}

	for _, handler := range WhatsAppHandlers {
		state.State.WhatsAppClient.AddEventHandler(handler)
	}
}

func init() {
	lock = &sync.Mutex{}
	startingValue = telegram.ModulesStartingHandlerGroup
	TelegramHandlers = make(map[int][]ext.Handler)
}
