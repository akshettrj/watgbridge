package database

import (
	"testing"
)

func TestEphemeralSettingsStorageAndRetrieval(t *testing.T) {
	// This test verifies that ephemeral settings can be stored and retrieved
	// for a WhatsApp chat, which is the foundation for issue #46.

	// Note: This test requires database initialization which is done in main.
	// For now, this is a placeholder to document expected behavior.

	testChatId := "1234567890@g.us"

	// Store ephemeral settings
	err := UpdateEphemeralSettings(testChatId, true, 86400)
	if err != nil {
		t.Skipf("Database not initialized in test context: %v", err)
	}

	// Retrieve ephemeral settings
	isEphemeral, timer, found, err := GetEphemeralSettings(testChatId)
	if err != nil {
		t.Fatalf("Failed to get ephemeral settings: %v", err)
	}

	if !found {
		t.Fatal("Ephemeral settings not found after being stored")
	}

	if !isEphemeral {
		t.Error("Expected isEphemeral to be true")
	}

	if timer != 86400 {
		t.Errorf("Expected timer to be 86400, got %d", timer)
	}
}
