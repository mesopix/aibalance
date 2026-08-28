// render.go renders the TUI dashboard: a card grid of healthy services
// (each stamped with its refresh state), compact failure groups, and a
// status bar.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"aibalance/internal/aibalance"
)

// Status colors follow the dataviz status-palette rule: reserved semantic
// colors, always paired with the text label, never color alone.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	nameStyle     = lipgloss.NewStyle().Bold(true)
	barGoodStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	barWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	barPoorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	barEmptyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Card layout constants: a quota row reserves 49 inner cells for its
// label, percent and reset columns, so cards need ~63 inner cells (card
// width 67) to keep the usage bar readable; column counts are keyed to
// how many minimum-width cards plus gaps fit the terminal.
const (
	minCardColumnsWidth = 135 // two columns: 2*67 + 1 gap
	maxCardColumnsWidth = 203 // three columns: 3*67 + 2 gaps
	maxCardWidth        = 72  // single-column cards stay readable, not stretched
	minCardWidth        = 26  // below this the quota rows degrade but stay intact
)

// Quota-row layout constants: a quota row is a strict four-part line —
// label column, usage bar, percent, reset distance — identical on every
// card. The label and reset columns fit the longest supported values; a
// longer one would break the layout and panics at render time.
const (
	quotaLabelColumns   = 13 // "monthly tools", the longest supported label
	maxQuotaLabelWidth  = quotaLabelColumns
	quotaPercentColumns = 6  // "97.56%", the widest expected percent
	quotaResetColumns   = 25 // "(Resets in 29d 23h 59min)", the longest
)

// refreshingMark is the card's top-right marker while that service's
// refresh runs; one cell keeps it out of the service name's way.
const refreshingMark = "⟳"

// dashboardState is the render snapshot of the bubbletea model.
type dashboardState struct {
	views       []aibalance.ServiceView
	refreshedAt map[string]time.Time // per-service data age, keyed by service ID
	inFlight    map[string]bool      // services with a refresh running
	settings    aibalance.GUISettings
	lastRefresh string
	source      string // "cache" or "live"
	notice      string // transient hint, e.g. the login-page outcome
	err         error
	width       int // terminal width in cells; 0 means unknown
}

// refreshInterval returns the service's auto-refresh interval, or 0 when
// auto-refresh is off and the age counts toward no deadline.
func (state dashboardState) refreshInterval(serviceID string) time.Duration {
	if !state.settings.AutoRefresh {
		return 0
	}
	return state.settings.AutoRefreshInterval(serviceID)
}

// renderHeader renders the title plus refresh timestamp and data source.
func renderHeader(state dashboardState) string {
	header := titleStyle.Render("AI Credit Visualizer")
	if state.lastRefresh != "" {
		header += mutedStyle.Render(" · " + state.lastRefresh + " · " + state.source)
	}
	return header
}

// renderDashboardContent renders the scrollable body: card grid, then
// failure and login groups, separated by blank lines.
func renderDashboardContent(state dashboardState) string {
	failedViews, loginViews, okViews := partitionViews(state.views)

	var sections []string
	if len(okViews) > 0 {
		sections = append(sections, renderCardGrid(okViews, state))
	}
	if len(failedViews) > 0 {
		stampRefreshingDetail(failedViews, state.inFlight)
		sections = append(sections, renderCompactGroup("✗ 异常", errorStyle, failedViews))
	}
	if len(loginViews) > 0 {
		// The CLI's --login-setup hint is meaningless in the TUI; key l
		// opens the login pages instead.
		for viewIndex := range loginViews {
			if loginViews[viewIndex].Detail == "run --login-setup" {
				loginViews[viewIndex].Detail = "press l to login"
			}
		}
		stampRefreshingDetail(loginViews, state.inFlight)
		sections = append(sections, renderCompactGroup("⚠ 需要登录", warnStyle, loginViews))
	}
	if len(sections) == 0 {
		hint := "no data yet"
		if refreshing := refreshingNames(state.inFlight); len(refreshing) > 0 {
			hint = refreshingMark + " refreshing " + strings.Join(refreshing, ", ")
		} else if state.err != nil {
			hint = state.err.Error()
		}
		return mutedStyle.Render(hint)
	}
	return strings.Join(sections, "\n\n")
}

// refreshingNames lists the display names of in-flight services in
// canonical order, for the empty-dashboard refresh hint.
func refreshingNames(inFlight map[string]bool) []string {
	var names []string
	for _, serviceName := range aibalance.ServiceOrder {
		if inFlight[serviceName] {
			names = append(names, aibalance.ServiceDisplayName(serviceName))
		}
	}
	return names
}

// partitionViews splits views by status, keeping the canonical order.
func partitionViews(views []aibalance.ServiceView) (failed, login, ok []aibalance.ServiceView) {
	for _, view := range views {
		switch view.Status {
		case "OK":
			ok = append(ok, view)
		case "NEEDS_LOGIN":
			login = append(login, view)
		default: // ERROR, PARTIAL, SKIPPED
			failed = append(failed, view)
		}
	}
	return failed, login, ok
}

// stampRefreshingDetail replaces the detail of in-flight services with the
// refresh marker; compact-group services have no card to stamp.
func stampRefreshingDetail(views []aibalance.ServiceView, inFlight map[string]bool) {
	for viewIndex := range views {
		if inFlight[views[viewIndex].ServiceID] {
			views[viewIndex].Detail = refreshingMark + " refreshing"
		}
	}
}

// renderCardGrid lays healthy services out as a grid of bordered cards,
// filled row by row and left-aligned within each row.
func renderCardGrid(okViews []aibalance.ServiceView, state dashboardState) string {
	columns := cardColumns(state.width)
	cardWidth := cardWidth(state.width, columns)

	var rows []string
	for startIndex := 0; startIndex < len(okViews); startIndex += columns {
		endIndex := min(startIndex+columns, len(okViews))
		cards := make([]string, 0, endIndex-startIndex)
		for cardIndex, view := range okViews[startIndex:endIndex] {
			card := renderServiceCard(view, cardStamp(view.ServiceID, state), cardWidth)
			if cardIndex < columns-1 {
				card += " " // column gap
			}
			cards = append(cards, card)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return strings.Join(rows, "\n\n")
}

// cardColumns picks the grid column count from the terminal width.
func cardColumns(terminalWidth int) int {
	switch {
	case terminalWidth >= maxCardColumnsWidth:
		return 3
	case terminalWidth >= minCardColumnsWidth:
		return 2
	default:
		return 1
	}
}

// cardWidth sizes cards to split the terminal evenly across columns,
// clamped so single-column cards stay compact and narrow ones intact.
func cardWidth(terminalWidth, columns int) int {
	if terminalWidth <= 0 {
		return maxCardWidth
	}
	width := (terminalWidth - (columns - 1)) / columns
	width = min(width, maxCardWidth)
	return max(width, minCardWidth)
}

// renderServiceCard renders one healthy service as a bordered card:
// quota rows first, then a single-line facts summary. stamp is the
// pre-styled status shown at the right end of the top edge.
func renderServiceCard(view aibalance.ServiceView, stamp string, width int) string {
	innerWidth := width - 4

	var lines []string
	for quotaIndex, quota := range view.Quotas {
		if quotaIndex > 0 {
			lines = append(lines, "") // blank line between quota groups
		}
		lines = append(lines, renderCardQuota(quota, innerWidth)...)
	}
	if len(view.Facts) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "") // blank line before the facts summary
		}
		facts := strings.Join(view.Facts, " · ")
		lines = append(lines, mutedStyle.Render(truncateLine(facts, innerWidth)))
	}
	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("no quota data"))
	}
	return renderCard(view.Name, stamp, lines, width)
}

// cardStamp renders a card's top-right status: the grey refresh marker
// while that service's refresh runs, otherwise how long ago its data was
// refreshed; empty when the age is unknown.
func cardStamp(serviceID string, state dashboardState) string {
	if serviceID == "" {
		return ""
	}
	if state.inFlight[serviceID] {
		return mutedStyle.Render(refreshingMark)
	}
	refreshedTime, known := state.refreshedAt[serviceID]
	if !known || refreshedTime.IsZero() {
		return ""
	}
	elapsed := time.Since(refreshedTime)
	return ageStyle(elapsed, state.refreshInterval(serviceID)).Render(formatAge(elapsed))
}

// ageStyle colors the age by how much of the refresh interval it has eaten:
// green below 30%, yellow below 80%, red beyond. Without an interval the
// age counts toward no deadline and stays muted.
func ageStyle(elapsed, interval time.Duration) lipgloss.Style {
	if interval <= 0 {
		return mutedStyle
	}
	switch elapsedShare := float64(elapsed) / float64(interval); {
	case elapsedShare < 0.3:
		return okStyle
	case elapsedShare < 0.8:
		return warnStyle
	default:
		return errorStyle
	}
}

// formatAge compacts an elapsed duration: "now", "5m", "3h", "2d".
func formatAge(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}

// renderCardQuota renders one quota as a single four-part line: label,
// usage bar, percent, reset distance.
func renderCardQuota(quota aibalance.QuotaView, innerWidth int) []string {
	if len(quota.Label) > maxQuotaLabelWidth {
		panic(fmt.Sprintf("quota label %q exceeds maxQuotaLabelWidth %d",
			quota.Label, maxQuotaLabelWidth))
	}
	if quota.Detail != "" {
		detail := truncateLine(quota.Detail, innerWidth-quotaLabelColumns-2)
		return []string{fmt.Sprintf("%-*s  %s", quotaLabelColumns, quota.Label,
			mutedStyle.Render(detail))}
	}

	percent := barStyleFor(quota.PercentLeft).Render(quotaPercentText(quota) + "%")
	resetSuffix := mutedStyle.Render(formatResetSuffix(quota.Reset))

	label := fmt.Sprintf("%-*s", quotaLabelColumns, quota.Label)
	bar := renderBar(quota.PercentLeft, quotaBarWidth(innerWidth))

	// The percent right-aligns just left of the reset column, which
	// itself right-aligns to the card edge.
	usedWidth := quotaLabelColumns + 2 + lipgloss.Width(bar)
	percentStart := innerWidth - quotaResetColumns - 1 - lipgloss.Width(percent)
	line := label + "  " + bar +
		strings.Repeat(" ", max(percentStart-usedWidth, 1)) + percent
	usedWidth = percentStart + lipgloss.Width(percent)
	resetStart := innerWidth - lipgloss.Width(resetSuffix)
	line += strings.Repeat(" ", max(resetStart-usedWidth, 1)) + resetSuffix
	return []string{line}
}

// quotaBarWidth spans the space between the label and percent columns.
func quotaBarWidth(innerWidth int) int {
	reserved := quotaLabelColumns + 2 + 2 + quotaPercentColumns + 1 + quotaResetColumns
	return max(innerWidth-reserved, 6)
}

// quotaPercentText renders the percent number, "?" when unknown.
func quotaPercentText(quota aibalance.QuotaView) string {
	if quota.PercentLeft == nil {
		return "?"
	}
	return trimNumber(quota.PercentLeft)
}

// formatResetSuffix renders the reset segment of a quota row: the
// distance to the reset time, e.g. "(Resets in 6d 3h 16min)", "(Reset
// 2h ago)" for a passed reset, or the leading clause of unparseable
// text ("Unused, no reset yet" -> "(Unused)"); empty when unknown.
func formatResetSuffix(reset string) string {
	return formatResetSuffixAt(reset, time.Now())
}

// formatResetSuffixAt is formatResetSuffix against a fixed reference
// time, so tests get deterministic distances.
func formatResetSuffixAt(reset string, now time.Time) string {
	if reset == "" {
		return ""
	}
	parsed, isTime := aibalance.ParseCachedTimestamp(reset)
	if !isTime {
		if commaIndex := strings.Index(reset, ","); commaIndex > 0 {
			return "(" + reset[:commaIndex] + ")"
		}
		return "(" + reset + ")"
	}
	until := parsed.Sub(now)
	if until >= 0 {
		return "(Resets in " + formatLater(until) + ")"
	}
	return "(Reset " + formatLater(-until) + " ago)"
}

// formatLater compacts a duration: "16min", "3h 16min", "6d 3h 16min";
// zero segments drop out.
func formatLater(until time.Duration) string {
	totalMinutes := int(until.Minutes())
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60
	var segments []string
	if days > 0 {
		segments = append(segments, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		segments = append(segments, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(segments) == 0 {
		segments = append(segments, fmt.Sprintf("%dmin", minutes))
	}
	return strings.Join(segments, " ")
}

// renderCard wraps lines in a rounded border with the title embedded in
// the top edge and the optional pre-styled status stamp at its right end;
// every line is padded to the same total width.
func renderCard(title string, stamp string, lines []string, width int) string {
	innerWidth := width - 4
	if lipgloss.Width(title) > innerWidth-4 {
		title = truncateLine(title, innerWidth-4)
	}

	titleWidth := lipgloss.Width(title)
	stampChunk := ""
	if stamp != "" {
		stampChunk = " " + stamp + " "
	}
	// "╭─ title ──── 3m ─╮"; the stamp drops out when the edge gets too tight.
	dashCount := width - 5 - titleWidth
	edgeTail := mutedStyle.Render("╮")
	if stampChunk != "" {
		dashCount = width - 6 - titleWidth - lipgloss.Width(stampChunk)
		if dashCount < 1 {
			stampChunk = ""
			dashCount = width - 5 - titleWidth
		} else {
			edgeTail = stampChunk + mutedStyle.Render("─╮")
		}
	}
	top := mutedStyle.Render("╭─ ") + nameStyle.Render(title) +
		mutedStyle.Render(" "+strings.Repeat("─", max(dashCount, 0))) + edgeTail
	bottom := mutedStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")

	body := make([]string, 0, len(lines))
	for _, line := range lines {
		pad := max(innerWidth-lipgloss.Width(line), 0)
		body = append(body, mutedStyle.Render("│ ")+line+
			strings.Repeat(" ", pad)+mutedStyle.Render(" │"))
	}
	return top + "\n" + strings.Join(body, "\n") + "\n" + bottom
}

// renderCompactGroup renders a one-line-per-service group; failures and
// login-pending services have no quota rows.
func renderCompactGroup(title string, groupStyle lipgloss.Style, views []aibalance.ServiceView) string {
	nameWidth := 0
	for _, view := range views {
		if len(view.Name) > nameWidth {
			nameWidth = len(view.Name)
		}
	}

	lines := []string{groupStyle.Render(title)}
	for _, view := range views {
		detail := view.Detail
		if detail == "" {
			detail = "no data"
		}
		lines = append(lines, fmt.Sprintf("  %-*s  %s",
			nameWidth, view.Name, mutedStyle.Render(detail)))
	}
	return strings.Join(lines, "\n")
}

// renderStatusBar shows service counts on the left and the dashboard state
// with key hints on the right, spread across the terminal width. Refresh
// progress lives on the cards, not here.
func renderStatusBar(state dashboardState) string {
	failedViews, loginViews, okViews := partitionViews(state.views)

	var stats []string
	if len(okViews) > 0 {
		stats = append(stats, okStyle.Render(fmt.Sprintf("✓ %d ok", len(okViews))))
	}
	if len(loginViews) > 0 {
		stats = append(stats, warnStyle.Render(fmt.Sprintf("⚠ %d login", len(loginViews))))
	}
	if len(failedViews) > 0 {
		stats = append(stats, errorStyle.Render(fmt.Sprintf("✗ %d error", len(failedViews))))
	}
	left := strings.Join(stats, mutedStyle.Render(" · "))

	right := okStyle.Render("ready")
	if state.err != nil {
		right = errorStyle.Render(state.err.Error())
	}
	if state.notice != "" {
		right += "  " + warnStyle.Render(state.notice)
	}
	right += "  " + mutedStyle.Render("r refresh · l login · q quit · ↑↓ scroll")

	if state.width <= 0 {
		return left + "  " + right
	}
	gap := max(state.width-lipgloss.Width(left)-lipgloss.Width(right), 2)
	return left + strings.Repeat(" ", gap) + right
}

// renderBar renders the usage bar colored by remaining percent.
func renderBar(percentLeft any, barWidth int) string {
	percent, isNumber := toFloatValue(percentLeft)
	if !isNumber {
		return barEmptyStyle.Render(strings.Repeat("·", barWidth))
	}

	bounded := min(max(percent, 0), 100)
	filled := int((bounded/100)*float64(barWidth) + 0.5)

	return barStyleFor(percentLeft).Render(strings.Repeat("█", filled)) +
		barEmptyStyle.Render(strings.Repeat("░", barWidth-filled))
}

// barStyleFor picks the semantic color shared by the bar and percent text.
func barStyleFor(percentLeft any) lipgloss.Style {
	percent, isNumber := toFloatValue(percentLeft)
	if !isNumber {
		return barEmptyStyle
	}
	switch {
	case percent < 20:
		return barPoorStyle
	case percent < 50:
		return barWarnStyle
	default:
		return barGoodStyle
	}
}

// truncateLine clips a plain-text line to maxWidth cells, rune-safe.
func truncateLine(line string, maxWidth int) string {
	if maxWidth < 4 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= maxWidth {
		return line
	}
	return string(runes[:maxWidth-1]) + "…"
}

// toFloatValue extracts a float from a JSON-decoded number.
func toFloatValue(value any) (float64, bool) {
	switch typedValue := value.(type) {
	case float64:
		return typedValue, true
	case int:
		return float64(typedValue), true
	case int64:
		return float64(typedValue), true
	default:
		return 0, false
	}
}

// trimNumber renders a number without a trailing .0 on whole values.
func trimNumber(value any) string {
	switch typedValue := value.(type) {
	case float64:
		if typedValue == float64(int64(typedValue)) {
			return fmt.Sprint(int64(typedValue))
		}
		return fmt.Sprint(typedValue)
	default:
		return fmt.Sprint(value)
	}
}
