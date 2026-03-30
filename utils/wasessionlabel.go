package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// waDeviceTemplates are plausible browser + OS strings shown as the linked device name (WhatsApp DeviceProps.Os).
var waDeviceTemplates = []struct {
	product, version, platform string
}{
	{"Chrome", "121.0.6167.189", "Windows NT 10.0; Win64; x64"},
	{"Chrome", "120.0.6099.216", "macOS 14.3"},
	{"Microsoft Edge", "120.0.2210.133", "Windows NT 10.0"},
	{"Chrome", "119.0.6045.200", "Windows NT 10.0; Win64; x64"},
	{"Firefox", "122.0", "Ubuntu; Linux x86_64"},
	{"Brave", "1.62.165", "Windows NT 10.0"},
	{"Chrome", "121.0.6167.85", "macOS 14.2.1"},
	{"Microsoft Edge", "121.0.2277.98", "Windows NT 10.0; Win64; x64"},
}

// RandomWhatsAppDeviceLabel returns a unique-looking browser/OS label for WhatsApp linked devices.
// A random suffix keeps wa_session_name unique in the DB without tying it to the user-chosen bridge label.
func RandomWhatsAppDeviceLabel() (string, error) {
	var noise [8]byte
	if _, err := rand.Read(noise[:]); err != nil {
		return "", err
	}
	idx := int(noise[0]) % len(waDeviceTemplates)
	t := waDeviceTemplates[idx]
	tag := hex.EncodeToString(noise[2:8]) // 12 hex chars
	return fmt.Sprintf("%s %s (%s) ·%s", t.product, t.version, t.platform, tag), nil
}
