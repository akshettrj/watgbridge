package utils

import (
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// ParsePhoneToWhatsAppUser parses a human-entered phone into E.164 digits without '+' for WhatsApp APIs.
// defaultRegion is an ISO 3166-1 alpha-2 code (e.g. KZ, US) used when the number has no country code; if empty, only international (+ or 00…) input is accepted.
func ParsePhoneToWhatsAppUser(raw string, defaultRegion string) (digits string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "00") {
		raw = "+" + raw[2:]
	}
	region := strings.TrimSpace(defaultRegion)
	if region == "" && !strings.HasPrefix(raw, "+") {
		return "", false
	}
	if region == "" {
		region = "US" // ignored when the number includes a country calling code
	}
	num, err := phonenumbers.Parse(raw, region)
	if err != nil || !phonenumbers.IsValidNumber(num) {
		return "", false
	}
	e164 := phonenumbers.Format(num, phonenumbers.E164)
	if len(e164) < 2 || e164[0] != '+' {
		return "", false
	}
	return e164[1:], true
}

// WaCheckResolvePhoneUser accepts the same inputs as /check: legacy WaParseJID forms plus libphonenumber-valid numbers.
func WaCheckResolvePhoneUser(phoneInput string, defaultRegion string) (userDigits string, ok bool) {
	phoneInput = strings.TrimSpace(phoneInput)
	if phoneInput == "" {
		return "", false
	}
	if jid, jok := WaParseJID(phoneInput); jok && jid.User != "" {
		return jid.User, true
	}
	return ParsePhoneToWhatsAppUser(phoneInput, defaultRegion)
}
