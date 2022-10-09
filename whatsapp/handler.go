package whatsapp

import (
	"fmt"
	"html"

	"wa-tg-bridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"go.mau.fi/whatsmeow/types/events"
)

func WhatsAppEventHandler(evt interface{}) {
	waClient := state.State.WhatsAppClient
	tgBot := state.State.TelegramBot
	cfg := state.State.Config

	switch v := evt.(type) {

	case *events.CallOffer:
		// TODO: Check and handle group calls
		var callerName string
		caller, err := waClient.Store.Contacts.GetContact(v.CallCreator)
		if err != nil || !caller.Found {
			callerName = v.CallCreator.String()
		} else {
			callerName = caller.FullName
			if callerName == "" {
				callerName = caller.BusinessName
			}
			if callerName == "" {
				callerName = caller.PushName
			}
			callerName += fmt.Sprintf(" [ %s ]", v.CallCreator.User)
		}

		fmt.Printf("%+v\n", v)

		tgBot.SendMessage(
			cfg.Telegram.TargetChatID,
			fmt.Sprintf(
				`#calls

You received a new call

🧑: <i>%s</i>
🕛: <code>%s</code>`,
				html.EscapeString(callerName),
				html.EscapeString(v.Timestamp.Local().Format(cfg.TimeFormat)),
			),
			&gotgbot.SendMessageOpts{},
		)

	}
}
