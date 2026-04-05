package mainbot

// NormalizeTargetChatID converts common user-entered supergroup/channel forms to Telegram’s full negative id (-100…).
// Already-negative values are returned unchanged (legacy groups, channels, etc.).
func NormalizeTargetChatID(id int64) int64 {
	if id < 0 {
		return id
	}
	const trillion = int64(1_000_000_000_000)
	// Some tools show the full id as a positive number (e.g. 1001234567890).
	if id >= trillion {
		return -id
	}
	// Bare supergroup id (digits only, no "-100" prefix).
	return -trillion - id
}
