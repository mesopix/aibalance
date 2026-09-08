package aibalance

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	configmanager "github.com/mesopix/go-config-manager"

	"aibalance/config"
)

// defaultAutoRefreshInterval reuses the retired C++ GUI's 300s default. The
// canonical default lives in config/gui_settings.json.example; this constant
// only backstops documents that omit a service or the interval field.
const defaultAutoRefreshInterval = 300 * time.Second

// guiSettingsSchemaVersion is carried in meta.version of the two-layer
// config.json document managed by go-config-manager. Version 2 moved the
// .env.local values (DeepSeek key, CDP endpoints) into this document.
const guiSettingsSchemaVersion = "2"

// appName identifies this application's config directory under
// os.UserConfigDir(); it is stable across binary renames.
const appName = "aibalance"

// ServiceSetting is one service's resolved entry from the config document.
type ServiceSetting struct {
	Enabled             bool
	AutoRefreshInterval time.Duration
}

// GUISettings is the resolved config document: the environment fields
// (DeepSeek API key and the two CDP endpoints, merged from the retired
// .env.local) and per-service overrides. Services absent from the document
// fall back to disabled with the default interval: every service is opt-in.
// Auto-refresh is always on; only the per-service interval is configurable.
type GUISettings struct {
	DeepSeekAPIKey string
	ChromeCDPURL   string
	ChromeCDPURL2  string
	Services       map[string]ServiceSetting
}

// guiSettingsDocument mirrors the fields layer of the on-disk JSON; pointer
// fields distinguish an absent entry (fall back to the default) from an
// explicit false or 0. The retired auto_refresh key is left as an unknown
// field: standard decoding ignores it, so old files load unchanged.
type guiSettingsDocument struct {
	DeepSeekAPIKey string                        `json:"deepseek_api_key"`
	ChromeCDPURL   string                        `json:"chrome_cdp_url"`
	ChromeCDPURL2  string                        `json:"chrome_cdp_url_2"`
	Services       map[string]guiServiceDocument `json:"services"`
}

type guiServiceDocument struct {
	Enabled                    *bool `json:"enabled"`
	AutoRefreshIntervalSeconds *int  `json:"auto_refresh_interval_seconds"`
}

// GUISettingsPath returns the config.json path inside the user config
// directory. It loads the config manager solely to read the path; callers
// needing the full config should use LoadGUISettings instead.
func GUISettingsPath() string {
	return filepath.Join(UserConfigDirectory(), appName, "config.json")
}

// userConfigDirectory resolves the base directory for this app's config,
// mirroring os.UserConfigDir but returning empty on error so tests can
// redirect via t.Setenv without panicking.
func userConfigDirectory() string {
	return UserConfigDirectory()
}

// assembleConfigManager builds a Manager bound to <user config
// dir>/aibalance/config.json with the embedded example as the first-run
// template, and registers the version chain so Load validates or upgrades
// legacy documents on disk.
func assembleConfigManager() (*configmanager.Manager, error) {
	manager := configmanager.NewManager()
	if err := manager.Init("", appName, ""); err != nil {
		return nil, err
	}
	if err := manager.RegisterDefaults(config.GUISettingsExample); err != nil {
		return nil, err
	}
	if err := manager.SetCurrentVersion(guiSettingsSchemaVersion); err != nil {
		return nil, err
	}
	// v1 predates the environment fields and versionless documents predate
	// the schema version itself; both keep their fields because absent
	// keys decode to the fallbacks resolveFromDocument already applies.
	keepFields := func(fields map[string]any) (map[string]any, error) {
		return fields, nil
	}
	if err := manager.RegisterUpgrader("1", guiSettingsSchemaVersion, keepFields); err != nil {
		return nil, err
	}
	if err := manager.RegisterUpgrader(configmanager.UnknownVersion, guiSettingsSchemaVersion, keepFields); err != nil {
		return nil, err
	}
	return manager, nil
}

// LoadGUISettings reads config.json via go-config-manager. A missing file
// is materialized from the embedded example before parsing, making that
// example the single source of defaults, and documents at an older schema
// version are upgraded and written back. Corrupt files surface as
// *configmanager.CorruptConfigError; callers must treat this as fatal
// rather than falling back to defaults.
func LoadGUISettings() (GUISettings, error) {
	manager, assembleErr := assembleConfigManager()
	if assembleErr != nil {
		return fallbackSettingsWithError(assembleErr)
	}
	loaded, loadErr := manager.Load()
	if loadErr != nil {
		var corruptErr *configmanager.CorruptConfigError
		if errors.As(loadErr, &corruptErr) {
			return GUISettings{}, loadErr
		}
		return fallbackSettingsWithError(loadErr)
	}

	settings, decodeErr := decodeFromManager(loaded)
	if decodeErr != nil {
		return fallbackSettingsWithError(decodeErr)
	}
	return settings, nil
}

// fallbackSettingsWithError decodes the embedded example as the settings
// fallback for non-fatal load failures and joins the triggering error;
// callers warn and continue with the defaults.
func fallbackSettingsWithError(loadErr error) (GUISettings, error) {
	fallback, decodeErr := decodeGUISettings(config.GUISettingsExample)
	return fallback, errors.Join(loadErr, decodeErr)
}

// decodeFromManager extracts the fields layer from a loaded Config and
// decodes it into GUISettings.
func decodeFromManager(manager *configmanager.Config) (GUISettings, error) {
	var document guiSettingsDocument
	if err := manager.DecodeFields(&document); err != nil {
		return GUISettings{}, err
	}
	return resolveFromDocument(document), nil
}

// decodeGUISettings decodes raw JSON bytes (used for the embedded example
// and fallback paths) into GUISettings.
func decodeGUISettings(documentBytes []byte) (GUISettings, error) {
	// The embedded example is a two-layer document; extract the fields layer
	// so the same resolver handles both legacy flat and new layered inputs.
	var envelope struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(documentBytes, &envelope); err == nil && len(envelope.Fields) > 0 {
		var document guiSettingsDocument
		if decodeErr := json.Unmarshal(envelope.Fields, &document); decodeErr != nil {
			return GUISettings{}, decodeErr
		}
		return resolveFromDocument(document), nil
	}

	// Legacy flat document (tests, migration fixtures).
	var document guiSettingsDocument
	if decodeErr := json.Unmarshal(documentBytes, &document); decodeErr != nil {
		return GUISettings{}, decodeErr
	}
	return resolveFromDocument(document), nil
}

// resolveFromDocument converts a decoded document into GUISettings,
// applying per-field fallbacks for absent entries.
func resolveFromDocument(document guiSettingsDocument) GUISettings {
	settings := GUISettings{
		DeepSeekAPIKey: document.DeepSeekAPIKey,
		ChromeCDPURL:   document.ChromeCDPURL,
		ChromeCDPURL2:  document.ChromeCDPURL2,
	}
	settings.Services = make(map[string]ServiceSetting, len(document.Services))
	for serviceName, serviceDocument := range document.Services {
		setting := ServiceSetting{
			Enabled:             false,
			AutoRefreshInterval: defaultAutoRefreshInterval,
		}
		if serviceDocument.Enabled != nil {
			setting.Enabled = *serviceDocument.Enabled
		}
		if serviceDocument.AutoRefreshIntervalSeconds != nil &&
			*serviceDocument.AutoRefreshIntervalSeconds > 0 {
			setting.AutoRefreshInterval =
				time.Duration(*serviceDocument.AutoRefreshIntervalSeconds) * time.Second
		}
		settings.Services[serviceName] = setting
	}
	return settings
}

// SaveGUISettings writes config.json atomically via go-config-manager,
// materializing every known service's resolved setting; service IDs
// outside ServiceOrder are dropped. The loaded config already carries the
// current meta.version: fresh files come from the versioned template and
// legacy files are promoted by the version chain during Load.
func SaveGUISettings(settings GUISettings) error {
	document := guiSettingsDocument{
		DeepSeekAPIKey: settings.DeepSeekAPIKey,
		ChromeCDPURL:   settings.ChromeCDPURL,
		ChromeCDPURL2:  settings.ChromeCDPURL2,
		Services:       make(map[string]guiServiceDocument, len(ServiceOrder)),
	}
	for _, serviceName := range ServiceOrder {
		enabled := settings.IsServiceEnabled(serviceName)
		intervalSeconds := int(settings.AutoRefreshInterval(serviceName).Seconds())
		document.Services[serviceName] = guiServiceDocument{
			Enabled:                    &enabled,
			AutoRefreshIntervalSeconds: &intervalSeconds,
		}
	}

	manager, err := assembleConfigManager()
	if err != nil {
		return fmt.Errorf("load config for save: %w", err)
	}
	loaded, loadErr := manager.Load()
	if loadErr != nil {
		return fmt.Errorf("load config for save: %w", loadErr)
	}
	if setErr := loaded.SetFieldsFrom(document); setErr != nil {
		return setErr
	}
	return loaded.Save()
}

// IsServiceEnabled reports whether the service participates in refresh and
// display; unlisted services default to disabled — every service must be
// opted in by the user.
func (settings GUISettings) IsServiceEnabled(serviceName string) bool {
	setting, exists := settings.Services[serviceName]
	if !exists {
		return false
	}
	return setting.Enabled
}

// EnabledServices returns the enabled services in canonical order.
func (settings GUISettings) EnabledServices() []string {
	enabled := make([]string, 0, len(ServiceOrder))
	for _, serviceName := range ServiceOrder {
		if settings.IsServiceEnabled(serviceName) {
			enabled = append(enabled, serviceName)
		}
	}
	return enabled
}

// AutoRefreshInterval returns the service's refresh interval; unlisted
// services and non-positive configured values fall back to the default.
func (settings GUISettings) AutoRefreshInterval(serviceName string) time.Duration {
	setting, exists := settings.Services[serviceName]
	if !exists || setting.AutoRefreshInterval <= 0 {
		return defaultAutoRefreshInterval
	}
	return setting.AutoRefreshInterval
}
