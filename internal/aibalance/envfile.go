package aibalance

import (
	"os"
	"path/filepath"
	"strings"
)

// UserDataDirectory returns the legacy user data directory for .env.local
// and latest_summary.json. New config files live under os.UserConfigDir via
// go-config-manager; this path is retained only for backward compatibility.
func UserDataDirectory() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, appName)
	}
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName)
	}
	if homeDirectory := os.Getenv("HOME"); homeDirectory != "" {
		return filepath.Join(homeDirectory, ".local", "share", appName)
	}
	return filepath.Join(os.TempDir(), appName)
}

// UserConfigDirectory returns the base directory used by go-config-manager
// for this app's config.json. It mirrors os.UserConfigDir but falls back
// to UserDataDirectory when the system call fails, keeping tests that
// redirect LOCALAPPDATA functional on Windows.
func UserConfigDirectory() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return UserDataDirectory()
	}
	return dir
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

