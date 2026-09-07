package main

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"aibalance/internal/aibalance"
)

// TestResolveGUISettingsCarriesEnvironmentFields guards the config editor
// save path: the loaded base's environment fields must survive the fold so
// saving never wipes the migrated DeepSeek key or CDP endpoints.
func TestResolveGUISettingsCarriesEnvironmentFields(t *testing.T) {
	base := aibalance.GUISettings{
		DeepSeekAPIKey: "sk-keep",
		ChromeCDPURL:   "http://127.0.0.1:9222",
		ChromeCDPURL2:  "http://127.0.0.1:9333",
	}
	serviceCount := len(aibalance.ServiceOrder)
	resolved := resolveGUISettings(base, make([]bool, serviceCount), make([]time.Duration, serviceCount))

	if resolved.DeepSeekAPIKey != base.DeepSeekAPIKey {
		t.Errorf("DeepSeekAPIKey = %q, want %q", resolved.DeepSeekAPIKey, base.DeepSeekAPIKey)
	}
	if resolved.ChromeCDPURL != base.ChromeCDPURL {
		t.Errorf("ChromeCDPURL = %q, want %q", resolved.ChromeCDPURL, base.ChromeCDPURL)
	}
	if resolved.ChromeCDPURL2 != base.ChromeCDPURL2 {
		t.Errorf("ChromeCDPURL2 = %q, want %q", resolved.ChromeCDPURL2, base.ChromeCDPURL2)
	}
}

// TestRunConfigMenuDiscardNeedsSecondQuit guards the anti-data-loss guard:
// after an edit, the first q only warns; the second discards without saving.
func TestRunConfigMenuDiscardNeedsSecondQuit(t *testing.T) {
	saveCalls := 0
	saveSettings := func(aibalance.GUISettings) error { saveCalls++; return nil }

	menuErr := runConfigMenu(bufio.NewReader(strings.NewReader("1\nq\nq\n")),
		aibalance.GUISettings{}, saveSettings)

	if menuErr != nil {
		t.Fatalf("runConfigMenu error: %v", menuErr)
	}
	if saveCalls != 0 {
		t.Errorf("save called %d times, want 0 (edits discarded)", saveCalls)
	}
}

// TestRunConfigMenuCleanQuitExitsImmediately verifies a lone q still quits
// right away when nothing was edited; unrecognized commands must not count
// as edits.
func TestRunConfigMenuCleanQuitExitsImmediately(t *testing.T) {
	saveCalls := 0
	saveSettings := func(aibalance.GUISettings) error { saveCalls++; return nil }

	menuErr := runConfigMenu(bufio.NewReader(strings.NewReader("nope\nq\n")),
		aibalance.GUISettings{}, saveSettings)

	if menuErr != nil {
		t.Fatalf("runConfigMenu error: %v", menuErr)
	}
	if saveCalls != 0 {
		t.Errorf("save called %d times, want 0", saveCalls)
	}
}

// TestRunConfigMenuSaveAfterQuitWarning verifies s still saves after the
// discard warning, carrying the in-menu service toggle into the save.
func TestRunConfigMenuSaveAfterQuitWarning(t *testing.T) {
	saveCalls := 0
	var lastSaved aibalance.GUISettings
	saveSettings := func(settings aibalance.GUISettings) error {
		saveCalls++
		lastSaved = settings
		return nil
	}

	menuErr := runConfigMenu(bufio.NewReader(strings.NewReader("1\nq\ns\n")),
		aibalance.GUISettings{}, saveSettings)

	if menuErr != nil {
		t.Fatalf("runConfigMenu error: %v", menuErr)
	}
	if saveCalls != 1 {
		t.Fatalf("save called %d times, want 1", saveCalls)
	}
	if !lastSaved.IsServiceEnabled(aibalance.ServiceOrder[0]) {
		t.Errorf("saved %s = disabled, want enabled (toggled in the menu)",
			aibalance.ServiceOrder[0])
	}
}

// TestRunConfigMenuEOFWithEditsDoesNotSave verifies EOF after edits exits
// the menu without invoking the saver.
func TestRunConfigMenuEOFWithEditsDoesNotSave(t *testing.T) {
	saveCalls := 0
	saveSettings := func(aibalance.GUISettings) error { saveCalls++; return nil }

	menuErr := runConfigMenu(bufio.NewReader(strings.NewReader("1\n")),
		aibalance.GUISettings{}, saveSettings)

	if menuErr != nil {
		t.Fatalf("runConfigMenu error: %v", menuErr)
	}
	if saveCalls != 0 {
		t.Errorf("save called %d times, want 0", saveCalls)
	}
}
