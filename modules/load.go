package modules

import (
	"log"
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
)

func GetNewTelegramHandlerGroup() int {
	lock.Lock()
	defer lock.Unlock()

	returnValue := startingValue
	startingValue += 1

	return returnValue
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

	log.Println("Modules loaded:")
	for _, plugin := range state.State.Modules {
		log.Println(plugin)
	}
}

func init() {
	lock = &sync.Mutex{}
	startingValue = telegram.ModulesStartingHandlerGroup
	TelegramHandlers = make(map[int][]ext.Handler)
}
