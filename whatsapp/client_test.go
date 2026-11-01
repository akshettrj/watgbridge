package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// testOwnerID is a synthetic id; the value only has to be stable across the
// assertions below.
const testOwnerID = int64(123456789)

// testQRCode is arbitrary data; the QR code contents do not matter here.
const testQRCode = "2@AbCdEfGhIjKlMnOpQrStUvWxYz0123456789,+test"

// fakeBotClient records the Telegram API calls made through a bot and replies
// with a canned result, so tests never touch the network.
type fakeBotClient struct {
	err     error
	result  string
	methods []string
	params  map[string]any
}

func (f *fakeBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	f.methods = append(f.methods, method)
	f.params = params
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(f.result), nil
}

func (f *fakeBotClient) GetAPIURL(*gotgbot.RequestOpts) string { return gotgbot.DefaultAPIURL }

func (f *fakeBotClient) FileURL(_ string, _ string, _ *gotgbot.RequestOpts) string {
	return gotgbot.DefaultAPIURL
}

func newTestBot(t *testing.T, client gotgbot.BotClient) *gotgbot.Bot {
	t.Helper()

	bot, err := gotgbot.NewBot("123456:test", &gotgbot.BotOpts{
		BotClient:         client,
		DisableTokenCheck: true,
	})
	if err != nil {
		t.Fatalf("failed to create test bot : %v", err)
	}

	return bot
}

func TestSendQRCodeToOwnerSendsPhoto(t *testing.T) {
	client := &fakeBotClient{result: `{"message_id":42}`}
	bot := newTestBot(t, client)

	if err := sendQRCodeToOwner(bot, testOwnerID, testQRCode); err != nil {
		t.Fatalf("expected the QR code to be sent, got : %v", err)
	}

	if len(client.methods) != 1 || client.methods[0] != "sendPhoto" {
		t.Fatalf("expected a single sendPhoto call, got : %v", client.methods)
	}

	if chatID, ok := client.params["chat_id"].(int64); !ok || chatID != testOwnerID {
		t.Errorf("expected the QR code to go to chat %d, got : %#v", testOwnerID, client.params["chat_id"])
	}

	if caption, ok := client.params["caption"].(string); !ok || caption != "Scan the above QR code to login to WhatsApp." {
		t.Errorf("unexpected caption : %#v", client.params["caption"])
	}

	photo, ok := client.params["photo"].(*gotgbot.FileReader)
	if !ok {
		t.Fatalf("expected a photo upload, got : %#v", client.params["photo"])
	}
	if photo.Name != "qrcode.png" {
		t.Errorf("expected the photo to be named qrcode.png, got : %q", photo.Name)
	}
	if photo.Data == nil {
		t.Error("expected the photo payload to carry the rendered QR code")
	}
}

func TestSendQRCodeToOwnerReturnsTelegramErrors(t *testing.T) {
	sendErr := errors.New("telegram : 403 bot can't initiate conversation with a user")
	client := &fakeBotClient{err: sendErr}
	bot := newTestBot(t, client)

	err := sendQRCodeToOwner(bot, testOwnerID, testQRCode)
	if err == nil {
		t.Fatal("expected an error when telegram rejects the QR code photo")
	}

	if !errors.Is(err, sendErr) {
		t.Errorf("expected the telegram error to be wrapped, got : %v", err)
	}
}
