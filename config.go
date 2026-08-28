package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"aibalance/internal/aibalance"
)

// runConfig implements the "aibalance config" subcommand: by default an
// interactive stdin menu editor for gui_settings.json (auto-refresh switch,
// service toggles, per-service refresh intervals); --edit opens the file in
// the user's editor instead.
func runConfig(args []string) {
	flags := flag.NewFlagSet("config", flag.ExitOnError)
	editMode := flags.Bool("edit", false, "Open gui_settings.json in your editor ($EDITOR, notepad by default).")
	flags.Parse(args)

	if *editMode {
		openSettingsInEditor()
		return
	}

	settings, loadErr := aibalance.LoadGUISettings()
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "load gui_settings.json (starting from defaults): %v\n", loadErr)
	}

	serviceCount := len(aibalance.ServiceOrder)
	enabled := make([]bool, serviceCount)
	intervals := make([]time.Duration, serviceCount)
	for serviceIndex, serviceName := range aibalance.ServiceOrder {
		enabled[serviceIndex] = settings.IsServiceEnabled(serviceName)
		intervals[serviceIndex] = settings.AutoRefreshInterval(serviceName)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("  a) auto_refresh  %s\n", onOffLabel(settings.AutoRefresh))
		for serviceIndex, serviceName := range aibalance.ServiceOrder {
			fmt.Printf("  %d) %-18s %-4s refresh %s\n", serviceIndex+1,
				aibalance.ServiceDisplayName(serviceName),
				onOffLabel(enabled[serviceIndex]),
				intervals[serviceIndex].Round(time.Second))
		}
		fmt.Println("  <n> toggle | <n> <seconds> set interval | a auto_refresh | s save | q quit")
		fmt.Print("> ")

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return // EOF or closed stdin: quit without saving
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "s":
			saveErr := aibalance.SaveGUISettings(resolveGUISettings(settings, enabled, intervals))
			if saveErr != nil {
				fmt.Fprintf(os.Stderr, "save gui_settings.json: %v\n", saveErr)
				os.Exit(1)
			}
			fmt.Printf("saved %s\n", aibalance.GUISettingsPath())
			return
		case "q":
			return
		case "a":
			settings.AutoRefresh = !settings.AutoRefresh
		default:
			serviceIndex, convErr := strconv.Atoi(fields[0])
			if convErr != nil || serviceIndex < 1 || serviceIndex > serviceCount {
				fmt.Println("  unrecognized command")
				continue
			}
			if len(fields) == 1 {
				enabled[serviceIndex-1] = !enabled[serviceIndex-1]
				continue
			}
			seconds, secondsErr := strconv.Atoi(fields[1])
			if secondsErr != nil || seconds <= 0 {
				fmt.Println("  interval must be a positive number of seconds")
				continue
			}
			intervals[serviceIndex-1] = time.Duration(seconds) * time.Second
		}
	}
}

// openSettingsInEditor launches the user's editor on gui_settings.json and
// warns when the edited file no longer parses.
func openSettingsInEditor() {
	editorFields := strings.Fields(firstNonEmpty(os.Getenv("EDITOR"), "notepad"))
	editorCommand := exec.Command(editorFields[0], append(editorFields[1:], aibalance.GUISettingsPath())...)
	editorCommand.Stdin = os.Stdin
	editorCommand.Stdout = os.Stdout
	editorCommand.Stderr = os.Stderr
	if runErr := editorCommand.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "run editor: %v\n", runErr)
		return
	}
	if _, loadErr := aibalance.LoadGUISettings(); loadErr != nil {
		fmt.Fprintf(os.Stderr, "load gui_settings.json after edit: %v\n", loadErr)
	}
}

// resolveGUISettings folds the editor's tracked state back into a
// GUISettings value covering every known service. The loaded base carries
// the environment fields (DeepSeek key, CDP endpoints) through untouched.
func resolveGUISettings(base aibalance.GUISettings, enabled []bool, intervals []time.Duration) aibalance.GUISettings {
	settings := aibalance.GUISettings{
		AutoRefresh:    base.AutoRefresh,
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
