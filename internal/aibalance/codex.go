package aibalance

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// codexURLCandidates mirrors CODEX_URL_CANDIDATES in ai_balance.py.
var codexURLCandidates = []string{
	"https://chatgpt.com/codex/cloud/settings/analytics",
	"https://chatgpt.com/codex/usage",
	"https://chatgpt.com/codex/cloud/usage",
	"https://chatgpt.com/codex/cloud",
	"https://chatgpt.com/codex/settings/usage",
	"https://chatgpt.com/codex/settings",
	"https://chatgpt.com/codex",
	"https://chatgpt.com/",
}

// codexNote mirrors the note attached when no URL candidate yields usage.
const codexNote = "Official OpenAI help mentions a Codex usage page but does not publish a stable direct URL. " +
	"Pass --codex-url if the app exposes a different path in your account."

// codexProfileControlSelectors mirrors CODEX_PROFILE_CONTROL_SELECTORS.
// Playwright-only :has-text() selectors are dropped: Chrome's native
// querySelector rejects them, and the href selectors cover the same menu.
var codexProfileControlSelectors = []string{
	`[data-testid="accounts-profile-button"]`,
	`[data-testid="profile-button"]`,
	`button[data-testid*="profile" i]`,
	`[role="button"][data-testid*="profile" i]`,
	`button[aria-label*="profile menu" i]`,
	`button[aria-label*="account menu" i]`,
	`button[aria-label*="user menu" i]`,
	`button[aria-label*="个人资料"]`,
	`button[aria-label*="账户"]`,
	`button[aria-label*="帐户"]`,
}

// codexUsageControlSelectors mirrors CODEX_USAGE_CONTROL_SELECTORS.
var codexUsageControlSelectors = []string{
	`a[href*="/codex/cloud/settings/analytics"]`,
	`a[href*="/codex/settings/usage"]`,
	`a[href*="/codex/cloud/usage"]`,
	`a[href*="/codex/usage"]`,
}

// codexBankedResetTextPatterns mirrors CODEX_BANKED_RESET_TEXT_PATTERNS.
var codexBankedResetTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(\d{1,6})\s+(?:banked\s+)?(?:rate[-\s]?limit\s+)?resets?\s+available\b`),
	regexp.MustCompile(`(?i)\b(?:banked|available|remaining)\s+(?:rate[-\s]?limit\s+)?resets?\s*[:：]\s*(\d{1,6})\b`),
	regexp.MustCompile(`(?i)\b(?:banked\s+)?(?:rate[-\s]?limit\s+)?resets?\s+available\s*[:：]?\s*(\d{1,6})\b`),
	regexp.MustCompile(`(?:可用|剩余)(?:的)?(?:Codex\s*)?(?:额度|限额|速率限制)?重置(?:次数|机会)?\s*[:：]?\s*(\d{1,6})`),
	regexp.MustCompile(`(\d{1,6})\s*次(?:Codex\s*)?(?:额度|限额|速率限制)?重置(?:机会)?(?:可用|可使用|剩余)`),
}

// codexBankedResetDirectKeys mirrors CODEX_BANKED_RESET_DIRECT_KEYS.
var codexBankedResetDirectKeys = map[string]bool{
	"available_rate_limit_resets": true, "available_reset_count": true,
	"available_resets": true, "available_resets_count": true,
	"banked_reset_count": true, "banked_resets": true,
	"banked_resets_count": true, "codex_reset_count": true,
	"codex_resets_available": true, "rate_limit_reset_balance": true,
	"rate_limit_reset_count": true, "rate_limit_resets": true,
	"rate_limit_resets_available": true, "referral_resets": true,
	"remaining_reset_count": true, "remaining_resets": true,
	"reset_available_count": true, "reset_count_available": true,
	"resets_available": true, "reward_resets": true,
}

// codexBankedResetValueKeys mirrors CODEX_BANKED_RESET_VALUE_KEYS.
var codexBankedResetValueKeys = map[string]bool{
	"available": true, "available_count": true, "balance": true,
	"count": true, "remaining": true, "remaining_count": true,
	"total": true, "value": true,
}

// codexResetTimeKeyFragments mirrors CODEX_RESET_TIME_KEY_FRAGMENTS.
var codexResetTimeKeyFragments = []string{
	"last_reset", "next_reset", "reset_after", "reset_at", "reset_date",
	"reset_interval", "reset_period", "reset_time", "reset_timestamp",
	"reset_window", "seconds_to_reset", "seconds_until_reset",
}

// runCodexService mirrors probe_codex_dashboard: try each URL candidate
// until one yields a usage signal, then fall back to the best attempt.
func runCodexService(ctx context.Context, options RunOptions) map[string]any {
	if options.CDPURL == "" {
		return map[string]any{
			"status": "error",
			"error":  "no CDP URL configured (set CHROME_CDP_URL or --cdp-url)",
		}
	}

	browser, connectErr := connectCDP(ctx, options.CDPURL)
	if connectErr != nil {
		return map[string]any{
			"status": "error",
			"error":  browserErrorMessage(connectErr.Error()),
		}
	}
	page, acquireErr := acquireServicePage(browser, codexURLCandidates[0])
	if acquireErr != nil {
		return map[string]any{
			"status": "error",
			"error":  browserErrorMessage(acquireErr.Error()),
		}
	}

	var attempts []map[string]any
	for _, candidateURL := range codexURLCandidates {
		attempt := probeWebDashboard(ctx, page, candidateURL, options.TimeoutMS, options.WaitMS, nil, collectCodexProfileUsageText)
		attempts = append(attempts, attempt)
		if codexUsageSignal(attempt) {
			attempt["tried_urls"] = triedURLs(attempts)
			return attempt
		}
		if attempt["status"] == "needs_login" {
			// Every candidate sits behind the same session, so the remaining
			// URLs would only repeat the redirect to the login page.
			attempt["tried_urls"] = triedURLs(attempts)
			return attempt
		}
	}

	bestAttempt := map[string]any{}
	if len(attempts) > 0 {
		for attemptKey, attemptValue := range attempts[0] {
			bestAttempt[attemptKey] = attemptValue
		}
		if bestAttempt["status"] == "ok" {
			bestAttempt["status"] = "partial"
		}
	} else {
		bestAttempt["status"] = "error"
		bestAttempt["error"] = "no Codex URL candidates"
	}
	bestAttempt["tried_urls"] = triedURLs(attempts)
	bestAttempt["attempts"] = summarizeAttempts(attempts)
	bestAttempt["note"] = codexNote
	return bestAttempt
}

// triedURLs lists the requested URLs of every attempt so far.
func triedURLs(attempts []map[string]any) []string {
	urls := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		urls = append(urls, attempt["requested_url"].(string))
	}
	return urls
}

// summarizeAttempts mirrors summarize_attempt in ai_balance.py.
func summarizeAttempts(attempts []map[string]any) []map[string]any {
	summaries := make([]map[string]any, 0, len(attempts))
	for _, attempt := range attempts {
		summaries = append(summaries, map[string]any{
			"requested_url": attempt["requested_url"],
			"status":        attempt["status"],
			"current_url":   attempt["current_url"],
			"title":         attempt["title"],
			"login_hint":    attempt["login_hint"],
			"error":         attempt["error"],
		})
	}
	return summaries
}

// codexUsageSignal mirrors codex_usage_signal in ai_balance.py. The
// candidate_lines haystack is replaced by the full visible text, which
// contains the same lines plus context.
func codexUsageSignal(result map[string]any) bool {
	if result["status"] != "ok" {
		return false
	}
	if findBankedResetCount(result) != nil {
		return true
	}

	visibleText, _ := result["_visible_text"].(string)
	responseURLs := make([]string, 0, 8)
	responses, _ := result["json_responses"].([]CapturedJSONResponse)
	for _, response := range responses {
		responseURLs = append(responseURLs, response.URL)
	}
	haystack := strings.ToLower(visibleText + " " + strings.Join(responseURLs, " "))
	if !strings.Contains(haystack, "codex") {
		return false
	}

	concreteTerms := []string{"7d", "remaining", "reset", "resets", "quota", "limit reached", "used"}
	for _, term := range concreteTerms {
		if strings.Contains(haystack, term) {
			return true
		}
	}

	responseHaystack := strings.ToLower(strings.Join(responseURLs, " "))
	if strings.Contains(responseHaystack, "codex") {
		for _, term := range []string{"usage", "limit", "quota"} {
			if strings.Contains(responseHaystack, term) {
				return true
			}
		}
	}
	return false
}

// summarizeChatGPTCodex mirrors summarize_chatgpt_codex in ai_balance.py.
func summarizeChatGPTCodex(result map[string]any) map[string]any {
	summary := map[string]any{
		"status": result["status"],
	}
	if errorValue, hasError := result["error"]; hasError && errorValue != nil {
		summary["error"] = errorValue
		return summary
	}
	if result["status"] == "needs_login" {
		summary["reason"] = "needs_login"
		return summary
	}

	if bankedResets := findBankedResetCount(result); bankedResets != nil {
		summary["banked_resets_remaining"] = *bankedResets
	}
	visibleText, _ := result["_visible_text"].(string)

	if weeklyMatch := codexWeeklyPattern.FindStringSubmatch(visibleText); weeklyMatch != nil {
		weeklyRemaining := ToInt(weeklyMatch[1])
		var weeklyReset any
		if weeklyMatch[2] != "" {
			weeklyReset = parsePageResetTime(weeklyMatch[2])
		}
		if weeklyReset == nil && weeklyRemaining != nil && *weeklyRemaining == 100 {
			weeklyReset = resetUnusedText
		}
		summary["weekly"] = map[string]any{
			"remaining_percent": deref(weeklyRemaining),
			"reset":             weeklyReset,
		}
	}
	if creditsMatch := codexCreditsPattern.FindStringSubmatch(visibleText); creditsMatch != nil {
		summary["credits_remaining"] = deref(ToFloat(creditsMatch[1]))
	}
	if turnsMatch := codexTurnsPattern.FindStringSubmatch(visibleText); turnsMatch != nil {
		summary["turns"] = deref(ToInt(turnsMatch[1]))
	}

	if subscriptionPayload := findJSONResponse(result, "backend-api/subscriptions?account_id"); subscriptionPayload != nil {
		if activeUntil := FormatISODatetime(subscriptionPayload["active_until"]); activeUntil != nil {
			summary["subscription_valid_until"] = activeUntil
		}
		if willRenew, isBool := subscriptionPayload["will_renew"].(bool); isBool {
			summary["auto_renew"] = willRenew
		}
	}

	return summary
}

// codexWeeklyPattern matches "Weekly usage limit 42% remaining Resets ...".
var codexWeeklyPattern = regexp.MustCompile(
	`(?i)Weekly\s+usage\s+limit\s+(\d+)%\s+remaining(?:\s+Resets\s+([^\n]+))?`)

// codexCreditsPattern matches "Credits remaining 1,234.5".
var codexCreditsPattern = regexp.MustCompile(`(?i)Credits\s+remaining\s+([0-9,.]+)`)

// codexTurnsPattern matches "Turns 1,234".
var codexTurnsPattern = regexp.MustCompile(`(?i)Turns\s+([0-9,]+)`)

// codexResetDatePatterns mirror the strptime formats in parse_page_reset_time.
var codexResetDatePatterns = []string{
	"Jan 2, 2006 3:04 PM",
	"January 2, 2006 3:04 PM",
}

// codexResetClockPattern matches a bare "3:04 PM" time.
var codexResetClockPattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})\s*(AM|PM|am|pm)$`)

// parsePageResetTime mirrors parse_page_reset_time in formatting.py.
func parsePageResetTime(value string) any {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return nil
	}

	for _, layout := range codexResetDatePatterns {
		if parsed, parseErr := time.ParseInLocation(layout, cleaned, shanghaiZone); parseErr == nil {
			return parsed.In(shanghaiZone).Format(shanghaiLayout)
		}
	}

	if match := codexResetClockPattern.FindStringSubmatch(cleaned); match != nil {
		layout := "3:04 PM"
		if strings.ToLower(match[3]) == "am" || strings.ToLower(match[3]) == "pm" {
			if parsed, parseErr := time.ParseInLocation(layout, cleaned, shanghaiZone); parseErr == nil {
				return parsed.In(shanghaiZone).Format(shanghaiLayout)
			}
		}
	}
	return cleaned
}

// findBankedResetCount mirrors find_banked_reset_count in ai_balance.py.
func findBankedResetCount(result map[string]any) *int {
	responses, _ := result["json_responses"].([]CapturedJSONResponse)
	for responseIndex := len(responses) - 1; responseIndex >= 0; responseIndex-- {
		payload := responses[responseIndex].JSON
		if payload == nil {
			continue
		}
		if count := extractBankedResetCountFromPayload(payload); count != nil {
			return count
		}
	}

	for _, textKey := range []string{"_profile_usage_text", "_visible_text"} {
		textValue, isString := result[textKey].(string)
		if !isString {
			continue
		}
		if count := extractBankedResetCountFromText(textValue); count != nil {
			return count
		}
	}
	return nil
}

// extractBankedResetCountFromText mirrors extract_banked_reset_count_from_text.
func extractBankedResetCountFromText(value string) *int {
	for _, pattern := range codexBankedResetTextPatterns {
		match := pattern.FindStringSubmatch(value)
		if match == nil {
			continue
		}
		if count := nonnegativeCount(match[1]); count != nil {
			return count
		}
	}
	return nil
}

// extractBankedResetCountFromPayload mirrors
// extract_banked_reset_count_from_payload: BFS over the JSON tree scoring
// candidate paths by how strongly they look like banked-reset counters.
func extractBankedResetCountFromPayload(payload any) *int {
	type candidate struct {
		score int
		count int
	}
	var candidates []candidate

	type pendingNode struct {
		node any
		path []string
	}
	pending := []pendingNode{{node: payload}}
	inspectedNodes := 0

	for len(pending) > 0 && inspectedNodes < 10_000 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		inspectedNodes++
		if len(current.path) > 32 {
			continue
		}

		switch node := current.node.(type) {
		case string:
			if textCount := extractBankedResetCountFromText(node); textCount != nil {
				candidates = append(candidates, candidate{score: 115, count: *textCount})
			}
		case []any:
			for itemIndex := len(node) - 1; itemIndex >= 0; itemIndex-- {
				pending = append(pending, pendingNode{node: node[itemIndex], path: current.path})
			}
		case map[string]any:
			for key, childValue := range node {
				normalizedKey := normalizeJSONKey(key)
				childPath := append(append([]string(nil), current.path...), normalizedKey)
				if count := nonnegativeCount(childValue); count != nil {
					if score := bankedResetPathScore(childPath); score > 0 {
						candidates = append(candidates, candidate{score: score, count: *count})
					}
				}
				switch childValue.(type) {
				case map[string]any, []any, string:
					pending = append(pending, pendingNode{node: childValue, path: childPath})
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, item := range candidates[1:] {
		if item.score > best.score {
			best = item
		}
	}
	return &best.count
}

// bankedResetPathScore mirrors banked_reset_path_score in ai_balance.py.
func bankedResetPathScore(path []string) int {
	if len(path) == 0 {
		return 0
	}

	leafKey := path[len(path)-1]
	combinedPath := strings.Join(path, "_")
	for _, fragment := range codexResetTimeKeyFragments {
		if strings.Contains(combinedPath, fragment) {
			return 0
		}
	}
	if codexBankedResetDirectKeys[leafKey] || codexBankedResetDirectKeys[combinedPath] {
		return 120
	}
	if !strings.Contains(combinedPath, "reset") {
		return 0
	}

	contextMarkers := []string{"banked", "codex", "invite", "promotion", "rate_limit", "referral", "reward"}
	hasSpecificContext := false
	for _, marker := range contextMarkers {
		if strings.Contains(combinedPath, marker) {
			hasSpecificContext = true
			break
		}
	}
	if strings.Contains(combinedPath, "banked") {
		return 115
	}
	if strings.Contains(leafKey, "available") || strings.Contains(leafKey, "remaining") {
		return 110
	}
	if hasSpecificContext && (strings.Contains(leafKey, "count") || strings.Contains(leafKey, "balance")) {
		return 105
	}
	if hasSpecificContext && codexBankedResetValueKeys[leafKey] {
		return 100
	}
	return 0
}

// normalizeJSONKey mirrors normalize_json_key in utils.py.
func normalizeJSONKey(value string) string {
	snakeCase := camelCasePattern.ReplaceAllString(value, "${1}_${2}")
	lowered := strings.ToLower(snakeCase)
	normalized := nonWordPattern.ReplaceAllString(lowered, "_")
	return strings.Trim(normalized, "_")
}

var camelCasePattern = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var nonWordPattern = regexp.MustCompile(`[^a-z0-9]+`)

// nonnegativeCount mirrors nonnegative_count in utils.py.
func nonnegativeCount(value any) *int {
	switch typedValue := value.(type) {
	case bool:
		return nil
	case float64:
		if typedValue == float64(int64(typedValue)) && typedValue >= 0 && typedValue <= 999_999_999 {
			converted := int(typedValue)
			return &converted
		}
		return nil
	case int:
		if typedValue >= 0 && typedValue <= 999_999_999 {
			return &typedValue
		}
		return nil
	case string:
		normalized := strings.ReplaceAll(strings.TrimSpace(typedValue), ",", "")
		if !digitsOnlyPattern.MatchString(normalized) {
			return nil
		}
		if count := ToInt(normalized); count != nil && *count >= 0 && *count <= 999_999_999 {
			return count
		}
		return nil
	default:
		return nil
	}
}

var digitsOnlyPattern = regexp.MustCompile(`^\d{1,9}$`)

// collectCodexProfileUsageText mirrors collect_codex_profile_usage_text:
// open the profile menu, read it, optionally follow the usage link, then
// close the menu with Escape. Best-effort throughout.
func collectCodexProfileUsageText(page *rod.Page) string {
	if !clickFirstVisible(page, codexProfileControlSelectors) {
		return ""
	}

	var capturedTexts []string
	defer func() {
		_ = page.Keyboard.Press(input.Escape)
	}()

	time.Sleep(400 * time.Millisecond)
	menuText := readVisibleBodyText(page)
	if menuText != "" {
		capturedTexts = append(capturedTexts, menuText)
	}

	if extractBankedResetCountFromText(menuText) == nil && clickFirstVisible(page, codexUsageControlSelectors) {
		time.Sleep(500 * time.Millisecond)
		// Usage panels can keep background requests open; idle is best-effort.
		_ = page.WaitRequestIdle(3*time.Second, nil, nil, nil)
		usageText := readVisibleBodyText(page)
		if usageText != "" && !containsString(capturedTexts, usageText) {
			capturedTexts = append(capturedTexts, usageText)
		}
	}

	return strings.Join(capturedTexts, "\n")
}

// clickFirstVisible mirrors click_first_visible: try each selector, click
// the first visible match (up to 12 candidates per selector).
func clickFirstVisible(page *rod.Page, selectors []string) bool {
	for _, selector := range selectors {
		elements, elementsErr := page.Elements(selector)
		if elementsErr != nil {
			continue
		}
		candidateCount := min(len(elements), 12)
		for candidateIndex := 0; candidateIndex < candidateCount; candidateIndex++ {
			candidate := elements[candidateIndex]
			visible, visibleErr := candidate.Visible()
			if visibleErr != nil || !visible {
				continue
			}
			if clickErr := candidate.Click(proto.InputMouseButtonLeft, 1); clickErr == nil {
				return true
			}
		}
	}
	return false
}

// readVisibleBodyText mirrors read_visible_body_text: empty on failure.
func readVisibleBodyText(page *rod.Page) string {
	text, textErr := evalString(page, `() => document.body ? document.body.innerText : ""`)
	if textErr != nil {
		return ""
	}
	return text
}

// containsString reports whether the list holds the exact value.
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
