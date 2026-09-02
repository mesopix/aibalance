package aibalance

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// LoadStartupSettings is the single startup entry: it loads config.json
// (materializing it from the embedded example on a fresh machine), folds
// any legacy .env.local into it once, and bridges the environment fields
// into the process environment. Corrupt config files are fatal; other
// errors are joined for the caller to warn about.
func LoadStartupSettings() (GUISettings, error) {
	settings, loadErr := LoadGUISettings()
	if loadErr != nil {
		return settings, loadErr
	}
	migrateErr := migrateEnvLocal(&settings)
	applyErr := applyGUISettingsEnvironment(settings)
	return settings, errors.Join(migrateErr, applyErr)
}

// migrateEnvLocal folds a legacy .env.local into settings once: known keys
// fill empty fields only (existing settings win), the merged settings are
// saved, and the file is deleted when every active key was consumed. The
// file is never re-created; unknown keys keep it in place with a warning.
func migrateEnvLocal(settings *GUISettings) error {
	envBytes, readErr := os.ReadFile(EnvLocalPath())
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil
		}
		return readErr
	}

	fieldPointers := map[string]*string{
		"DEEPSEEK_API_KEY": &settings.DeepSeekAPIKey,
		"CHROME_CDP_URL":   &settings.ChromeCDPURL,
		"CHROME_CDP_URL_2": &settings.ChromeCDPURL2,
	}
	unknownKeys := make([]string, 0)
	fieldChanged := false
	for key, value := range parseEnvLocal(envBytes) {
		fieldPointer, knownKey := fieldPointers[key]
		if !knownKey {
			unknownKeys = append(unknownKeys, key)
			continue
		}
		if *fieldPointer == "" && value != "" {
			*fieldPointer = value
			fieldChanged = true
		}
	}

	if fieldChanged {
		if saveErr := SaveGUISettings(*settings); saveErr != nil {
			return saveErr // keep .env.local so the migration retries next run
		}
	}

	if len(unknownKeys) > 0 {
		sort.Strings(unknownKeys)
		return fmt.Errorf(".env.local has keys gui_settings.json does not consume (%s); keeping the file",
			strings.Join(unknownKeys, ", "))
	}
	if removeErr := os.Remove(EnvLocalPath()); removeErr != nil {
		return removeErr // values are already merged; surface the leftover file
	}
	return nil
}

// applyGUISettingsEnvironment exports the settings' environment fields as
// process environment variables, never overriding variables already set in
// the real environment.
func applyGUISettingsEnvironment(settings GUISettings) error {
	environmentValues := map[string]string{
		"DEEPSEEK_API_KEY": settings.DeepSeekAPIKey,
		"CHROME_CDP_URL":   settings.ChromeCDPURL,
		"CHROME_CDP_URL_2": settings.ChromeCDPURL2,
	}
	var setErrors []error
	for variableName, value := range environmentValues {
		if value == "" || os.Getenv(variableName) != "" {
			continue
		}
		if setErr := os.Setenv(variableName, value); setErr != nil {
			setErrors = append(setErrors, setErr)
		}
	}
	return errors.Join(setErrors...)
}
