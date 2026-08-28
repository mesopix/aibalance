package aibalance

import (
	"os"
	"path/filepath"
	"strings"
)

// UserDataDirectory mirrors user_data_directory in ai_balance.py:
// LOCALAPPDATA, then XDG_DATA_HOME, then HOME, then the temp directory.
func UserDataDirectory() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "AICreditVisualizer")
	}
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "AICreditVisualizer")
	}
	if homeDirectory := os.Getenv("HOME"); homeDirectory != "" {
		return filepath.Join(homeDirectory, ".local", "share", "AICreditVisualizer")
	}
	return filepath.Join(os.TempDir(), "AICreditVisualizer")
}

// EnvLocalPath returns the legacy .env.local path inside the user data
// directory; the startup migration folds it into gui_settings.json once.
func EnvLocalPath() string {
	return filepath.Join(UserDataDirectory(), ".env.local")
}

// writeUserDataFile atomically writes contents to filePath (temp file +
// rename) with owner-only permissions, creating parent directories as
// needed.
func writeUserDataFile(filePath string, contents []byte) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(filePath), 0o700); mkdirErr != nil {
		return mkdirErr
	}
	tempPath := filePath + ".tmp"
	if writeErr := os.WriteFile(tempPath, contents, 0o600); writeErr != nil {
		return writeErr
	}
	return os.Rename(tempPath, filePath)
}

// parseEnvLocal extracts the active KEY=VALUE assignments from .env.local
// contents: blank lines, '#' comments, and lines without '=' are skipped;
// surrounding whitespace and quotes are trimmed. It mirrors the lenient
// parsing of load_env_file in ai_balance.py.
func parseEnvLocal(contents []byte) map[string]string {
	assignments := make(map[string]string)
	for _, rawLine := range strings.Split(string(contents), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}

		equalsIndex := strings.Index(line, "=")
		key := strings.TrimSpace(line[:equalsIndex])
		value := strings.Trim(strings.TrimSpace(line[equalsIndex+1:]), `"'`)
		if key != "" {
			assignments[key] = value
		}
	}
	return assignments
}

