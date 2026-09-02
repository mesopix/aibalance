package aibalance

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"aibalance/config"
)

// useTempConfigDir redirects os.UserConfigDir to a temp directory so tests
// never touch the real user config. It covers Windows (AppData/LOCALAPPDATA),
// Linux (XDG_CONFIG_HOME/XDG_DATA_HOME), and macOS (HOME).
func useTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("HOME", dir)
}

// writeGUISettings writes a raw config.json document into the redirected
// user config directory for tests that need a pre-existing file.
func writeGUISettings(t *testing.T, document string) {
	t.Helper()
	useTempConfigDir(t)
	configDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), appName)
	if mkdirErr := os.MkdirAll(configDir, 0o700); mkdirErr != nil {
		t.Fatalf("mkdir config dir: %v", mkdirErr)
	}
	settingsPath := filepath.Join(configDir, "config.json")
	if writeErr := os.WriteFile(settingsPath, []byte(document), 0o600); writeErr != nil {
		t.Fatalf("write config.json: %v", writeErr)
	}
}

func TestLoadGUISettingsMissingFileMaterializesExample(t *testing.T) {
	useTempConfigDir(t)

	settings, err := LoadGUISettings()
	if err != nil {
		t.Fatalf("LoadGUISettings() error: %v", err)
	}
	if settings.AutoRefresh {
		t.Error("AutoRefresh should default to false when the file is missing")
	}
	if enabled := settings.EnabledServices(); len(enabled) != len(ServiceOrder) {
		t.Errorf("EnabledServices() = %v, want all %d services", enabled, len(ServiceOrder))
	}
	for _, serviceName := range ServiceOrder {
		if !settings.IsServiceEnabled(serviceName) {
			t.Errorf("IsServiceEnabled(%q) = false, want true by default", serviceName)
		}
		if interval := settings.AutoRefreshInterval(serviceName); interval != defaultAutoRefreshInterval {
			t.Errorf("AutoRefreshInterval(%q) = %v, want default %v", serviceName, interval, defaultAutoRefreshInterval)
		}
	}

	// The embedded example is written to disk so users can edit it directly.
	// Verify the materialized file parses to the same settings as the example;
	// SaveGUISettings always stamps meta.version so raw-byte equality is not
	// expected after a save round-trip.
	if _, statErr := os.Stat(GUISettingsPath()); statErr != nil {
		t.Fatalf("materialized config.json missing: %v", statErr)
	}
	materialized, loadErr := LoadGUISettings()
	if loadErr != nil {
		t.Fatalf("reload materialized config.json: %v", loadErr)
	}
	exampleSettings, decodeErr := decodeGUISettings(config.GUISettingsExample)
	if decodeErr != nil {
		t.Fatalf("decode embedded example: %v", decodeErr)
	}
	if materialized.AutoRefresh != exampleSettings.AutoRefresh ||
		materialized.DeepSeekAPIKey != exampleSettings.DeepSeekAPIKey ||
		materialized.ChromeCDPURL != exampleSettings.ChromeCDPURL ||
		materialized.ChromeCDPURL2 != exampleSettings.ChromeCDPURL2 {
		t.Errorf("materialized settings %+v differ from example %+v", materialized, exampleSettings)
	}
	if len(materialized.Services) != len(exampleSettings.Services) {
		t.Errorf("materialized services count = %d, want %d", len(materialized.Services), len(exampleSettings.Services))
	}
}

// TestEmbeddedGUISettingsExampleMatchesRegistry keeps the example file the
// single source of defaults: it must parse, cover exactly ServiceOrder, and
// carry sane defaults for every service.
func TestEmbeddedGUISettingsExampleMatchesRegistry(t *testing.T) {
	settings, decodeErr := decodeGUISettings(config.GUISettingsExample)
	if decodeErr != nil {
		t.Fatalf("embedded example does not parse: %v", decodeErr)
	}
	if len(settings.Services) != len(ServiceOrder) {
		t.Fatalf("example covers %d services, want exactly the %d in ServiceOrder",
			len(settings.Services), len(ServiceOrder))
	}
	for _, serviceName := range ServiceOrder {
		setting, exists := settings.Services[serviceName]
		if !exists {
			t.Errorf("example is missing service %q", serviceName)
			continue
		}
		if !setting.Enabled {
			t.Errorf("example should enable %q by default", serviceName)
		}
		if setting.AutoRefreshInterval != defaultAutoRefreshInterval {
			t.Errorf("example interval for %q = %v, want default %v",
				serviceName, setting.AutoRefreshInterval, defaultAutoRefreshInterval)
		}
	}
	if settings.AutoRefresh {
		t.Error("example should default auto_refresh to false")
	}
	if settings.DeepSeekAPIKey != "" {
		t.Error("example should ship an empty deepseek_api_key so materializing stays inert")
	}
	if settings.ChromeCDPURL != "http://127.0.0.1:9222" {
		t.Errorf("example chrome_cdp_url = %q, want the 9222 default", settings.ChromeCDPURL)
	}
	if settings.ChromeCDPURL2 != "http://127.0.0.1:9333" {
		t.Errorf("example chrome_cdp_url_2 = %q, want the 9333 default", settings.ChromeCDPURL2)
	}
}

func TestLoadGUISettingsFullDocument(t *testing.T) {
	writeGUISettings(t, `{
		"meta": {"version": "2"},
		"fields": {
			"auto_refresh": true,
			"deepseek_api_key": "sk-doc",
			"chrome_cdp_url": "http://127.0.0.1:8222",
			"chrome_cdp_url_2": "http://127.0.0.1:9444",
			"services": {
				"qwen_token_plan": {"auto_refresh_interval_seconds": 120, "enabled": true},
				"chatgpt_codex": {"auto_refresh_interval_seconds": 300, "enabled": false},
				"z_ai_coding_plan_2": {"auto_refresh_interval_seconds": 300, "enabled": false},
				"deepseek_api": {"auto_refresh_interval_seconds": 300, "enabled": true}
			}
		}
	}`)

	settings, err := LoadGUISettings()
	if err != nil {
		t.Fatalf("LoadGUISettings() error: %v", err)
	}
	if !settings.AutoRefresh {
		t.Error("AutoRefresh = false, want true")
	}
	if settings.DeepSeekAPIKey != "sk-doc" {
		t.Errorf("DeepSeekAPIKey = %q, want %q", settings.DeepSeekAPIKey, "sk-doc")
	}
	if settings.ChromeCDPURL != "http://127.0.0.1:8222" {
		t.Errorf("ChromeCDPURL = %q, want the document value", settings.ChromeCDPURL)
	}
	if settings.ChromeCDPURL2 != "http://127.0.0.1:9444" {
		t.Errorf("ChromeCDPURL2 = %q, want the document value", settings.ChromeCDPURL2)
	}

	wantEnabled := []string{"qwen_token_plan", "bigmodel_coding_plan", "bigmodel_coding_plan_2", "z_ai_coding_plan",
		"kimi_coding_plan", "qoder_team_credit", "deepseek_api"}
	enabled := settings.EnabledServices()
	if len(enabled) != len(wantEnabled) {
		t.Fatalf("EnabledServices() = %v, want %v", enabled, wantEnabled)
	}
	for serviceIndex, serviceName := range wantEnabled {
		if enabled[serviceIndex] != serviceName {
			t.Errorf("EnabledServices()[%d] = %q, want %q", serviceIndex, enabled[serviceIndex], serviceName)
		}
	}

	if interval := settings.AutoRefreshInterval("qwen_token_plan"); interval != 120*time.Second {
		t.Errorf("AutoRefreshInterval(qwen_token_plan) = %v, want 2m", interval)
	}
	if interval := settings.AutoRefreshInterval("deepseek_api"); interval != 300*time.Second {
		t.Errorf("AutoRefreshInterval(deepseek_api) = %v, want 5m", interval)
	}
}

func TestLoadGUISettingsPartialAndInvalidEntries(t *testing.T) {
	writeGUISettings(t, `{
		"meta": {},
		"fields": {
			"auto_refresh": true,
			"services": {
				"qwen_token_plan": {"enabled": false},
				"kimi_coding_plan": {"auto_refresh_interval_seconds": 0},
				"qoder_team_credit": {"auto_refresh_interval_seconds": -5, "enabled": true},
				"retired_service": {"enabled": false}
			}
		}
	}`)

	settings, err := LoadGUISettings()
	if err != nil {
		t.Fatalf("LoadGUISettings() error: %v", err)
	}
	if settings.IsServiceEnabled("qwen_token_plan") {
		t.Error("IsServiceEnabled(qwen_token_plan) = true, want false")
	}
	// Entries without an enabled field default to enabled.
	if !settings.IsServiceEnabled("kimi_coding_plan") {
		t.Error("IsServiceEnabled(kimi_coding_plan) = false, want default true")
	}
	// Non-positive intervals fall back to the default.
	if interval := settings.AutoRefreshInterval("kimi_coding_plan"); interval != defaultAutoRefreshInterval {
		t.Errorf("AutoRefreshInterval(kimi_coding_plan) = %v, want default %v", interval, defaultAutoRefreshInterval)
	}
	if interval := settings.AutoRefreshInterval("qoder_team_credit"); interval != defaultAutoRefreshInterval {
		t.Errorf("AutoRefreshInterval(qoder_team_credit) = %v, want default %v", interval, defaultAutoRefreshInterval)
	}
	// Unknown service IDs in the document are ignored.
	if !settings.IsServiceEnabled("z_ai_coding_plan") {
		t.Error("IsServiceEnabled(z_ai_coding_plan) = false, want true (unlisted)")
	}
}

func TestLoadGUISettingsMalformedDocument(t *testing.T) {
	writeGUISettings(t, `{"meta": {}, "fields": {"auto_refresh": true, "services":`)

	_, err := LoadGUISettings()
	if err == nil {
		t.Fatal("LoadGUISettings() should fail on malformed JSON")
	}
}

// TestLoadGUISettingsV1DocumentYieldsEmptyEnvironmentFields pins the
// version 1 compatibility: documents written before the .env.local merge
// decode with empty environment fields.
func TestLoadGUISettingsV1DocumentYieldsEmptyEnvironmentFields(t *testing.T) {
	writeGUISettings(t, `{"meta": {"version": "1"}, "fields": {"auto_refresh": false, "services": {}}}`)

	settings, err := LoadGUISettings()
	if err != nil {
		t.Fatalf("LoadGUISettings() error: %v", err)
	}
	if settings.DeepSeekAPIKey != "" || settings.ChromeCDPURL != "" || settings.ChromeCDPURL2 != "" {
		t.Errorf("version 1 document decoded environment fields (%q, %q, %q), want all empty",
			settings.DeepSeekAPIKey, settings.ChromeCDPURL, settings.ChromeCDPURL2)
	}
}

func TestSaveGUISettingsRoundTrip(t *testing.T) {
	useTempConfigDir(t)

	saved := GUISettings{
		AutoRefresh:    true,
		DeepSeekAPIKey: "sk-roundtrip",
		ChromeCDPURL:   "http://127.0.0.1:9222",
		ChromeCDPURL2:  "http://127.0.0.1:9333",
		Services: map[string]ServiceSetting{
			"qwen_token_plan": {Enabled: true, AutoRefreshInterval: 120 * time.Second},
			"chatgpt_codex":   {Enabled: false, AutoRefreshInterval: defaultAutoRefreshInterval},
			"retired_service": {Enabled: false, AutoRefreshInterval: defaultAutoRefreshInterval},
		},
	}
	if saveErr := SaveGUISettings(saved); saveErr != nil {
		t.Fatalf("SaveGUISettings() error: %v", saveErr)
	}

	loaded, err := LoadGUISettings()
	if err != nil {
		t.Fatalf("LoadGUISettings() error: %v", err)
	}
	if !loaded.AutoRefresh {
		t.Error("AutoRefresh = false, want true")
	}
	if loaded.DeepSeekAPIKey != "sk-roundtrip" {
		t.Errorf("DeepSeekAPIKey = %q, want %q", loaded.DeepSeekAPIKey, "sk-roundtrip")
	}
	if loaded.ChromeCDPURL != "http://127.0.0.1:9222" {
		t.Errorf("ChromeCDPURL = %q, want the saved value", loaded.ChromeCDPURL)
	}
	if loaded.ChromeCDPURL2 != "http://127.0.0.1:9333" {
		t.Errorf("ChromeCDPURL2 = %q, want the saved value", loaded.ChromeCDPURL2)
	}
	if !loaded.IsServiceEnabled("qwen_token_plan") {
		t.Error("IsServiceEnabled(qwen_token_plan) = false, want true")
	}
	if loaded.IsServiceEnabled("chatgpt_codex") {
		t.Error("IsServiceEnabled(chatgpt_codex) = true, want false")
	}
	if interval := loaded.AutoRefreshInterval("qwen_token_plan"); interval != 120*time.Second {
		t.Errorf("AutoRefreshInterval(qwen_token_plan) = %v, want 2m", interval)
	}
	// Service IDs outside ServiceOrder are dropped rather than written back.
	if _, exists := loaded.Services["retired_service"]; exists {
		t.Error("retired_service should be dropped by SaveGUISettings")
	}
}
