//go:build !windows

package aibalance

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// chromeBundlePaths lists the fixed Chrome locations checked before PATH.
// They only resolve on macOS; on Linux every entry is simply absent.
var chromeBundlePaths = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
}

// chromeCommandNames lists the Chrome binaries resolved through PATH.
var chromeCommandNames = []string{
	"google-chrome-stable",
	"google-chrome",
	"chromium-browser",
	"chromium",
}

// FindChromeExecutable locates Chrome: the macOS application bundle first,
// then the usual binary names on PATH.
func FindChromeExecutable() (string, error) {
	for _, candidatePath := range chromeBundlePaths {
		if info, statErr := os.Stat(candidatePath); statErr == nil && !info.IsDir() {
			return candidatePath, nil
		}
	}
	for _, commandName := range chromeCommandNames {
		if resolvedPath, lookupErr := exec.LookPath(commandName); lookupErr == nil {
			return resolvedPath, nil
		}
	}
	return "", fmt.Errorf("Google Chrome was not found; install Chrome or add it to PATH")
}

// detachedProcessAttr puts Chrome in a new process group so the resident
// browser is not reaped when this process exits.
func detachedProcessAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
