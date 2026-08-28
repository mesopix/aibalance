// Command aibalance is the single AICreditVisualizer entry point: with no
// arguments it runs the terminal dashboard (cached summary, refresh through
// the embedded scraper, quota bars); "aibalance cli" runs the Python-
// compatible scraping CLI that expects an already-running CDP Chrome;
// "aibalance config" interactively edits gui_settings.json.
package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"aibalance/internal/aibalance"
)

// refreshTimeout bounds one service's refresh. Navigation waits cap at 30s
// and response body fetches at 5s, so a healthy pass finishes well inside it
// and a wedged one surfaces as an error instead of a spinning card.
const refreshTimeout = 2 * time.Minute

// loginTimeout bounds opening login pages: Chrome start-up plus tab
// creation, no scraping.
const loginTimeout = time.Minute

// elapsedTickInterval re-renders the dashboard so card ages stay current.
const elapsedTickInterval = 30 * time.Second

// cacheStaleThreshold is how long a cached summary is shown without a
// refresh; it reuses the retired C++ GUI's 300s auto-refresh default.
const cacheStaleThreshold = 5 * time.Minute

// serviceRefreshDoneMsg carries one service's independent refresh outcome;
// output is that service's raw Run output, nil on err.
type serviceRefreshDoneMsg struct {
	service string
	output  map[string]any
	err     error
}

// autoRefreshTickMsg fires when the earliest per-service refresh deadline
// is reached; generation stamps out superseded ticks.
type autoRefreshTickMsg struct {
	generation int
	firedAt    time.Time
}

// elapsedTickMsg triggers a re-render so per-card ages tick up on screen.
type elapsedTickMsg struct{}

// loginDoneMsg carries the outcome of opening the login pages.
type loginDoneMsg struct {
	err error
}

// model is the bubbletea model for the dashboard.
type model struct {
	options         aibalance.RunOptions
	onceMode        bool // exit after the initial refreshes complete
	settings        aibalance.GUISettings
	enabledServices []string             // services allowed to refresh and display
	nextDue         map[string]time.Time // per-service next auto-refresh deadline
	lastRefreshAt   map[string]time.Time // per-service data age, keyed by service ID
	inFlight        map[string]bool      // services with a refresh command running
	tickGeneration  int                  // bumped on every armed tick
	summary         map[string]any
	views           []aibalance.ServiceView
	notice          string // transient status-bar hint, e.g. the login outcome
	err             error
	lastRefresh     string
	source          string // "cache" or "live"
	width           int    // terminal width in cells; 0 until the first resize
	viewport        viewport.Model
	saveSummary     func(map[string]any) error // cache-save seam; tests inject a stub
}

func newModel(options aibalance.RunOptions, settings aibalance.GUISettings, onceMode bool) *model {
	return &model{
		options:         options,
		onceMode:        onceMode,
		settings:        settings,
		enabledServices: settings.EnabledServices(),
		nextDue:         map[string]time.Time{},
		lastRefreshAt:   map[string]time.Time{},
		inFlight:        map[string]bool{},
		viewport:        viewport.New(0, 0),
		source:          "starting",
		saveSummary:     aibalance.SaveLatestSummary,
	}
}

// Init picks the startup path: --once always refreshes, otherwise the cache
// decides — a fresh cache is shown as-is, a missing or stale one refreshes.
func (m *model) Init() tea.Cmd {
	if m.onceMode {
		if len(m.enabledServices) == 0 {
			return tea.Quit
		}
		return tea.Batch(m.launchServiceRefreshes(m.enabledServices)...)
	}
	return tea.Batch(m.loadCache(), scheduleElapsedTick())
}

// scheduleElapsedTick arms the periodic re-render; --once exits after its
// single pass and never needs it.
func scheduleElapsedTick() tea.Cmd {
	return tea.Tick(elapsedTickInterval, func(time.Time) tea.Msg {
		return elapsedTickMsg{}
	})
}

// loadCache reads the cached summary; a nil summary means no usable cache.
func (m *model) loadCache() tea.Cmd {
	return func() tea.Msg {
		summary, err := aibalance.LoadLatestSummary()
		if err != nil || summary == nil {
			return cacheLoadedMsg{}
		}
		return cacheLoadedMsg{summary: summary}
	}
}

// cacheLoadedMsg carries the cached summary read at startup; a nil summary
// means no usable cache was found.
type cacheLoadedMsg struct {
	summary map[string]any
}

// startServiceRefresh runs one service's refresh in the background with its
// own context and timeout; the done message folds the result into the model.
// A runner panic becomes that service's error, not a dashboard crash.
func (m *model) startServiceRefresh(serviceName string) tea.Cmd {
	runOptions := m.options // value copy: the closure never touches the model
	return func() (msg tea.Msg) {
		defer func() {
			if recovered := recover(); recovered != nil {
				msg = serviceRefreshDoneMsg{service: serviceName,
					err: fmt.Errorf("runner panic: %v", recovered)}
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		if launchErr := ensureChromes(runOptions, []string{serviceName}); launchErr != nil {
			return serviceRefreshDoneMsg{service: serviceName, err: launchErr}
		}
		output, runErr := aibalance.Run(ctx, []string{serviceName}, runOptions, nil)
		if runErr != nil {
			return serviceRefreshDoneMsg{service: serviceName, err: runErr}
		}
		return serviceRefreshDoneMsg{service: serviceName, output: output}
	}
}

// launchServiceRefreshes marks the given services in-flight and returns one
// refresh command per service not already running; it must run on the Update
// loop so the marking is race-free.
func (m *model) launchServiceRefreshes(services []string) []tea.Cmd {
	var commands []tea.Cmd
	for _, serviceName := range services {
		if m.inFlight[serviceName] {
			continue
		}
		m.inFlight[serviceName] = true
		commands = append(commands, m.startServiceRefresh(serviceName))
	}
	if len(commands) > 0 {
		m.notice = "" // a fresh refresh supersedes any login hint
	}
	return commands
}

// ensureChromes starts or reuses only the automation Chromes the given
// services actually connect to; Chrome-free refreshes (e.g. DeepSeek-only)
// and disabled endpoints skip the launcher entirely.
func ensureChromes(options aibalance.RunOptions, services []string) error {
	needPrimary, needSecondary := aibalance.ChromeEndpointsForServices(services)

	if needPrimary {
		primaryPort, portErr := aibalance.ParseCDPPort(options.CDPURL)
		if portErr != nil {
			return fmt.Errorf("primary CDP URL: %w", portErr)
		}
		if launchErr := aibalance.EnsureCDPChromeReady(primaryPort,
			aibalance.ProfileDirectory("ai-balance-chrome")); launchErr != nil {
			return fmt.Errorf("primary automation Chrome: %w", launchErr)
		}
	}

	if needSecondary {
		secondaryPort, portErr := aibalance.ParseCDPPort(options.CDPURL2)
		if portErr != nil {
			return fmt.Errorf("secondary CDP URL: %w", portErr)
		}
		if launchErr := aibalance.EnsureCDPChromeReady(secondaryPort,
			aibalance.ProfileDirectory("ai-balance-chrome-2")); launchErr != nil {
			return fmt.Errorf("second account Chrome: %w", launchErr)
		}
	}
	return nil
}

// Update handles messages: keys, progress, refresh results, auto-refresh
// ticks.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMsg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typedMsg.Width
		m.viewport.Width = typedMsg.Width
		// Header, blank line, viewport, blank line, status bar.
		m.viewport.Height = max(typedMsg.Height-4, 3)
		return m, nil

	case tea.KeyMsg:
		switch typedMsg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			// Services already refreshing are skipped, not restarted.
			if len(m.enabledServices) > 0 {
				return m, tea.Batch(m.launchServiceRefreshes(m.enabledServices)...)
			}
		case "l":
			// handleLogin skips services whose refresh is navigating their
			// tab right now; the rest can open login pages immediately.
			return m, m.handleLogin()
		default:
			// j/k, arrows, page keys: scroll the dashboard body.
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(typedMsg)
			return m, cmd
		}

	case elapsedTickMsg:
		return m, scheduleElapsedTick()

	case loginDoneMsg:
		if typedMsg.err != nil {
			m.notice = "login: " + typedMsg.err.Error()
		} else {
			m.notice = "login pages opened — press r after login"
		}
		return m, nil

	case cacheLoadedMsg:
		if m.hasLiveSummary() {
			return m, nil
		}
		if typedMsg.summary != nil {
			// Disabled services never display, even from the cache.
			m.summary = aibalance.FilterSummaryServices(typedMsg.summary, m.enabledServices)
			m.views = aibalance.FormatSummaryViews(m.summary)
			m.source = "cache"
			m.lastRefresh = m.generatedAt()
			m.stampCacheRefreshedAt(typedMsg.summary)
			m.viewport.GotoTop()
		}
		if typedMsg.summary != nil && !cacheIsStale(typedMsg.summary) {
			// Fresh cache: show it and start each service's timer from now.
			m.armAutoRefreshFromNow()
			return m, m.scheduleNextAutoRefresh()
		}
		if len(m.enabledServices) == 0 {
			return m, nil
		}
		return m, tea.Batch(m.launchServiceRefreshes(m.enabledServices)...)

	case autoRefreshTickMsg:
		if typedMsg.generation != m.tickGeneration {
			return m, nil // superseded by a newer schedule
		}
		// Due-but-in-flight services are skipped here; their done handler
		// re-arms the scheduler with a fresh deadline.
		commands := m.launchServiceRefreshes(m.dueServices(typedMsg.firedAt))
		// Re-arm after the launches so the new tick skips them.
		return m, tea.Batch(append(commands, m.scheduleNextAutoRefresh())...)

	case serviceRefreshDoneMsg:
		return m.handleServiceRefreshDone(typedMsg)
	}
	return m, nil
}

// handleServiceRefreshDone folds one service's outcome into the model:
// merge, age stamp, and cache save on success; error surfacing on failure.
// Either way the deadline re-arms, then --once quits when nothing is left.
func (m *model) handleServiceRefreshDone(done serviceRefreshDoneMsg) (tea.Model, tea.Cmd) {
	delete(m.inFlight, done.service)
	m.rearmServiceSchedule(done.service)

	if done.err != nil {
		m.err = fmt.Errorf("%s: %w", aibalance.ServiceDisplayName(done.service), done.err)
	} else {
		m.err = nil
		refreshed := aibalance.SummarizeOutput(done.output)
		refreshedAccounts, _ := refreshed["accounts"].(map[string]any)
		serviceSummary, _ := refreshedAccounts[done.service].(map[string]any)
		if m.mergeServiceSummary(done.service, serviceSummary, refreshed["generated_at"]) {
			m.source = "live"
			m.lastRefresh = m.generatedAt()
			if saveErr := m.saveSummary(m.summary); saveErr != nil {
				m.err = saveErr
			}
			if len(m.inFlight) == 0 {
				m.viewport.GotoTop()
			}
		}
	}

	if m.onceMode && len(m.inFlight) == 0 {
		return m, tea.Quit
	}
	return m, m.scheduleNextAutoRefresh()
}

// mergeServiceSummary merges one service's summary into m.summary, stamps
// generatedAt when the refresh carries one, and rebuilds the views; it
// reports whether anything changed. The accounts map is rebuilt rather than
// mutated so any earlier snapshot of the summary stays immutable.
func (m *model) mergeServiceSummary(serviceName string, serviceSummary map[string]any, generatedAt any) bool {
	if serviceName == "" || serviceSummary == nil {
		return false
	}
	baseAccounts, _ := m.summary["accounts"].(map[string]any)
	mergedAccounts := make(map[string]any, len(baseAccounts)+1)
	maps.Copy(mergedAccounts, baseAccounts)
	mergedAccounts[serviceName] = serviceSummary
	if generatedAt == nil {
		generatedAt = m.summary["generated_at"]
	}
	m.summary = map[string]any{
		"generated_at": generatedAt,
		"accounts":     mergedAccounts,
	}
	m.views = aibalance.FormatSummaryViews(m.summary)
	m.lastRefreshAt[serviceName] = time.Now()
	return true
}

// stampCacheRefreshedAt seeds per-service refresh times from the cached
// summary's generated_at so card ages survive a restart.
func (m *model) stampCacheRefreshedAt(summary map[string]any) {
	generatedAt, parsed := aibalance.ParseCachedTimestamp(summary["generated_at"])
	if !parsed {
		return
	}
	accounts, _ := summary["accounts"].(map[string]any)
	for serviceName := range accounts {
		m.lastRefreshAt[serviceName] = generatedAt
	}
}

// handleLogin opens the login page of every service that needs it and is
// not refreshing right now.
func (m *model) handleLogin() tea.Cmd {
	loginServices := m.loginNeededServices()
	if len(loginServices) == 0 {
		m.notice = "no service needs login"
		return nil
	}
	var pendingServices []string
	for _, serviceName := range loginServices {
		if !m.inFlight[serviceName] {
			pendingServices = append(pendingServices, serviceName)
		}
	}
	if len(pendingServices) == 0 {
		m.notice = "login deferred: refresh in progress"
		return nil
	}
	m.notice = "opening login pages…"
	return m.openLoginPages(pendingServices)
}

// loginNeededServices returns the services that should get a login page:
// those showing NEEDS_LOGIN, plus enabled browser services with no view
// yet (fresh start before the first refresh settles — login state unknown).
func (m *model) loginNeededServices() []string {
	statusByID := make(map[string]string, len(m.views))
	for _, view := range m.views {
		statusByID[view.ServiceID] = view.Status
	}
	var loginServices []string
	for _, serviceName := range m.enabledServices {
		if aibalance.LoginTargetURL(serviceName) == "" {
			continue
		}
		if status, viewed := statusByID[serviceName]; viewed && status != "NEEDS_LOGIN" {
			continue
		}
		loginServices = append(loginServices, serviceName)
	}
	return loginServices
}

// openLoginPages ensures the services' automation Chromes are up, then
// opens one foreground login tab per service.
func (m *model) openLoginPages(services []string) tea.Cmd {
	options := m.options
	return func() tea.Msg {
		if launchErr := ensureChromes(options, services); launchErr != nil {
			return loginDoneMsg{err: launchErr}
		}
		ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
		defer cancel()
		return loginDoneMsg{err: aibalance.OpenLoginPages(ctx, services, options)}
	}
}

// armAutoRefreshFromNow schedules every enabled service one interval ahead
// from the current moment (the fresh-cache startup path).
func (m *model) armAutoRefreshFromNow() {
	if !m.settings.AutoRefresh {
		return
	}
	now := time.Now()
	for _, serviceName := range m.enabledServices {
		m.nextDue[serviceName] = now.Add(m.settings.AutoRefreshInterval(serviceName))
	}
}

// rearmServiceSchedule moves one service's deadline one interval ahead; a
// failed attempt counts too, so errors do not cause a retry storm.
func (m *model) rearmServiceSchedule(serviceName string) {
	if !m.settings.AutoRefresh {
		return
	}
	m.nextDue[serviceName] = time.Now().Add(m.settings.AutoRefreshInterval(serviceName))
}

// dueServices returns the enabled services whose deadline has passed.
func (m *model) dueServices(now time.Time) []string {
	var dueServices []string
	for _, serviceName := range m.enabledServices {
		deadline, scheduled := m.nextDue[serviceName]
		if scheduled && !now.Before(deadline) {
			dueServices = append(dueServices, serviceName)
		}
	}
	return dueServices
}

// scheduleNextAutoRefresh arms one tick at the earliest pending deadline;
// it returns nil when auto-refresh is off or nothing is scheduled.
func (m *model) scheduleNextAutoRefresh() tea.Cmd {
	if !m.settings.AutoRefresh || m.onceMode {
		return nil
	}
	var earliest time.Time
	for serviceName, deadline := range m.nextDue {
		// An in-flight service's stale deadline would fire immediately and
		// spin; its done handler re-arms it, so skip it here.
		if m.inFlight[serviceName] {
			continue
		}
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	if earliest.IsZero() {
		return nil
	}
	delay := max(time.Until(earliest), 0)
	m.tickGeneration++
	generation := m.tickGeneration
	return tea.Tick(delay, func(tickTime time.Time) tea.Msg {
		return autoRefreshTickMsg{generation: generation, firedAt: tickTime}
	})
}

// hasLiveSummary reports whether a live refresh already produced data.
func (m *model) hasLiveSummary() bool {
	return m.source == "live"
}

// cacheIsStale reports whether the cached summary predates the auto-refresh
// threshold; a missing or unparseable timestamp counts as stale.
func cacheIsStale(summary map[string]any) bool {
	generatedAt, parsed := aibalance.ParseCachedTimestamp(summary["generated_at"])
	if !parsed {
		return true
	}
	return time.Since(generatedAt) > cacheStaleThreshold
}

// generatedAt extracts the display timestamp from the current summary.
func (m *model) generatedAt() string {
	if m.summary == nil {
		return ""
	}
	if generatedAt, isString := m.summary["generated_at"].(string); isString {
		return generatedAt
	}
	return ""
}

// View renders the dashboard: fixed header, scrollable body, fixed status
// bar.
func (m *model) View() string {
	state := dashboardState{
		views:       m.views,
		refreshedAt: m.lastRefreshAt,
		inFlight:    m.inFlight,
		settings:    m.settings,
		lastRefresh: m.lastRefresh,
		source:      m.source,
		notice:      m.notice,
		err:         m.err,
		width:       m.width,
	}
	m.viewport.SetContent(renderDashboardContent(state))
	return renderHeader(state) + "\n\n" + m.viewport.View() + "\n\n" +
		renderStatusBar(state)
}

func main() {
	// The startup settings load runs for every entry point (TUI, cli,
	// config): it materializes gui_settings.json on a fresh machine, folds
	// a legacy .env.local into it once, and bridges the DeepSeek/CDP fields
	// into the environment for the flag defaults and scrapers below.
	settings, settingsErr := aibalance.LoadStartupSettings()
	if settingsErr != nil {
		fmt.Fprintf(os.Stderr, "load startup settings: %v\n", settingsErr)
	}

	// "aibalance cli ..." dispatches to the CLI mode; "aibalance config"
	// opens the settings editor; everything else is the TUI.
	if len(os.Args) > 1 && os.Args[1] == "cli" {
		runCLI(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "config" {
		runConfig(os.Args[2:])
		return
	}

	cdpURL := flag.String("cdp-url", firstNonEmpty(os.Getenv("CHROME_CDP_URL"), "http://127.0.0.1:9222"),
		"CDP endpoint of the primary automation Chrome")
	cdpURL2 := flag.String("cdp-url-2", firstNonEmpty(os.Getenv("CHROME_CDP_URL_2"), "http://127.0.0.1:9333"),
		"CDP endpoint of the second account Chrome (Z.ai #2, BigModel #2)")
	timeoutMS := flag.Int("timeout-ms", 30_000, "Navigation timeout per browser service in milliseconds")
	waitMS := flag.Int("wait-ms", 3_000, "Extra settle delay per browser service in milliseconds")
	onceMode := flag.Bool("once", false, "Refresh once and exit (for scripting and smoke tests)")
	flag.Usage = printMainUsage
	flag.Parse()

	options := aibalance.RunOptions{
		CDPURL:                 *cdpURL,
		CDPURL2:                *cdpURL2,
		TimeoutMS:              *timeoutMS,
		WaitMS:                 *waitMS,
		DeepSeekTimeoutSeconds: 20,
	}

	programOptions := []tea.ProgramOption{tea.WithAltScreen()}
	if *onceMode {
		// --once never reads keys; nil input skips the console reader whose
		// blocking read keeps Run() from returning after tea.Quit when stdin
		// is not a terminal (scripting and smoke tests).
		programOptions = append(programOptions, tea.WithInput(nil))
	}
	program := tea.NewProgram(newModel(options, settings, *onceMode), programOptions...)
	if _, runErr := program.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "run aibalance: %v\n", runErr)
		os.Exit(1)
	}
}

// printMainUsage documents the cli/config subcommands alongside the TUI
// flags; the default flag package usage knows nothing about the dispatch.
func printMainUsage() {
	output := flag.CommandLine.Output()
	fmt.Fprintf(output, "Usage of aibalance:\n")
	fmt.Fprintf(output, "  aibalance          Start the TUI dashboard (flags below)\n")
	fmt.Fprintf(output, "  aibalance cli      Scrape once and print JSON or human output ('cli -h' for flags)\n")
	fmt.Fprintf(output, "  aibalance config   Edit gui_settings.json ('config -h' for options)\n\n")
	fmt.Fprintf(output, "TUI flags:\n")
	flag.PrintDefaults()
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
