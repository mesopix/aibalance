package aibalance

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// chromeStartupTimeout bounds how long we wait for CDP after launching.
const chromeStartupTimeout = 20 * time.Second

// ProfileDirectory returns the automation Chrome profile path inside the
// user data directory (profiles live outside the repo so login state
// survives checkouts and clones).
func ProfileDirectory(profileName string) string {
	return filepath.Join(UserDataDirectory(), "profiles", profileName)
}

// FindChromeExecutable locates chrome.exe in the standard installation
// directories, mirroring findChromeExecutable in chrome_launcher.cpp.
func FindChromeExecutable() (string, error) {
	for _, environmentName := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		baseDirectory := os.Getenv(environmentName)
		if baseDirectory == "" {
			continue
		}
		candidate := filepath.Join(baseDirectory, "Google", "Chrome", "Application", "chrome.exe")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Google Chrome was not found in the standard installation directories")
}

// chromeEnsureMutex serializes readiness checks and launches: independent
// per-service refreshes may ensure the same Chrome concurrently, and the
// check-then-launch window must not admit a second Chrome on one profile.
var chromeEnsureMutex sync.Mutex

// EnsureCDPChromeReady makes sure a CDP-enabled Chrome listens on cdpPort,
// launching one with the given profile directory when absent. Mirrors
// ensureCdpChromeReady in chrome_launcher.cpp with the process-matching
// query simplified to a CDP endpoint probe.
func EnsureCDPChromeReady(cdpPort int, profileDirectory string) error {
	chromeEnsureMutex.Lock()
	defer chromeEnsureMutex.Unlock()

	if probeErr := probeCDPVersion(cdpPort); probeErr == nil {
		return nil // resident automation Chrome already serves CDP
	}

	chromeExecutable, findErr := FindChromeExecutable()
	if findErr != nil {
		return findErr
	}

	if launchErr := launchChromeDetached(chromeExecutable, cdpPort, profileDirectory); launchErr != nil {
		return fmt.Errorf("start Chrome: %w", launchErr)
	}

	deadline := time.Now().Add(chromeStartupTimeout)
	for time.Now().Before(deadline) {
		if probeErr := probeCDPVersion(cdpPort); probeErr == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("Chrome CDP did not become ready on port %d within %s",
		cdpPort, chromeStartupTimeout)
}

// probeCDPVersion checks that the CDP HTTP endpoint answers on the port.
func probeCDPVersion(cdpPort int) error {
	httpClient := &http.Client{Timeout: 2 * time.Second}
	response, requestErr := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", cdpPort))
	if requestErr != nil {
		return requestErr
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("CDP endpoint returned status %d", response.StatusCode)
	}
	return nil
}

// launchChromeDetached starts Chrome detached so it outlives this process,
// mirroring launchChromeDetached in chrome_launcher.cpp.
func launchChromeDetached(chromeExecutable string, cdpPort int, profileDirectory string) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(profileDirectory), 0o700); mkdirErr != nil {
		return fmt.Errorf("create profile parent directory: %w", mkdirErr)
	}

	command := exec.Command(chromeExecutable,
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port="+strconv.Itoa(cdpPort),
		"--user-data-dir="+profileDirectory,
		"--profile-directory=Default",
		"--no-first-run",
		"--no-default-browser-check",
	)
	// Detached: no job object, handles closed immediately, so the resident
	// Chrome survives this process exiting.
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if startErr := command.Start(); startErr != nil {
		return startErr
	}
	return command.Process.Release()
}

// ParseCDPPort extracts the port from a CDP endpoint URL.
func ParseCDPPort(cdpURL string) (int, error) {
	if cdpURL == "" {
		return 0, fmt.Errorf("empty CDP URL")
	}
	trimmedURL := strings.TrimPrefix(strings.TrimPrefix(cdpURL, "http://"), "https://")
	hostPart := trimmedURL
	if slashIndex := strings.IndexAny(hostPart, "/"); slashIndex >= 0 {
		hostPart = hostPart[:slashIndex]
	}
	if colonIndex := strings.LastIndex(hostPart, ":"); colonIndex >= 0 {
		port, parseErr := strconv.Atoi(hostPart[colonIndex+1:])
		if parseErr != nil {
			return 0, fmt.Errorf("invalid CDP URL %q: bad port", cdpURL)
		}
		return port, nil
	}
	return 0, fmt.Errorf("invalid CDP URL %q: no port", cdpURL)
}

// LocalPortOpen reports whether a TCP port is currently open, used to give
// a clearer error when something other than Chrome occupies the CDP port.
func LocalPortOpen(port int) bool {
	connection, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if dialErr != nil {
		return false
	}
	_ = connection.Close()
	return true
}
