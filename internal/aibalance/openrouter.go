package aibalance

import (
	"context"
	"regexp"
	"strings"
)

// openRouterCreditsURL is the OpenRouter credits settings page.
const openRouterCreditsURL = "https://openrouter.ai/settings/credits"

// openRouterRequiredResponses names the transfer-eligibility XHR the credits
// page fires on every load; its arrival implies the SSR content is rendered.
var openRouterRequiredResponses = []string{"credits/transfer/eligibility"}

// openRouterBalanceLabelEval reads the SSR balance card's aria-label. It is
// the only place the pay-as-you-go total appears: the digits animate inside
// a number-flow element body.innerText cannot see, and no XHR carries them.
const openRouterBalanceLabelEval = `() => {
	const element = document.querySelector('[aria-label^="Total available credits"]');
	return element ? element.getAttribute("aria-label") : "";
}`

// openRouterBalancePattern extracts the amount from the aria-label, e.g.
// "Total available credits: $6.79".
var openRouterBalancePattern = regexp.MustCompile(`(?i)total available credits:\s*\$([\d,]+(?:\.\d+)?)`)

// runOpenRouterCredits probes the credits page, then lifts the balance
// figure out of the DOM. probeWebDashboard covers login detection and the
// XHR wait; the label read runs once the page has settled.
func runOpenRouterCredits(ctx context.Context, options RunOptions) map[string]any {
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
	page, acquireErr := acquireServicePage(browser, openRouterCreditsURL)
	if acquireErr != nil {
		return map[string]any{
			"status": "error",
			"error":  browserErrorMessage(acquireErr.Error()),
		}
	}

	result := probeWebDashboard(ctx, page, openRouterCreditsURL,
		options.TimeoutMS, options.WaitMS, openRouterRequiredResponses, nil)
	if result["status"] != "ok" {
		return result
	}

	balanceLabel, evalErr := evalString(page, openRouterBalanceLabelEval)
	if evalErr != nil {
		// A dead renderer cannot serve the text reads either; fail the pass
		// rather than report an OK account with no balance.
		result["status"] = "error"
		result["error"] = browserErrorMessage(evalErr.Error())
		return result
	}
	result["balance_label"] = balanceLabel
	return result
}

// summarizeOpenRouterCredits reduces the credits page to the public summary:
// the pay-as-you-go balance. No limit or cycle exists, so the credits entry
// only carries the remaining amount.
func summarizeOpenRouterCredits(result map[string]any) map[string]any {
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

	balanceLabel, _ := result["balance_label"].(string)
	match := openRouterBalancePattern.FindStringSubmatch(balanceLabel)
	if match == nil {
		return summary
	}
	amount := ToFloat(strings.ReplaceAll(match[1], ",", ""))
	if amount != nil {
		summary["credits"] = map[string]any{"remaining": roundTo2(*amount)}
	}
	return summary
}
