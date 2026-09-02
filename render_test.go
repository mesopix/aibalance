package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"aibalance/internal/aibalance"
)

// renderContentPlain renders the scrollable body with color sequences
// stripped so layout assertions see the plain text grid.
func renderContentPlain(state dashboardState) string {
	return ansi.Strip(renderDashboardContent(state))
}

func TestRenderContentOrder(t *testing.T) {
	views := []aibalance.ServiceView{
		{Name: "Kimi Coding Plan", Status: "OK", Quotas: []aibalance.QuotaView{
			{Label: "7d", Remaining: 67.0, Limit: 100.0, PercentLeft: 67.0,
				Reset: "2026-09-01 13:56 CST"},
		}},
		{Name: "ChatGPT Codex", Status: "NEEDS_LOGIN", Detail: "run --login-setup"},
		{Name: "Broken Service", Status: "ERROR", Detail: "boom"},
	}

	rendered := renderContentPlain(dashboardState{
		views:       views,
		lastRefresh: "2026-08-25 20:19 CST",
		source:      "cache",
	})

	cardIndex := strings.Index(rendered, "╭─ Kimi Coding Plan")
	failedIndex := strings.Index(rendered, "✗ 异常")
	loginIndex := strings.Index(rendered, "⚠ 需要登录")
	if cardIndex < 0 || failedIndex < 0 || loginIndex < 0 {
		t.Fatalf("missing card or group titles in:\n%s", rendered)
	}
	if !(cardIndex < failedIndex && failedIndex < loginIndex) {
		t.Errorf("section order failed -> card, failed, login; got %d, %d, %d",
			cardIndex, failedIndex, loginIndex)
	}

	// Login rows align service names and show the key-l hint.
	if !strings.Contains(rendered, "  ChatGPT Codex  press l to login") {
		t.Errorf("login row not aligned/padded in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "  Broken Service  boom") {
		t.Errorf("failed row not aligned/padded in:\n%s", rendered)
	}

	header := ansi.Strip(renderHeader(dashboardState{
		lastRefresh: "2026-08-25 20:19 CST",
		source:      "cache",
	}))
	if !strings.Contains(header, "AI Credit Visualizer · 2026-08-25 20:19 CST · cache") {
		t.Errorf("header missing timestamp/source: %q", header)
	}
}

func TestRenderCardQuotaColumnsAlign(t *testing.T) {
	// Use a future reset so the suffix is "(Resets in ...)" regardless of
	// when CI runs; the hardcoded past date drifted into "(Reset ... ago)".
	futureReset := time.Now().Add(5 * 24 * time.Hour).Format("2006-01-02 15:04 MST")
	views := []aibalance.ServiceView{
		{Name: "BigModel", Status: "OK", Quotas: []aibalance.QuotaView{
			{Label: "5h", Remaining: 2000.0, Limit: 2000.0, PercentLeft: 100.0,
				Reset: "Unused, no reset yet"},
			{Label: "monthly tools", Remaining: 2702.0, Limit: 10000.0, PercentLeft: 28.0,
				Reset: futureReset},
		}},
	}

	rendered := renderContentPlain(dashboardState{views: views, width: 120})
	// The card shows only the percent, never the remaining/limit amounts.
	if strings.Contains(rendered, "2000/2000") || strings.Contains(rendered, "2702/10k") {
		t.Errorf("quota amounts should not render in:\n%s", rendered)
	}
	percentLines := filterLines(rendered, func(line string) bool {
		return strings.Contains(line, "100%") || strings.Contains(line, "28%")
	})
	if len(percentLines) != 2 {
		t.Fatalf("want 2 percent lines, got %d in:\n%s", len(percentLines), rendered)
	}
	// The right-aligned percent must end at the same cell.
	percentEndFirst := strings.Index(percentLines[0], "100%") + len("100%")
	percentEndSecond := strings.Index(percentLines[1], "28%") + len("28%")
	if percentEndFirst != percentEndSecond {
		t.Errorf("percent columns misaligned in:\n%s", rendered)
	}

	barLines := filterLines(rendered, func(line string) bool {
		return strings.Contains(line, "█") || strings.Contains(line, "░")
	})
	if len(barLines) != 2 {
		t.Fatalf("want 2 bar lines, got %d in:\n%s", len(barLines), rendered)
	}
	// Bars start at the same cell and share one width.
	if strings.Index(barLines[0], "█") != strings.Index(barLines[1], "█") {
		t.Errorf("bar columns misaligned in:\n%s", rendered)
	}
	barWidthFirst := strings.Count(barLines[0], "█") + strings.Count(barLines[0], "░")
	barWidthSecond := strings.Count(barLines[1], "█") + strings.Count(barLines[1], "░")
	if barWidthFirst != barWidthSecond {
		t.Errorf("bar widths differ: %d vs %d in:\n%s", barWidthFirst, barWidthSecond, rendered)
	}

	// The reset distance joins the quota line as its fourth segment.
	if !strings.Contains(rendered, "(Resets in") {
		t.Errorf("reset distance missing from quota line in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "(Unused)") {
		t.Errorf("unparseable reset should keep leading clause in:\n%s", rendered)
	}
	// Reset segments right-align to the card edge on every quota row.
	for _, barLine := range barLines {
		if !strings.HasSuffix(barLine, ") │") {
			t.Errorf("reset segment not right-aligned in:\n%s", rendered)
		}
	}

	// Quota groups are separated by a blank line.
	blankLines := filterLines(rendered, func(line string) bool {
		return strings.HasPrefix(line, "│") && strings.Trim(line, " │") == ""
	})
	if len(blankLines) != 1 {
		t.Errorf("want 1 blank separator between quota groups, got %d in:\n%s",
			len(blankLines), rendered)
	}
}

// filterLines keeps the lines the predicate accepts.
func filterLines(rendered string, keep func(string) bool) []string {
	var kept []string
	for line := range strings.SplitSeq(rendered, "\n") {
		if keep(line) {
			kept = append(kept, line)
		}
	}
	return kept
}

func TestRenderCardUniformWidth(t *testing.T) {
	views := []aibalance.ServiceView{
		{Name: "BigModel", Status: "OK", Quotas: []aibalance.QuotaView{
			{Label: "5h", Remaining: 2000.0, Limit: 2000.0, PercentLeft: 100.0},
			{Label: "monthly tools", Remaining: 2702.0, Limit: 10000.0, PercentLeft: 28.0},
		}, Facts: []string{"tokens 1,234 | credits 5.6"}},
	}

	card := ansi.Strip(renderServiceCard(views[0], "", 72))
	lines := strings.Split(card, "\n")
	for index, line := range lines {
		if lipgloss.Width(line) != 72 {
			t.Errorf("card line %d width = %d, want 72:\n%s", index, lipgloss.Width(line), card)
		}
	}
	if !strings.Contains(card, "tokens 1,234 | credits 5.6") {
		t.Errorf("facts line missing in:\n%s", card)
	}
	// The facts summary follows a blank line after the last quota group.
	factsIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "tokens 1,234 | credits 5.6") {
			factsIndex = index
		}
	}
	if factsIndex < 2 || strings.Trim(lines[factsIndex-1], " │") != "" {
		t.Errorf("facts line should follow a blank separator in:\n%s", card)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "now"},
		{59 * time.Second, "now"},
		{time.Minute, "1m"},
		{5 * time.Minute, "5m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{3 * time.Hour, "3h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, testCase := range cases {
		if got := formatAge(testCase.elapsed); got != testCase.want {
			t.Errorf("formatAge(%v) = %q, want %q", testCase.elapsed, got, testCase.want)
		}
	}
}

func TestCardStamp(t *testing.T) {
	state := dashboardState{
		refreshedAt: map[string]time.Time{
			"kimi_coding_plan": time.Now().Add(-3 * time.Minute),
		},
		inFlight: map[string]bool{"qwen_token_plan": true},
		settings: aibalance.GUISettings{AutoRefresh: true},
	}

	if got := ansi.Strip(cardStamp("kimi_coding_plan", state)); got != "3m" {
		t.Errorf("cardStamp(known) = %q, want 3m", got)
	}
	// An in-flight service shows the marker instead of a stale age.
	if got := ansi.Strip(cardStamp("qwen_token_plan", state)); got != refreshingMark {
		t.Errorf("cardStamp(in flight) = %q, want %q", got, refreshingMark)
	}
	if got := cardStamp("unknown_service", state); got != "" {
		t.Errorf("cardStamp(unknown) = %q, want empty", got)
	}
	if got := cardStamp("", state); got != "" {
		t.Errorf("cardStamp(no id) = %q, want empty", got)
	}
	state.refreshedAt = map[string]time.Time{"kimi_coding_plan": {}}
	if got := cardStamp("kimi_coding_plan", state); got != "" {
		t.Errorf("cardStamp(zero stamp) = %q, want empty", got)
	}
}

func TestAgeStyleThresholds(t *testing.T) {
	interval := 300 * time.Second
	cases := []struct {
		elapsed time.Duration
		want    lipgloss.Style
	}{
		{0, okStyle},
		{89 * time.Second, okStyle},   // 29.7% of the interval
		{90 * time.Second, warnStyle}, // 30%
		{239 * time.Second, warnStyle},
		{240 * time.Second, errorStyle}, // 80%
		{10 * time.Minute, errorStyle},  // overdue
	}
	for _, testCase := range cases {
		got := ageStyle(testCase.elapsed, interval).GetForeground()
		if got != testCase.want.GetForeground() {
			t.Errorf("ageStyle(%v) color = %v, want %v",
				testCase.elapsed, got, testCase.want.GetForeground())
		}
	}
	// No refresh deadline means the age carries no urgency.
	if got := ageStyle(time.Hour, 0).GetForeground(); got != mutedStyle.GetForeground() {
		t.Errorf("ageStyle(no interval) color = %v, want muted", got)
	}
}

// With auto-refresh off the stamp must stay muted rather than fake urgency.
func TestCardStampWithoutAutoRefresh(t *testing.T) {
	state := dashboardState{
		refreshedAt: map[string]time.Time{"kimi_coding_plan": time.Now().Add(-9 * time.Hour)},
	}
	want := mutedStyle.Render("9h")
	if got := cardStamp("kimi_coding_plan", state); got != want {
		t.Errorf("cardStamp(auto-refresh off) = %q, want %q", got, want)
	}
}

func TestRenderCardAgeInBorder(t *testing.T) {
	view := aibalance.ServiceView{
		ServiceID: "kimi_coding_plan",
		Name:      "Kimi Coding",
		Status:    "OK",
		Quotas:    []aibalance.QuotaView{{Label: "5h", Remaining: 3.2, Limit: 4.0, PercentLeft: 78.0}},
	}
	// A colored stamp must not widen the top edge: only cells count.
	card := ansi.Strip(renderServiceCard(view, okStyle.Render("3m"), 72))
	lines := strings.Split(card, "\n")
	if !strings.Contains(lines[0], " 3m ─╮") {
		t.Errorf("top edge missing age stamp: %q", lines[0])
	}
	for index, line := range lines {
		if lipgloss.Width(line) != 72 {
			t.Errorf("card line %d width = %d, want 72:\n%s", index, lipgloss.Width(line), card)
		}
	}
}

func TestRenderContentMarksRefreshingServices(t *testing.T) {
	state := dashboardState{
		views: []aibalance.ServiceView{
			{ServiceID: "kimi_coding_plan", Name: "Kimi Coding", Status: "OK",
				Quotas: []aibalance.QuotaView{{Label: "5h", PercentLeft: 78.0}}},
			{ServiceID: "qoder_team_credit", Name: "Qoder", Status: "ERROR", Detail: "boom"},
		},
		refreshedAt: map[string]time.Time{"kimi_coding_plan": time.Now()},
		inFlight:    map[string]bool{"kimi_coding_plan": true, "qoder_team_credit": true},
		width:       120,
	}

	rendered := renderContentPlain(state)
	if !strings.Contains(rendered, " "+refreshingMark+" ─╮") {
		t.Errorf("card top edge missing refresh marker in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Qoder  "+refreshingMark+" refreshing") {
		t.Errorf("failed row missing refresh marker in:\n%s", rendered)
	}
}

func TestRenderCardGridColumns(t *testing.T) {
	views := make([]aibalance.ServiceView, 0, 5)
	for _, name := range []string{"A", "B", "C", "D", "E"} {
		views = append(views, aibalance.ServiceView{
			Name:   name,
			Status: "OK",
			Quotas: []aibalance.QuotaView{{Label: "5h", PercentLeft: 50.0}},
		})
	}

	cases := []struct {
		terminalWidth int
		wantCards     int
	}{
		{210, 3},
		{150, 2},
		{100, 1},
	}
	for _, testCase := range cases {
		rendered := renderContentPlain(dashboardState{views: views, width: testCase.terminalWidth})
		firstCardRow := filterLines(rendered, func(line string) bool {
			return strings.Contains(line, "╭─")
		})[0]
		cardCount := strings.Count(firstCardRow, "╭─")
		if cardCount != testCase.wantCards {
			t.Errorf("width %d: first row has %d cards, want %d in:\n%s",
				testCase.terminalWidth, cardCount, testCase.wantCards, rendered)
		}
	}
}

func TestCardSizing(t *testing.T) {
	columnCases := []struct {
		terminalWidth int
		wantColumns   int
	}{
		{0, 1},
		{50, 1},
		{134, 1},
		{135, 2},
		{202, 2},
		{203, 3},
		{300, 3},
	}
	for _, testCase := range columnCases {
		if got := cardColumns(testCase.terminalWidth); got != testCase.wantColumns {
			t.Errorf("cardColumns(%d) = %d, want %d",
				testCase.terminalWidth, got, testCase.wantColumns)
		}
	}

	widthCases := []struct {
		terminalWidth int
		columns       int
		wantWidth     int
	}{
		{120, 1, 72},
		{135, 2, 67},
		{150, 2, 72},
		{203, 3, 67},
		{300, 3, 72},
		{30, 1, 30},
		{0, 1, 72},
	}
	for _, testCase := range widthCases {
		if got := cardWidth(testCase.terminalWidth, testCase.columns); got != testCase.wantWidth {
			t.Errorf("cardWidth(%d, %d) = %d, want %d",
				testCase.terminalWidth, testCase.columns, got, testCase.wantWidth)
		}
	}
}

func TestFormatResetSuffix(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	// A whole-minute reference keeps the formatted distance exact.
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, shanghai)
	format := func(offset time.Duration) string {
		return now.Add(offset).Format("2006-01-02 15:04 CST")
	}
	future := formatResetSuffixAt(format(6*24*time.Hour+3*time.Hour+16*time.Minute), now)
	if future != "(Resets in 6d 3h 16min)" {
		t.Errorf("future reset = %q, want (Resets in 6d 3h 16min)", future)
	}
	passed := formatResetSuffixAt(format(-(2*time.Hour + 3*time.Minute)), now)
	if passed != "(Reset 2h 3min ago)" {
		t.Errorf("passed reset = %q, want (Reset 2h 3min ago)", passed)
	}
	if got := formatResetSuffix("Unused, no reset yet"); got != "(Unused)" {
		t.Errorf("unparseable reset = %q, want (Unused)", got)
	}
	if got := formatResetSuffix(""); got != "" {
		t.Errorf("empty reset = %q, want empty", got)
	}
}

func TestFormatLater(t *testing.T) {
	cases := []struct {
		until time.Duration
		want  string
	}{
		{30 * time.Second, "0min"},
		{16 * time.Minute, "16min"},
		{59 * time.Minute, "59min"},
		{time.Hour, "1h"},
		{3*time.Hour + 16*time.Minute, "3h 16min"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{36 * time.Hour, "1d 12h"},
		{6*24*time.Hour + 3*time.Hour, "6d 3h"},
		{6*24*time.Hour + 3*time.Hour + 16*time.Minute, "6d 3h 16min"},
	}
	for _, testCase := range cases {
		if got := formatLater(testCase.until); got != testCase.want {
			t.Errorf("formatLater(%v) = %q, want %q", testCase.until, got, testCase.want)
		}
	}
}

// A label wider than the layout cap must fail loudly instead of silently
// breaking the strict three-part quota row.
func TestRenderCardQuotaLabelCap(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Errorf("wide quota label should panic")
		}
	}()
	renderCardQuota(aibalance.QuotaView{
		Label:       "much too long label",
		PercentLeft: 50.0,
	}, 30)
}

func TestRenderBarFillsByPercent(t *testing.T) {
	if got := ansi.Strip(renderBar(nil, 8)); got != strings.Repeat("·", 8) {
		t.Errorf("renderBar(nil) = %q", got)
	}
	if got := ansi.Strip(renderBar(100.0, 8)); got != strings.Repeat("█", 8) {
		t.Errorf("renderBar(100) = %q", got)
	}
	if got := ansi.Strip(renderBar(0.0, 8)); got != strings.Repeat("░", 8) {
		t.Errorf("renderBar(0) = %q", got)
	}
	if got := ansi.Strip(renderBar(50.0, 8)); got != strings.Repeat("█", 4)+strings.Repeat("░", 4) {
		t.Errorf("renderBar(50) = %q", got)
	}
}

func TestRenderStatusBarStates(t *testing.T) {
	views := []aibalance.ServiceView{
		{Name: "Kimi", Status: "OK"},
		{Name: "Codex", Status: "NEEDS_LOGIN"},
		{Name: "Broken", Status: "ERROR"},
	}

	ready := ansi.Strip(renderStatusBar(dashboardState{views: views}))
	for _, fragment := range []string{"✓ 1 ok", "⚠ 1 login", "✗ 1 error", "ready", "r refresh · l login · q quit · ↑↓ scroll"} {
		if !strings.Contains(ready, fragment) {
			t.Errorf("ready status missing %q: %q", fragment, ready)
		}
	}

	notified := ansi.Strip(renderStatusBar(dashboardState{notice: "login pages opened — press r after login"}))
	if !strings.Contains(notified, "login pages opened — press r after login") {
		t.Errorf("notice missing from status bar: %q", notified)
	}

	failed := ansi.Strip(renderStatusBar(dashboardState{err: errExample{}}))
	if !strings.Contains(failed, "boom") {
		t.Errorf("error status missing message: %q", failed)
	}
}

// errExample is a fixed error for status-bar assertions.
type errExample struct{}

func (errExample) Error() string { return "boom" }

func TestRenderEmptyDashboardShowsRefreshHint(t *testing.T) {
	idle := renderContentPlain(dashboardState{})
	if !strings.Contains(idle, "no data yet") {
		t.Errorf("idle empty dashboard missing hint: %q", idle)
	}

	refreshing := renderContentPlain(dashboardState{
		inFlight: map[string]bool{"chatgpt_codex": true},
	})
	if !strings.Contains(refreshing, refreshingMark+" refreshing ChatGPT Codex") {
		t.Errorf("in-flight empty dashboard missing refresh hint: %q", refreshing)
	}
}
