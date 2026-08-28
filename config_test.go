package main

import (
	"testing"
	"time"

	"aibalance/internal/aibalance"
)

// TestResolveGUISettingsCarriesEnvironmentFields guards the config editor
// save path: the loaded base's environment fields must survive the fold so
// saving never wipes the migrated DeepSeek key or CDP endpoints.
func TestResolveGUISettingsCarriesEnvironmentFields(t *testing.T) {
	base := aibalance.GUISettings{
		AutoRefresh:    true,
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
	if !resolved.AutoRefresh {
		t.Error("AutoRefresh = false, want the base value")
	}
}
