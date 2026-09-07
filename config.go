package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	configmanager "github.com/mesopix/go-config-manager"

	"aibalance/internal/aibalance"
)

// runConfig implements the "aibalance config" subcommand: by default an
// interactive stdin menu editor for config.json (service toggles and
// per-service refresh intervals; auto-refresh itself is always on); --edit
// opens the file in the user's editor instead. Corrupt config files are
// fatal.
func runConfig(args []string) {
	flags := flag.NewFlagSet("config", flag.ExitOnError)
	editMode := flags.Bool("edit", false, "Open config.json in your editor ($EDITOR, notepad by default).")
	flags.Parse(args)

	if *editMode {
		openSettingsInEditor()
		return
	}

	settings, loadErr := aibalance.LoadGUISettings()
	if loadErr != nil {
		var corruptErr *configmanager.CorruptConfigError
		if errors.As(loadErr, &corruptErr) {
			fmt.Fprintf(os.Stderr, "config file %s is corrupt: %v\n", corruptErr.Path, corruptErr.Err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "load config.json: %v\n", loadErr)
		os.Exit(1)
	}

	if menuErr := runConfigMenu(bufio.NewReader(os.Stdin), settings, aibalance.SaveGUISettings); menuErr != nil {
		fmt.Fprintf(os.Stderr, "save config.json: %v\n", menuErr)
		os.Exit(1)
	}
}

// runConfigMenu drives the interactive config editor loop until the user
// saves or quits. Edits set unsavedChanges, so a lone q never discards them
// silently: it asks for a second q to confirm, and EOF reports the discard.
func runConfigMenu(reader *bufio.Reader, settings aibalance.GUISettings,
	saveSettings func(aibalance.GUISettings) error) error {
	serviceCount := len(aibalance.ServiceOrder)
	enabled := make([]bool, serviceCount)
	intervals := make([]time.Duration, serviceCount)
	for serviceIndex, serviceName := range aibalance.ServiceOrder {
		enabled[serviceIndex] = settings.IsServiceEnabled(serviceName)
		intervals[serviceIndex] = settings.AutoRefreshInterval(serviceName)
	}

	unsavedChanges := false
	quitArmed := false
	for {
		fmt.Println()
		for serviceIndex, serviceName := range aibalance.ServiceOrder {
			fmt.Printf("  %d) %-18s %-4s refresh %s\n", serviceIndex+1,
				aibalance.ServiceDisplayName(serviceName),
				onOffLabel(enabled[serviceIndex]),
				intervals[serviceIndex].Round(time.Second))
		}
		if unsavedChanges {
			fmt.Println("  * unsaved changes — s saves, q discards")
		}
		fmt.Println("  <n> toggle | <n> <seconds> set interval | s save | q quit")
		fmt.Print("> ")

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			// EOF or closed stdin: quit without saving, but not silently.
			if unsavedChanges {
				fmt.Fprintln(os.Stderr, "config.json not saved: unsaved changes discarded")
			}
			return nil
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "s":
			saveErr := saveSettings(resolveGUISettings(settings, enabled, intervals))
			if saveErr != nil {
				return saveErr
			}
			fmt.Printf("saved %s\n", aibalance.GUISettingsPath())
			return nil
		case "q":
			if !unsavedChanges || quitArmed {
				return nil
			}
			quitArmed = true
			fmt.Println("  unsaved changes — q again to discard, or s to save")
		default:
			serviceIndex, convErr := strconv.Atoi(fields[0])
			if convErr != nil || serviceIndex < 1 || serviceIndex > serviceCount {
				fmt.Println("  unrecognized command")
				continue
			}
			if len(fields) == 1 {
				enabled[serviceIndex-1] = !enabled[serviceIndex-1]
				unsavedChanges = true
				quitArmed = false
				continue
			}
			seconds, secondsErr := strconv.Atoi(fields[1])
			if secondsErr != nil || seconds <= 0 {
				fmt.Println("  interval must be a positive number of seconds")
				continue
			}
			intervals[serviceIndex-1] = time.Duration(seconds) * time.Second
			unsavedChanges = true
			quitArmed = false
		}
	}
}

// openSettingsInEditor launches the user's editor on config.json and
// exits fatally when the edited file no longer parses.
func openSettingsInEditor() {
	editorFields := strings.Fields(firstNonEmpty(os.Getenv("EDITOR"), "notepad"))
	editorCommand := exec.Command(editorFields[0], append(editorFields[1:], aibalance.GUISettingsPath())...)
	editorCommand.Stdin = os.Stdin
	editorCommand.Stdout = os.Stdout
	editorCommand.Stderr = os.Stderr
	if runErr := editorCommand.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "run editor: %v\n", runErr)
		os.Exit(1)
	}
	if _, loadErr := aibalance.LoadGUISettings(); loadErr != nil {
		var corruptErr *configmanager.CorruptConfigError
		if errors.As(loadErr, &corruptErr) {
			fmt.Fprintf(os.Stderr, "config file %s is corrupt after edit: %v\n", corruptErr.Path, corruptErr.Err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "load config.json after edit: %v\n", loadErr)
		os.Exit(1)
	}
}

// resolveGUISettings folds the editor's tracked state back into a
// GUISettings value covering every known service. The loaded base carries
// the environment fields (DeepSeek key, CDP endpoints) through untouched.
func resolveGUISettings(base aibalance.GUISettings, enabled []bool, intervals []time.Duration) aibalance.GUISettings {
	settings := aibalance.GUISettings{
		DeepSeekAPIKey: base.DeepSeekAPIKey,
		ChromeCDPURL:   base.ChromeCDPURL,
		ChromeCDPURL2:  base.ChromeCDPURL2,
		Services:       make(map[string]aibalance.ServiceSetting, len(aibalance.ServiceOrder)),
	}
	for serviceIndex, serviceName := range aibalance.ServiceOrder {
		settings.Services[serviceName] = aibalance.ServiceSetting{
			Enabled:             enabled[serviceIndex],
			AutoRefreshInterval: intervals[serviceIndex],
		}
	}
	return settings
}

// onOffLabel renders a boolean as on/off for the config display.
func onOffLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
