package aibalance

import (
	"testing"
)

func TestSummarizeOpenRouterCredits(t *testing.T) {
	result := map[string]any{
		"status":        "ok",
		"balance_label": "Total available credits: $6.79",
	}
	summary := summarizeOpenRouterCredits(result)

	if summary["status"] != "ok" {
		t.Fatalf("status = %v, want ok", summary["status"])
	}
	credits, isMap := summary["credits"].(map[string]any)
	if !isMap {
		t.Fatalf("credits missing: %#v", summary)
	}
	if credits["remaining"] != float64(6.79) {
		t.Errorf("remaining = %v, want 6.79", credits["remaining"])
	}
	if _, hasPercent := credits["remaining_percent"]; hasPercent {
		t.Errorf("pay-as-you-go balance must not carry a percent: %#v", credits)
	}
}

func TestSummarizeOpenRouterCreditsThousandSeparator(t *testing.T) {
	result := map[string]any{
		"status":        "ok",
		"balance_label": "Total available credits: $1,234.50",
	}
	summary := summarizeOpenRouterCredits(result)

	credits, isMap := summary["credits"].(map[string]any)
	if !isMap {
		t.Fatalf("credits missing: %#v", summary)
	}
	if credits["remaining"] != float64(1234.5) {
		t.Errorf("remaining = %v, want 1234.5", credits["remaining"])
	}
}

func TestSummarizeOpenRouterCreditsWithoutLabel(t *testing.T) {
	summary := summarizeOpenRouterCredits(map[string]any{"status": "ok"})
	if _, hasCredits := summary["credits"]; hasCredits {
		t.Errorf("credits should be absent without a label: %#v", summary)
	}
}

func TestSummarizeOpenRouterCreditsNeedsLogin(t *testing.T) {
	summary := summarizeOpenRouterCredits(map[string]any{"status": "needs_login"})
	if summary["reason"] != "needs_login" {
		t.Errorf("reason = %v, want needs_login", summary["reason"])
	}
}

func TestFormatOpenRouterView(t *testing.T) {
	// Feed the summarizer's in-memory output straight into the view layer;
	// both must agree on shapes without a JSON round-trip.
	summary := summarizeOpenRouterCredits(map[string]any{
		"status":        "ok",
		"balance_label": "Total available credits: $6.79",
	})

	view := formatServiceView("openrouter_credits", summary)
	if len(view.Quotas) != 1 {
		t.Fatalf("quotas = %#v, want one credits row", view.Quotas)
	}
	quota := view.Quotas[0]
	if quota.Label != "credits" {
		t.Errorf("label = %q, want credits", quota.Label)
	}
	if quota.Detail != "$6.79 available" {
		t.Errorf("detail = %q, want $6.79 available", quota.Detail)
	}
	if quota.PercentLeft != nil || quota.Limit != nil {
		t.Errorf("limit-less balance must not carry limit/percent: %#v", quota)
	}

	emptyView := formatServiceView("openrouter_credits", map[string]any{"status": "ok"})
	if len(emptyView.Facts) != 1 || emptyView.Facts[0] != "no balance data" {
		t.Errorf("facts = %#v, want a no-balance line", emptyView.Facts)
	}
}
