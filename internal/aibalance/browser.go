package aibalance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// probeTimeoutDefaults mirror the Python CLI defaults.
const (
	defaultTimeoutMS = 30_000
	defaultWaitMS    = 3_000
	networkIdleLimit = 10 * time.Second
	evalTimeout      = 30 * time.Second
	// responsePollInterval is how often the required-response wait rechecks
	// the collector.
	responsePollInterval = 100 * time.Millisecond
	// captureQuietWindow is how long captures must stop arriving before the
	// wait gives up on a required response the dashboard never requests.
	captureQuietWindow = 2 * time.Second
)

// assertLoopbackCDPURL refuses non-loopback CDP endpoints, mirroring
// assert_loopback_cdp_url (CDP grants full browser control).
func assertLoopbackCDPURL(rawURL string) error {
	parsedURL, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return fmt.Errorf("invalid CDP URL: %w", parseErr)
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hostname != "127.0.0.1" && hostname != "localhost" && hostname != "::1" {
		return fmt.Errorf("refusing non-loopback CDP endpoint: %s", parsedURL.Host)
	}
	return nil
}

// resolveWebSocketURL fetches webSocketDebuggerUrl from the CDP endpoint.
func resolveWebSocketURL(ctx context.Context, cdpURL string) (string, error) {
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, cdpURL+"/json/version", nil)
	if requestErr != nil {
		return "", fmt.Errorf("build version request: %w", requestErr)
	}
	response, responseErr := http.DefaultClient.Do(request)
	if responseErr != nil {
		return "", fmt.Errorf("query CDP version endpoint: %w", responseErr)
	}
	defer response.Body.Close()

	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&versionInfo); decodeErr != nil {
		return "", fmt.Errorf("decode version payload: %w", decodeErr)
	}
	if versionInfo.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("CDP endpoint returned no webSocketDebuggerUrl")
	}
	return versionInfo.WebSocketDebuggerURL, nil
}

// connectCDP attaches to an already-running automation Chrome. The caller
// must NOT call browser.Close(): rod's Close terminates the whole Chrome
// process, unlike Playwright's stop(); disconnecting happens when the
// rod.Browser context is cancelled or the process exits.
func connectCDP(ctx context.Context, cdpURL string) (*rod.Browser, error) {
	if cdpURL == "" {
		return nil, fmt.Errorf("no CDP URL configured (set CHROME_CDP_URL or --cdp-url)")
	}
	if loopbackErr := assertLoopbackCDPURL(cdpURL); loopbackErr != nil {
		return nil, loopbackErr
	}
	wsURL, resolveErr := resolveWebSocketURL(ctx, cdpURL)
	if resolveErr != nil {
		return nil, resolveErr
	}
	browser := rod.New().Context(ctx).ControlURL(wsURL)
	if connectErr := browser.Connect(); connectErr != nil {
		return nil, fmt.Errorf("connect CDP browser: %w", connectErr)
	}
	return browser, nil
}

// makeWebDashboardRunner builds the standard runner for services that
// scrape one dashboard page over CDP. cdpSelector picks which automation
// Chrome to drive (primary or the second z.ai account). requiredResponses
// are the API URL fragments the summarizer reads; the probe stops waiting
// once all of them have been captured.
func makeWebDashboardRunner(targetURL string, requiredResponses []string, cdpSelector func(options RunOptions) string) ServiceRunner {
	return func(ctx context.Context, options RunOptions) map[string]any {
		cdpURL := cdpSelector(options)
		if cdpURL == "" {
			return map[string]any{
				"status": "error",
				"error":  "no CDP URL configured (set CHROME_CDP_URL or --cdp-url)",
			}
		}

		browser, connectErr := connectCDP(ctx, cdpURL)
		if connectErr != nil {
			return map[string]any{
				"status": "error",
				"error":  browserErrorMessage(connectErr.Error()),
			}
		}

		page, acquireErr := acquireServicePage(browser, targetURL)
		if acquireErr != nil {
			return map[string]any{
				"status": "error",
				"error":  browserErrorMessage(acquireErr.Error()),
			}
		}

		return probeWebDashboard(ctx, page, targetURL, options.TimeoutMS, options.WaitMS, requiredResponses, nil)
	}
}

// OpenLoginPages opens one foreground tab per service on its automation
// Chrome, navigated to the service's dashboard page — the site's login flow
// takes over from there. Tabs stay open for interactive login; the caller
// refreshes afterwards to pick up the new session. Disconnecting when ctx
// ends does not close Chrome or the tabs.
func OpenLoginPages(ctx context.Context, services []string, options RunOptions) error {
	type loginTarget struct {
		cdpURL    string
		targetURL string
	}
	var targets []loginTarget
	opened := map[string]bool{}
	for _, serviceName := range services {
		definition, implemented := serviceRegistry[serviceName]
		if !implemented || definition.TargetURL == "" {
			continue
		}
		cdpURL := options.CDPURL
		if definition.BrowserEndpoint == BrowserEndpointSecondary {
			cdpURL = options.CDPURL2
		}
		if cdpURL == "" {
			return fmt.Errorf("service %q: no CDP URL configured", serviceName)
		}
		dedupeKey := cdpURL + " " + definition.TargetURL
		if opened[dedupeKey] {
			continue
		}
		opened[dedupeKey] = true
		targets = append(targets, loginTarget{cdpURL: cdpURL, targetURL: definition.TargetURL})
	}

	// Group by endpoint in first-seen order so tabs open deterministically.
	var endpointOrder []string
	targetURLsByEndpoint := map[string][]string{}
	for _, target := range targets {
		if _, known := targetURLsByEndpoint[target.cdpURL]; !known {
			endpointOrder = append(endpointOrder, target.cdpURL)
		}
		targetURLsByEndpoint[target.cdpURL] = append(targetURLsByEndpoint[target.cdpURL], target.targetURL)
	}

	var openErrs []error
	for _, cdpURL := range endpointOrder {
		browser, connectErr := connectCDP(ctx, cdpURL)
		if connectErr != nil {
			openErrs = append(openErrs, fmt.Errorf("%s: %w", cdpURL, connectErr))
			continue
		}
		for _, targetURL := range targetURLsByEndpoint[cdpURL] {
			// Background stays false so the tab is created active and the
			// login page is immediately visible to the user.
			if _, createErr := browser.Page(proto.TargetCreateTarget{URL: targetURL}); createErr != nil {
				openErrs = append(openErrs, fmt.Errorf("open %s: %w", targetURL, createErr))
			}
		}
	}
	return errors.Join(openErrs...)
}

// isSpecialPageURL reports whether a tab URL belongs to browser-internal
// pages that cannot be navigated like ordinary web pages.
func isSpecialPageURL(rawURL string) bool {
	loweredURL := strings.ToLower(rawURL)
	return strings.HasPrefix(loweredURL, "chrome://") ||
		strings.HasPrefix(loweredURL, "devtools://") ||
		strings.HasPrefix(loweredURL, "edge://")
}

// urlOrigin extracts the scheme://host[:port] prefix for web URLs, or ""
// for everything else (about:blank, browser-internal pages).
func urlOrigin(rawURL string) string {
	parsedURL, parseErr := url.Parse(rawURL)
	if parseErr != nil ||
		(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
		parsedURL.Host == "" {
		return ""
	}
	return parsedURL.Scheme + "://" + parsedURL.Host
}

// sameDocumentTarget reports whether currentURL already points at
// targetURL's document (equal scheme, host, and path, ignoring query and
// hash). Navigating between such URLs may restore from cache or only
// scroll without reloading, so the caller reloads with cache disabled.
func sameDocumentTarget(currentURL string, targetURL string) bool {
	parsedCurrent, currentErr := url.Parse(currentURL)
	parsedTarget, targetErr := url.Parse(targetURL)
	if currentErr != nil || targetErr != nil {
		return false
	}
	return parsedCurrent.Scheme == parsedTarget.Scheme &&
		parsedCurrent.Host == parsedTarget.Host &&
		parsedCurrent.Path == parsedTarget.Path
}

// pageClaimMutex serializes tab claims: parallel runners probing the same
// Chrome could both miss an origin tab and create duplicates. Claims are
// fast, so one package-wide lock spanning both Chromes suffices.
var pageClaimMutex sync.Mutex

// pageClaimTimeout bounds the CDP calls made while holding pageClaimMutex, so
// an unresponsive Chrome cannot stall every other service's claim.
const pageClaimTimeout = 15 * time.Second

// acquireServicePage returns the persistent tab dedicated to the service's
// dashboard origin, creating one (left open for later passes) when no tab
// matches.
func acquireServicePage(browser *rod.Browser, targetURL string) (*rod.Page, error) {
	claimedTarget, claimErr := claimServiceTarget(browser, targetURL)
	if claimErr != nil {
		return nil, claimErr
	}

	// Attaching runs outside the claim lock: rod attaches a session and
	// re-emulates the tab, which blocks while that renderer is busy.
	claimedPage, attachErr := browser.PageFromTarget(claimedTarget)
	if attachErr != nil {
		return nil, fmt.Errorf("attach page: %w", attachErr)
	}
	return claimedPage, nil
}

// claimServiceTarget returns the tab claimed for the service, creating one
// when no tab matches. Tabs are claimed by origin: SPAs rewrite path and
// query after load, and each automation Chrome hosts at most one service per
// origin. Target infos already carry the tab URL, so the scan attaches to
// nothing — rod's Pages() attaches to and re-emulates every tab in the
// Chrome, which blocks on tabs other services are navigating right now and
// rewrites their viewport and user agent.
func claimServiceTarget(browser *rod.Browser, targetURL string) (proto.TargetTargetID, error) {
	pageClaimMutex.Lock()
	defer pageClaimMutex.Unlock()

	boundedBrowser := browser.Timeout(pageClaimTimeout)
	targets, listErr := proto.TargetGetTargets{}.Call(boundedBrowser)
	if listErr != nil {
		return "", fmt.Errorf("list targets: %w", listErr)
	}

	targetOrigin := urlOrigin(targetURL)
	for _, targetInfo := range targets.TargetInfos {
		if targetInfo.Type != proto.TargetTargetInfoTypePage || isSpecialPageURL(targetInfo.URL) {
			continue
		}
		if urlOrigin(targetInfo.URL) == targetOrigin {
			return targetInfo.TargetID, nil
		}
	}

	// Background: true opens the tab without activating it, so refreshes
	// never bring the automation Chrome window to the foreground.
	createdTarget, createErr := proto.TargetCreateTarget{
		URL:        targetURL,
		Background: true,
	}.Call(boundedBrowser)
	if createErr != nil {
		return "", fmt.Errorf("create target: %w", createErr)
	}
	return createdTarget.TargetID, nil
}

// evalString evaluates a JavaScript expression returning a string. The
// evaluation is timeout-bounded: a frozen renderer (e.g. behind a modal
// dialog) would otherwise block until the pass context expires.
func evalString(page *rod.Page, expression string) (string, error) {
	result, evalErr := page.Timeout(evalTimeout).Eval(expression)
	if evalErr != nil {
		return "", fmt.Errorf("eval %q: %w", expression, evalErr)
	}
	if result == nil {
		return "", nil
	}
	return result.Value.Str(), nil
}

// waitForDashboardData waits for the service's API data and reports whether
// every required response arrived. rod's network-idle wait cannot serve this
// job: its window is a minimum wait rather than a cap, and dashboards that
// poll (the Aliyun console polls continuously) never produce one, so it burned
// the whole navigation budget on every pass.
//
// The wait ends as soon as all required responses are captured. Hosts sharing
// a summarizer do not always call every endpoint it reads — BigModel has no
// model-usage endpoint — so it also ends once captures stop arriving for
// captureQuietWindow, making a missing endpoint cost a short quiet window
// instead of the budget. Services that name no responses keep the idle wait:
// without a data signal, a quiet network is the only evidence the page
// finished loading.
func waitForDashboardData(ctx context.Context, boundedPage *rod.Page, collector *responseCollector, requiredResponses []string) bool {
	if len(requiredResponses) == 0 {
		boundedPage.WaitRequestIdle(networkIdleLimit, nil, nil, nil)()
		return false
	}

	ticker := time.NewTicker(responsePollInterval)
	defer ticker.Stop()
	capturedCount := 0
	var quietSince time.Time
	for {
		captured, ready := collector.progress(requiredResponses)
		if ready {
			return true
		}
		// The quiet window only starts once something has been captured: a
		// dashboard whose first API call is still pending has not gone quiet,
		// it has not started.
		if captured != capturedCount {
			capturedCount = captured
			quietSince = time.Now()
		} else if captured > 0 && time.Since(quietSince) >= captureQuietWindow {
			return false
		}

		select {
		case <-ticker.C:
		case <-boundedPage.GetContext().Done():
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// probeWebDashboard navigates a page to the service URL and captures page
// text plus JSON API responses, mirroring probe_web_dashboard in
// ai_balance.py. The "extracted" and "links" diagnostic fields are not
// ported; no summarizer consumes them. requiredResponses names the API URL
// fragments the summarizer reads, so the wait ends as soon as that data
// lands. profileCollector runs between the initial and final text reads
// (ChatGPT Codex opens its profile menu there); nil skips it.
func probeWebDashboard(ctx context.Context, page *rod.Page, targetURL string, timeoutMS int, waitMS int, requiredResponses []string, profileCollector func(*rod.Page) string) map[string]any {
	if timeoutMS <= 0 {
		timeoutMS = defaultTimeoutMS
	}
	if waitMS < 0 {
		waitMS = defaultWaitMS
	}

	collector := collectResponses(page)
	result := map[string]any{
		"status":         "ok",
		"requested_url":  targetURL,
		"json_responses": []CapturedJSONResponse{},
	}

	// Every wait runs on a timeout-bounded page clone: a navigation that
	// never fires its lifecycle event (e.g. blocked by a beforeunload
	// dialog) or a frozen renderer must not stall the pass until the
	// refresh context expires.
	timeout := time.Duration(timeoutMS) * time.Millisecond
	boundedPage := page.Timeout(timeout)

	// Pre-register the navigation waiter: cross-process navigation
	// invalidates the session, so waiters must attach first.
	waitNavigation := boundedPage.WaitNavigation(proto.PageLifecycleEventNameDOMContentLoaded)
	pageInfo, infoErr := page.Info()
	if infoErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(infoErr.Error())
		return result
	}
	var navigateErr error
	if sameDocumentTarget(pageInfo.URL, targetURL) {
		// The tab already shows the dashboard: a plain navigation to the
		// same document can restore the SPA from cache without re-firing
		// its API requests, so force a cache-busting reload instead.
		navigateErr = proto.PageReload{IgnoreCache: true}.Call(boundedPage)
	} else {
		navigateErr = boundedPage.Navigate(targetURL)
	}
	if navigateErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(navigateErr.Error())
		return result
	}
	waitNavigation()
	// WaitLoad is best-effort after cross-process re-attach, mirroring the
	// tolerated Python timeouts.
	_ = boundedPage.WaitLoad()
	dataReady := waitForDashboardData(ctx, boundedPage, collector, requiredResponses)
	// The settle delay covers late XHRs the service cannot name; once every
	// named response is in hand there is nothing left to settle for.
	if waitMS > 0 && !dataReady {
		select {
		case <-time.After(time.Duration(waitMS) * time.Millisecond):
		case <-ctx.Done():
		}
	}

	targetInfo, infoErr := page.Info()
	if infoErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(infoErr.Error())
		return result
	}
	currentURL := targetInfo.URL

	initialBodyText, textErr := evalString(page, `() => document.body ? document.body.innerText : ""`)
	if textErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(textErr.Error())
		return result
	}

	profileUsageText := ""
	if profileCollector != nil && !detectLoginHint(currentURL, initialBodyText) {
		profileUsageText = profileCollector(page)
	}

	finalBodyText, finalTextErr := evalString(page, `() => document.body ? document.body.innerText : ""`)
	if finalTextErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(finalTextErr.Error())
		return result
	}

	title, titleErr := evalString(page, `() => document.title`)
	if titleErr != nil {
		result["status"] = "error"
		result["error"] = browserErrorMessage(titleErr.Error())
		return result
	}

	// Merge the three text passes, dropping empties and duplicates while
	// keeping order, like dict.fromkeys in the Python implementation.
	bodyText := joinUniqueTexts(initialBodyText, profileUsageText, finalBodyText)

	result["title"] = title
	result["current_url"] = currentURL
	result["login_hint"] = detectLoginHint(currentURL, bodyText)
	result["json_responses"] = collector.results(page)
	result["_visible_text"] = RedactText(bodyText)
	if profileUsageText != "" {
		result["_profile_usage_text"] = RedactText(profileUsageText)
	}

	if result["login_hint"] == true {
		result["status"] = "needs_login"
	}
	return result
}

// joinUniqueTexts joins non-empty texts with newlines, skipping duplicates.
func joinUniqueTexts(texts ...string) string {
	seen := map[string]bool{}
	joined := make([]string, 0, len(texts))
	for _, text := range texts {
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		joined = append(joined, text)
	}
	return strings.Join(joined, "\n")
}

// detectLoginHint mirrors detect_login_hint in ai_balance.py.
func detectLoginHint(pageURL string, bodyText string) bool {
	loweredURL := strings.ToLower(pageURL)
	for _, part := range []string{"/auth", "/login", "/oauth", "/signin", "/sign-in"} {
		if strings.Contains(loweredURL, part) {
			return true
		}
	}

	loweredText := strings.ToLower(bodyText)
	firstScreenText := loweredText
	if len(firstScreenText) > 500 {
		firstScreenText = firstScreenText[:500]
	}
	for _, term := range []string{"log in", "sign in", "登录", "登入"} {
		if strings.Contains(firstScreenText, term) {
			return true
		}
	}

	loginTerms := []string{"log in", "sign in", "continue with", "登录", "登入", "验证码", "verify your"}
	dataTerms := []string{"balance", "credit", "credits", "usage", "quota", "余额", "剩余", "额度"}
	hasLoginTerm := false
	for _, term := range loginTerms {
		if strings.Contains(loweredText, term) {
			hasLoginTerm = true
			break
		}
	}
	if !hasLoginTerm {
		return false
	}
	for _, term := range dataTerms {
		if strings.Contains(loweredText, term) {
			return false
		}
	}
	return true
}

// browserErrorMessage mirrors browser_error_message in ai_balance.py:
// friendly text for known profile-lock failures, redacted text otherwise.
func browserErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if strings.Contains(trimmed, "Opening in existing browser session") ||
		strings.Contains(trimmed, "profile is already in use") {
		return "Automation Chrome profile is already in use. Close any Chrome window opened from " +
			"profiles\\ai-balance-chrome, or kill the leftover chrome.exe process whose command line contains " +
			"profiles\\ai-balance-chrome."
	}
	if strings.Contains(trimmed, "Target page, context or browser has been closed") && strings.Contains(trimmed, "--headless") {
		return "Chrome exited during headless startup. The automation profile may still be locked by another " +
			"Chrome process. Close any Chrome window opened from profiles\\ai-balance-chrome, then retry. " +
			"If the problem persists, run with --headed once to inspect the browser."
	}
	return RedactText(trimmed)
}
