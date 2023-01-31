package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"

	"watgbridge/state"

	_ "github.com/jackc/pgx/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func NewWhatsAppClient() error {
	store.DeviceProps.Os = proto.String(state.State.Config.WhatsApp.SessionName)
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	store.DeviceProps.PlatformType = waProto.DeviceProps_DESKTOP.Enum()
	dbLog := waLog.Stdout("WA_Database", "WARN", true)
	container, err := sqlstore.New(state.State.Config.WhatsApp.LoginDatabase.Type,
		state.State.Config.WhatsApp.LoginDatabase.URL, dbLog)
	if err != nil {
		return fmt.Errorf("could not initialize sqlstore for Whatsapp : %s", err)
	}
	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return fmt.Errorf("could not initialize device store for Whatsapp : %s", err)
	}
	clientLog := waLog.Stdout("WA_Client", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	state.State.WhatsAppClient = client

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp for login : %s", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.Generate(evt.Code, qrterminal.L, os.Stdout)
			} else {
				log.Println("[whatsapp] Login event :", evt.Event)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			return fmt.Errorf("could not connect to Whatsapp : %s", err)
		}
	}

	return nil
}
