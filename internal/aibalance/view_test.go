package aibalance

import "testing"

func TestFormatSummaryViewsZAI(t *testing.T) {
	summary := map[string]any{
		"accounts": map[string]any{
			"bigmodel_coding_plan": map[string]any{
				"status":     "ok",
				"plan_level": "lite",
				"five_hour": map[string]any{
					"remaining": 2000, "limit": 2000, "used": 0,
					"remaining_percent": 100, "reset": "Unused, no reset yet",
				},
				"weekly": map[string]any{
					"remaining": 2702, "limit": 10000, "used": 7297,
					"remaining_percent": 28, "reset": "2026-08-28 22:04 CST",
				},
				"total_tokens": float64(76334533),
			},
		},
	}

	views := FormatSummaryViews(summary)
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	view := views[0]
	if view.ServiceID != "bigmodel_coding_plan" {
		t.Errorf("view ServiceID = %q, want bigmodel_coding_plan", view.ServiceID)
	}
	if view.Name != "BigModel Coding" || view.Status != "OK" {
		t.Errorf("view = %s %s, want BigModel Coding OK", view.Name, view.Status)
	}
	if len(view.Quotas) != 2 {
		t.Fatalf("quotas = %d, want 2", len(view.Quotas))
	}
	weekly := view.Quotas[1]
	if weekly.Label != "7d" || ToFloat(weekly.PercentLeft) == nil || *ToFloat(weekly.PercentLeft) != 28 {
		t.Errorf("weekly quota = %#v, want label 7d percent 28", weekly)
	}
	if weekly.Reset != "2026-08-28 22:04 CST" {
		t.Errorf("weekly reset = %q, want 2026-08-28 22:04 CST", weekly.Reset)
	}
	// Overview counters (tokens/credits/calls) no longer surface as facts.
	if len(view.Facts) != 0 {
		t.Errorf("facts = %#v, want none", view.Facts)
	}
}

func TestFormatSummaryViewsKimiWindowOrder(t *testing.T) {
	summary := map[string]any{
		"accounts": map[string]any{
			"kimi_coding_plan": map[string]any{
				"status":              "ok",
				"plan":                "Kimi Membership",
				"current_period_end":  "2026-09-10 08:00 CST",
				"window": map[string]any{
					"duration_minutes":  300,
					"remaining":         0,
					"limit":             100,
					"remaining_percent": 0,
					"reset":             "2026-08-25 22:56 CST",
				},
				"weekly": map[string]any{
					"remaining":         67,
					"limit":             100,
					"remaining_percent": 67,
					"reset":             "2026-09-01 13:56 CST",
				},
			},
		},
	}

	views := FormatSummaryViews(summary)
	view := views[0]
	if len(view.Quotas) != 2 {
		t.Fatalf("quotas = %d, want 2", len(view.Quotas))
	}
	// The 5h window renders above the 7d weekly quota.
	if view.Quotas[0].Label != "5h" || view.Quotas[1].Label != "7d" {
		t.Errorf("quota labels = %q, %q; want 5h, 7d",
			view.Quotas[0].Label, view.Quotas[1].Label)
	}
	// The plan name is dropped; only the period end survives, compacted.
	if len(view.Facts) != 1 || view.Facts[0] != "period end 09-10 08:00" {
		t.Errorf("facts = %#v, want compacted period end only", view.Facts)
	}
}

func TestFormatSummaryViewsFailureStates(t *testing.T) {
	summary := map[string]any{
		"accounts": map[string]any{
			"deepseek_api": map[string]any{
				"status": "skipped", "reason": "missing_env",
			},
			"qwen_token_plan": map[string]any{
				"status": "needs_login", "reason": "needs_login",
			},
		},
	}

	views := FormatSummaryViews(summary)
	// ServiceOrder puts qwen first, deepseek last.
	if views[0].Status != "NEEDS_LOGIN" || views[0].Detail != "run --login-setup" {
		t.Errorf("qwen view = %#v, want NEEDS_LOGIN with login hint", views[0])
	}
	lastView := views[len(views)-1]
	if lastView.Status != "SKIPPED" || lastView.Detail != "missing DEEPSEEK_API_KEY" {
		t.Errorf("deepseek view = %#v, want SKIPPED missing env", lastView)
	}
}

func TestFormatSummaryViewsQwenUnlimited(t *testing.T) {
	summary := map[string]any{
		"accounts": map[string]any{
			"qwen_token_plan": map[string]any{
				"status": "ok",
				"five_hour": map[string]any{
					"unlimited": true,
				},
				"weekly": map[string]any{
					"remaining_percent": 97.56, "reset": "2026-08-18 14:39 CST",
				},
				"subscription_valid_until": "2026-10-01 00:00 CST",
			},
		},
	}

	views := FormatSummaryViews(summary)
	view := views[0]
	if len(view.Quotas) != 2 {
		t.Fatalf("quotas = %d, want 2", len(view.Quotas))
	}
	if !view.Quotas[0].Unlimited || view.Quotas[0].Detail == "" {
		t.Errorf("five_hour quota = %#v, want unlimited with detail", view.Quotas[0])
	}
	if ToFloat(view.Quotas[1].PercentLeft) == nil || *ToFloat(view.Quotas[1].PercentLeft) != 97.56 {
		t.Errorf("weekly percent = %v, want 97.56", view.Quotas[1].PercentLeft)
	}
	// The plan name and start date are dropped; only the expiry survives,
	// compacted to the reset-time style.
	if len(view.Facts) != 1 || view.Facts[0] != "valid until 10-01 00:00" {
		t.Errorf("facts = %#v, want compacted valid until only", view.Facts)
	}
}

func TestFormatSummaryViewsQoderLabel(t *testing.T) {
	summary := map[string]any{
		"accounts": map[string]any{
			"qoder_team_credit": map[string]any{
				"status": "ok",
				"plan_quota": map[string]any{
					"remaining": 316, "limit": 3000, "remaining_percent": 10.5,
				},
				"next_reset": "2026-09-01 00:00 CST",
			},
		},
	}

	views := FormatSummaryViews(summary)
	if len(views) != 1 || len(views[0].Quotas) != 1 {
		t.Fatalf("views = %#v, want one qoder quota", views)
	}
	if views[0].Quotas[0].Label != "all" {
		t.Errorf("qoder label = %q, want all", views[0].Quotas[0].Label)
	}
	if views[0].Quotas[0].Reset != "2026-09-01 00:00 CST" {
		t.Errorf("qoder reset = %q, want next_reset passthrough", views[0].Quotas[0].Reset)
	}
	if len(views[0].Facts) != 0 {
		t.Errorf("facts = %#v, want none", views[0].Facts)
	}
}

func TestStatusLabel(t *testing.T) {
	cases := []struct {
		result map[string]any
		want   string
	}{
		{map[string]any{"status": "ok"}, "OK"},
		{map[string]any{"status": "needs_login"}, "NEEDS_LOGIN"},
		{map[string]any{"status": "skipped"}, "SKIPPED"},
		{map[string]any{"status": "partial"}, "PARTIAL"},
		{map[string]any{"status": "error"}, "ERROR"},
		{nil, "SKIPPED"},
	}
	for _, testCase := range cases {
		if got := StatusLabel(testCase.result); got != testCase.want {
			t.Errorf("StatusLabel(%v) = %q, want %q", testCase.result, got, testCase.want)
		}
	}
}
