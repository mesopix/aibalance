package aibalance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeEnvLocal writes a raw .env.local into the redirected user config
// directory for migration tests.
func writeEnvLocal(t *testing.T, contents string) {
	t.Helper()
	if mkdirErr := os.MkdirAll(filepath.Dir(EnvLocalPath()), 0o700); mkdirErr != nil {
		t.Fatalf("mkdir user data dir: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(EnvLocalPath(), []byte(contents), 0o600); writeErr != nil {
		t.Fatalf("write .env.local: %v", writeErr)
	}
}

// restoreBridgeEnvironment snapshots the bridged variables and restores
// them when the test ends so applyGUISettingsEnvironment cannot leak state
// between tests.
func restoreBridgeEnvironment(t *testing.T) {
	t.Helper()
	for _, variableName := range []string{"DEEPSEEK_API_KEY", "CHROME_CDP_URL", "CHROME_CDP_URL_2"} {
		previous, wasSet := os.LookupEnv(variableName)
		t.Cleanup(func() {
			if wasSet {
				os.Setenv(variableName, previous)
			} else {
				os.Unsetenv(variableName)
			}
		})
	}
}

func TestParseEnvLocalSkipsCommentsAndStripsQuotes(t *testing.T) {
	assignments := parseEnvLocal([]byte(strings.Join([]string{
		"# comment line",
		`DEEPSEEK_API_KEY= "quoted key"`,
		"CHROME_CDP_URL=http://127.0.0.1:9222",
		"",
		"line_without_equals",
		`SINGLE_KEY='single'`,
	}, "\r\n")))
	want := map[string]string{
		"DEEPSEEK_API_KEY": "quoted key",
		"CHROME_CDP_URL":   "http://127.0.0.1:9222",
		"SINGLE_KEY":       "single",
	}
	if !reflect.DeepEqual(assignments, want) {
		t.Errorf("parseEnvLocal() = %v, want %v", assignments, want)
	}
}

func TestLoadStartupSettingsFreshMachineMaterializesSettingsOnly(t *testing.T) {
	useTempConfigDir(t)
	restoreBridgeEnvironment(t)

	// Ensure .env.local does not exist in the redirected user data dir;
	// otherwise a leftover from another test would be migrated in.
	envLocalPath := EnvLocalPath()
	if mkdirErr := os.MkdirAll(filepath.Dir(envLocalPath), 0o700); mkdirErr != nil {
		t.Fatalf("mkdir user data dir: %v", mkdirErr)
	}
	if removeErr := os.Remove(envLocalPath); removeErr != nil && !os.IsNotExist(removeErr) {
		t.Fatalf("remove stale .env.local: %v", removeErr)
	}

	settings, err := LoadStartupSettings()
	if err != nil {
		t.Fatalf("LoadStartupSettings() error: %v", err)
	}
	if settings.ChromeCDPURL != "http://127.0.0.1:9222" {
		t.Errorf("ChromeCDPURL = %q, want the 9222 template default", settings.ChromeCDPURL)
	}
	if settings.ChromeCDPURL2 != "http://127.0.0.1:9333" {
		t.Errorf("ChromeCDPURL2 = %q, want the 9333 template default", settings.ChromeCDPURL2)
	}
	if settings.DeepSeekAPIKey != "" {
		t.Errorf("DeepSeekAPIKey = %q, want the inert empty template value", settings.DeepSeekAPIKey)
	}

	if _, statErr := os.Stat(GUISettingsPath()); statErr != nil {
		t.Fatalf("first run should materialize config.json: %v", statErr)
	}
	// .env.local is retired: a fresh machine must never grow one again.
	if _, statErr := os.Stat(EnvLocalPath()); !os.IsNotExist(statErr) {
		t.Error("first run should not materialize .env.local")
	}
}

func TestMigrateEnvLocalMergesValuesAndDeletesFile(t *testing.T) {
	writeGUISettings(t, `{
		"meta": {"version": "1"},
		"fields": {
			"auto_refresh": true,
			"services": {"qwen_token_plan": {"enabled": true, "auto_refresh_interval_seconds": 120}}
		}
	}`)
	writeEnvLocal(t, strings.Join([]string{
		"# legacy template header",
		"DEEPSEEK_API_KEY=sk-legacy",
		"CHROME_CDP_URL=http://127.0.0.1:8222",
		"CHROME_CDP_URL_2=http://127.0.0.1:9444",
	}, "\n"))
	restoreBridgeEnvironment(t)

	settings, err := LoadStartupSettings()
	if err != nil {
		t.Fatalf("LoadStartupSettings() error: %v", err)
	}
	if settings.DeepSeekAPIKey != "sk-legacy" {
		t.Errorf("DeepSeekAPIKey = %q, want the migrated %q", settings.DeepSeekAPIKey, "sk-legacy")
	}
	if settings.ChromeCDPURL != "http://127.0.0.1:8222" {
		t.Errorf("ChromeCDPURL = %q, want the migrated value", settings.ChromeCDPURL)
	}
	if settings.ChromeCDPURL2 != "http://127.0.0.1:9444" {
		t.Errorf("ChromeCDPURL2 = %q, want the migrated value", settings.ChromeCDPURL2)
	}
	if !settings.AutoRefresh {
		t.Error("AutoRefresh = false, want the document's true")
	}
	if _, statErr := os.Stat(EnvLocalPath()); !os.IsNotExist(statErr) {
		t.Error("fully consumed .env.local should be deleted after migration")
	}

	// The merged values persist in the two-layer format with version "2".
	written, readErr := os.ReadFile(GUISettingsPath())
	if readErr != nil {
		t.Fatalf("read migrated config.json: %v", readErr)
	}
	var envelope struct {
		Meta struct {
			Version string `json:"version"`
		} `json:"meta"`
		Fields struct {
			DeepSeekAPIKey string `json:"deepseek_api_key"`
		} `json:"fields"`
	}
	if decodeErr := json.Unmarshal(written, &envelope); decodeErr != nil {
		t.Fatalf("migrated config.json does not parse: %v", decodeErr)
	}
	if envelope.Meta.Version != guiSettingsSchemaVersion {
		t.Errorf("meta.version = %q, want %q", envelope.Meta.Version, guiSettingsSchemaVersion)
	}
	if envelope.Fields.DeepSeekAPIKey != "sk-legacy" {
		t.Errorf("persisted deepseek_api_key = %q, want %q", envelope.Fields.DeepSeekAPIKey, "sk-legacy")
	}
}

func TestMigrateEnvLocalAllCommentedDeletesFileWithoutRewritingSettings(t *testing.T) {
	const settingsDocument = `{
		"meta": {},
		"fields": {
			"auto_refresh": true,
			"services": {"kimi_coding_plan": {"enabled": false, "auto_refresh_interval_seconds": 600}}
		}
	}`
	writeGUISettings(t, settingsDocument)
	writeEnvLocal(t, "# every assignment stays commented\n#DEEPSEEK_API_KEY=sk-unused\n")
	restoreBridgeEnvironment(t)

	if _, err := LoadStartupSettings(); err != nil {
		t.Fatalf("LoadStartupSettings() error: %v", err)
	}
	if _, statErr := os.Stat(EnvLocalPath()); !os.IsNotExist(statErr) {
		t.Error("all-commented .env.local should be deleted")
	}
	written, readErr := os.ReadFile(GUISettingsPath())
	if readErr != nil {
		t.Fatalf("read config.json: %v", readErr)
	}
	// Normalize whitespace for comparison since SaveGUISettings re-indents.
	var parsedOriginal, parsedWritten any
	if err := json.Unmarshal([]byte(settingsDocument), &parsedOriginal); err != nil {
		t.Fatalf("parse original: %v", err)
	}
	if err := json.Unmarshal(written, &parsedWritten); err != nil {
		t.Fatalf("parse written: %v", err)
	}
	if !reflect.DeepEqual(parsedOriginal, parsedWritten) {
		t.Error("config.json should only be rewritten when a field actually changed")
	}
}

func TestMigrateEnvLocalKeepsSettingsValueWhenFieldAlreadySet(t *testing.T) {
	writeGUISettings(t, `{
		"meta": {},
		"fields": {"auto_refresh": false, "deepseek_api_key": "sk-from-settings", "services": {}}
	}`)
	writeEnvLocal(t, "DEEPSEEK_API_KEY=sk-from-env\n")
	restoreBridgeEnvironment(t)

	settings, err := LoadStartupSettings()
	if err != nil {
		t.Fatalf("LoadStartupSettings() error: %v", err)
	}
	if settings.DeepSeekAPIKey != "sk-from-settings" {
		t.Errorf("DeepSeekAPIKey = %q, want the settings value to win", settings.DeepSeekAPIKey)
	}
	if _, statErr := os.Stat(EnvLocalPath()); !os.IsNotExist(statErr) {
		t.Error("consumed .env.local should be deleted even when settings won")
	}
}

func TestMigrateEnvLocalKeepsFileOnUnknownKeys(t *testing.T) {
	writeGUISettings(t, `{"meta": {}, "fields": {"auto_refresh": false, "services": {}}}`)
	writeEnvLocal(t, "DEEPSEEK_API_KEY=sk-legacy\nCUSTOM_KEY=1\n")
	restoreBridgeEnvironment(t)

	_, err := LoadStartupSettings()
	if err == nil {
		t.Fatal("unknown keys should surface an error so the user notices the file is no longer read")
	}
	if !strings.Contains(err.Error(), "CUSTOM_KEY") {
		t.Errorf("error should name the unconsumed key: %v", err)
	}
	if _, statErr := os.Stat(EnvLocalPath()); statErr != nil {
		t.Fatalf(".env.local with unknown keys must be kept: %v", statErr)
	}
	loaded, loadErr := LoadGUISettings()
	if loadErr != nil {
		t.Fatalf("LoadGUISettings() error: %v", loadErr)
	}
	if loaded.DeepSeekAPIKey != "sk-legacy" {
		t.Errorf("DeepSeekAPIKey = %q, want the known key still merged", loaded.DeepSeekAPIKey)
	}
}

func TestMigrateEnvLocalSkippedWhenSettingsMalformed(t *testing.T) {
	const brokenDocument = `{"meta": {}, "fields": {"auto_refresh": true, "services":`
	writeGUISettings(t, brokenDocument)
	writeEnvLocal(t, "DEEPSEEK_API_KEY=sk-legacy\n")
	restoreBridgeEnvironment(t)

	if _, err := LoadStartupSettings(); err == nil {
		t.Fatal("malformed config.json should surface the load error")
	}
	if _, statErr := os.Stat(EnvLocalPath()); statErr != nil {
		t.Fatalf(".env.local must survive for the next run: %v", statErr)
	}
	written, readErr := os.ReadFile(GUISettingsPath())
	if readErr != nil {
		t.Fatalf("read config.json: %v", readErr)
	}
	if string(written) != brokenDocument {
		t.Error("a failed load must not be followed by a merge-and-save overwrite")
	}
}

func TestApplyGUISettingsEnvironmentBridgesOnlyEmptyVars(t *testing.T) {
	restoreBridgeEnvironment(t)
	if setErr := os.Setenv("CHROME_CDP_URL", "http://real-env:9222"); setErr != nil {
		t.Fatalf("set CHROME_CDP_URL: %v", setErr)
	}

	settings := GUISettings{
		DeepSeekAPIKey: "sk-bridge",
		ChromeCDPURL:   "http://settings:9222",
		ChromeCDPURL2:  "http://settings:9333",
	}
	if err := applyGUISettingsEnvironment(settings); err != nil {
		t.Fatalf("applyGUISettingsEnvironment() error: %v", err)
	}
	if value := os.Getenv("CHROME_CDP_URL"); value != "http://real-env:9222" {
		t.Errorf("CHROME_CDP_URL = %q, want the pre-existing environment value", value)
	}
	if value := os.Getenv("DEEPSEEK_API_KEY"); value != "sk-bridge" {
		t.Errorf("DEEPSEEK_API_KEY = %q, want %q", value, "sk-bridge")
	}
	if value := os.Getenv("CHROME_CDP_URL_2"); value != "http://settings:9333" {
		t.Errorf("CHROME_CDP_URL_2 = %q, want %q", value, "http://settings:9333")
	}
}
