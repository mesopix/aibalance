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

// GUISettings is the resolved config document: the global auto_refresh
// switch, the environment fields (DeepSeek API key and the two CDP
// endpoints, merged from the retired .env.local), and per-service
// overrides. Services absent from the document fall back to enabled with
// the default interval.
type GUISettings struct {
	AutoRefresh    bool
	DeepSeekAPIKey string
	ChromeCDPURL   string
	ChromeCDPURL2  string
	Services       map[string]ServiceSetting
}

// guiSettingsDocument mirrors the fields layer of the on-disk JSON; pointer
// fields distinguish an absent entry (fall back to the default) from an
// explicit false or 0.
type guiSettingsDocument struct {
	AutoRefresh    bool                          `json:"auto_refresh"`
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

// LoadGUISettings reads config.json via go-config-manager. A missing file
// is materialized from the embedded example before parsing, making that
// example the single source of defaults. Corrupt files surface as
// *configmanager.CorruptConfigError; callers must treat this as fatal
// rather than falling back to defaults.
func LoadGUISettings() (GUISettings, error) {
	manager, err := configmanager.LoadAppConfig(appName, config.GUISettingsExample)
	if err != nil {
		var corruptErr *configmanager.CorruptConfigError
		if errors.As(err, &corruptErr) {
			return GUISettings{}, err
		}
		fallback, decodeErr := decodeGUISettings(config.GUISettingsExample)
		return fallback, errors.Join(err, decodeErr)
	}

	settings, decodeErr := decodeFromManager(manager)
	if decodeErr != nil {
		fallback, fallbackErr := decodeGUISettings(config.GUISettingsExample)
		return fallback, errors.Join(decodeErr, fallbackErr)
	}
	return settings, nil
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
		AutoRefresh:    document.AutoRefresh,
		DeepSeekAPIKey: document.DeepSeekAPIKey,
		ChromeCDPURL:   document.ChromeCDPURL,
		ChromeCDPURL2:  document.ChromeCDPURL2,
	}
	settings.Services = make(map[string]ServiceSetting, len(document.Services))
	for serviceName, serviceDocument := range document.Services {
		setting := ServiceSetting{
			Enabled:             true,
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
// outside ServiceOrder are dropped. The meta.version is always set to
// guiSettingsSchemaVersion so migrated documents carry the current schema.
func SaveGUISettings(settings GUISettings) error {
	document := guiSettingsDocument{
		AutoRefresh:    settings.AutoRefresh,
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

	manager, err := configmanager.LoadAppConfig(appName, config.GUISettingsExample)
	if err != nil {
		return fmt.Errorf("load config for save: %w", err)
	}
	if setErr := manager.SetFieldsFrom(document); setErr != nil {
		return setErr
	}
	// Always stamp the current schema version on save so migrated v1
	// documents are promoted and future loads see the correct version.
	meta := manager.Meta()
	meta["version"] = guiSettingsSchemaVersion
	return manager.Save()
}

// IsServiceEnabled reports whether the service participates in refresh and
// display; unlisted services default to enabled.
func (settings GUISettings) IsServiceEnabled(serviceName string) bool {
	setting, exists := settings.Services[serviceName]
	if !exists {
		return true
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
