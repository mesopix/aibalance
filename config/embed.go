// Package config ships the example configuration prototype from config/
// inside the binary. The settings loader materializes it into the user data
// directory on first run, so the example doubles as the default values.
package config

import _ "embed"

// GUISettingsExample is the gui_settings.json prototype written to the
// user data directory when no settings file exists yet.
//
//go:embed gui_settings.json.example
var GUISettingsExample []byte
