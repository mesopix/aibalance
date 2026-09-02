//go:build windows

package aibalance

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// chromeInstallEnvironments lists the variables whose Chrome install
// location is checked, in descending order of preference.
var chromeInstallEnvironments = []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"}

// FindChromeExecutable locates chrome.exe in the standard installation
// directories, mirroring findChromeExecutable in chrome_launcher.cpp.
func FindChromeExecutable() (string, error) {
	for _, environmentName := range chromeInstallEnvironments {
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

// detachedProcessAttr starts Chrome in its own process group with no job
// object, so the resident browser survives this process exiting.
func detachedProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
