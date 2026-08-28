package aibalance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"aibalance/config"
)

// defaultAutoRefreshInterval reuses the retired C++ GUI's 300s default. The
// canonical default lives in config/gui_settings.json.example; this constant
// only backstops documents that omit a service or the interval field.
const defaultAutoRefreshInterval = 300 * time.Second

// Schema markers carried by gui_settings.json since the retired C++ GUI.
// Version 2 moved the .env.local values (DeepSeek key, CDP endpoints) into
// this document; version 1 files decode with those fields empty.
const (
	guiSettingsSchema        = "ai_credit.gui_settings"
	guiSettingsSchemaVersion = 2
)

// ServiceSetting is one service's resolved entry from gui_settings.json.
type ServiceSetting struct {
	Enabled             bool
	AutoRefreshInterval time.Duration
}

// GUISettings is the resolved gui_settings.json document: the global
// auto_refresh switch, the environment fields (DeepSeek API key and the two
// CDP endpoints, merged from the retired .env.local), and per-service
// overrides. Services absent from the document fall back to enabled with
// the default interval.
type GUISettings struct {
	AutoRefresh    bool
	DeepSeekAPIKey string
	ChromeCDPURL   string
	ChromeCDPURL2  string
	Services       map[string]ServiceSetting
}

// guiSettingsDocument mirrors the on-disk JSON; pointer fields distinguish
// an absent entry (fall back to the default) from an explicit false or 0.
type guiSettingsDocument struct {
	AutoRefresh    bool                          `json:"auto_refresh"`
	Schema         string                        `json:"schema,omitempty"`
	SchemaVersion  int                           `json:"schema_version,omitempty"`
	DeepSeekAPIKey string                        `json:"deepseek_api_key"`
	ChromeCDPURL   string                        `json:"chrome_cdp_url"`
	ChromeCDPURL2  string                        `json:"chrome_cdp_url_2"`
	Services       map[string]guiServiceDocument `json:"services"`
}

type guiServiceDocument struct {
	Enabled                    *bool `json:"enabled"`
	AutoRefreshIntervalSeconds *int  `json:"auto_refresh_interval_seconds"`
}

// GUISettingsPath returns the gui_settings.json path inside the user data
// directory, matching the location the retired C++ GUI used.
func GUISettingsPath() string {
	return filepath.Join(UserDataDirectory(), "gui_settings.json")
}

// LoadGUISettings reads gui_settings.json. A missing file is materialized
// from the embedded example (config/gui_settings.json.example) before
// parsing, making that example the single source of defaults; unreadable
// or malformed files fall back to the example's settings plus the error
// so callers can warn and continue.
func LoadGUISettings() (GUISettings, error) {
	settingsBytes, readErr := os.ReadFile(GUISettingsPath())
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			settings, _ := decodeGUISettings(config.GUISettingsExample)
			return settings, readErr
		}
		materializeErr := writeUserDataFile(GUISettingsPath(), config.GUISettingsExample)
		settings, _ := decodeGUISettings(config.GUISettingsExample)
		return settings, materializeErr
	}

	settings, decodeErr := decodeGUISettings(settingsBytes)
	if decodeErr != nil {
		fallback, _ := decodeGUISettings(config.GUISettingsExample)
		return fallback, decodeErr
	}
	return settings, nil
}

// decodeGUISettings decodes one gui_settings.json document; entries absent
// from the document keep the per-field fallbacks below.
func decodeGUISettings(documentBytes []byte) (GUISettings, error) {
	var document guiSettingsDocument
	if decodeErr := json.Unmarshal(documentBytes, &document); decodeErr != nil {
		return GUISettings{}, decodeErr
	}

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
	return settings, nil
}

// SaveGUISettings writes gui_settings.json atomically (temp file + rename),
// materializing every known service's resolved setting; service IDs outside
// ServiceOrder are dropped.
func SaveGUISettings(settings GUISettings) error {
	document := guiSettingsDocument{
		AutoRefresh:    settings.AutoRefresh,
		Schema:         guiSettingsSchema,
		SchemaVersion:  guiSettingsSchemaVersion,
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

	encoded, encodeErr := json.MarshalIndent(document, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	return writeUserDataFile(GUISettingsPath(), encoded)
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
