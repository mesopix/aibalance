package aibalance

import "testing"

func TestExtractBankedResetCountEnglishText(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"1 reset available", 1},
		{"3 banked rate-limit resets available", 3},
		{"Available resets: 12", 12},
		{"Resets available: 0", 0},
	}
	for _, testCase := range cases {
		if got := extractBankedResetCountFromText(testCase.text); got == nil || *got != testCase.want {
			t.Errorf("extractBankedResetCountFromText(%q) = %v, want %d", testCase.text, got, testCase.want)
		}
	}
}

func TestExtractBankedResetCountChineseText(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"可用重置次数：2", 2},
		{"还有 4 次额度重置机会可用", 4},
	}
	for _, testCase := range cases {
		if got := extractBankedResetCountFromText(testCase.text); got == nil || *got != testCase.want {
			t.Errorf("extractBankedResetCountFromText(%q) = %v, want %d", testCase.text, got, testCase.want)
		}
	}
}

func TestExtractBankedResetCountNestedJSON(t *testing.T) {
	payload := map[string]any{
		"rateLimit": map[string]any{
			"primaryWindow": map[string]any{
				"resetAt": float64(1_900_000_000),
			},
			"bankedResets": map[string]any{
				"available": float64(3),
			},
		},
	}
	if got := extractBankedResetCountFromPayload(payload); got == nil || *got != 3 {
		t.Errorf("extractBankedResetCountFromPayload() = %v, want 3", got)
	}
}

func TestExtractBankedResetCountPreservesExplicitZero(t *testing.T) {
	payload := map[string]any{
		"codex": map[string]any{
			"availableResetCount": float64(0),
		},
	}
	if got := extractBankedResetCountFromPayload(payload); got == nil || *got != 0 {
		t.Errorf("extractBankedResetCountFromPayload() = %v, want 0", got)
	}
}

func TestExtractBankedResetCountIgnoresTimestamps(t *testing.T) {
	payload := map[string]any{
		"rateLimit": map[string]any{
			"primaryWindow": map[string]any{
				"resetAt":           float64(1_900_000_000),
				"resetAfterSeconds": float64(3_600),
				"resetTime":         "2030-03-17T08:00:00Z",
			},
		},
	}
	if got := extractBankedResetCountFromPayload(payload); got != nil {
		t.Errorf("extractBankedResetCountFromPayload() = %v, want nil", got)
	}
}

func TestSummarizeCodexPrefersLatestJSON(t *testing.T) {
	result := map[string]any{
		"status": "ok",
		"json_responses": []CapturedJSONResponse{
			{URL: "https://chatgpt.com/backend-api/codex/usage", Status: 200, JSON: map[string]any{"bankedResets": float64(4)}},
			{URL: "https://chatgpt.com/backend-api/codex/usage", Status: 200, JSON: map[string]any{"bankedResets": float64(2)}},
		},
		"_profile_usage_text": "1 reset available",
		"_visible_text":       "Weekly usage limit 80% remaining Resets 9:30 PM",
	}

	summary := summarizeChatGPTCodex(result)
	if summary["banked_resets_remaining"] != 2 {
		t.Errorf("banked_resets_remaining = %v, want 2", summary["banked_resets_remaining"])
	}
}

func TestSummarizeCodexTextFallbackAndUnknown(t *testing.T) {
	visibleResult := map[string]any{
		"status":              "ok",
		"json_responses":      []CapturedJSONResponse{},
		"_profile_usage_text": "1 reset available",
		"_visible_text":       "Weekly usage limit 80% remaining Resets 9:30 PM",
	}
	if got := summarizeChatGPTCodex(visibleResult)["banked_resets_remaining"]; got != 1 {
		t.Errorf("banked_resets_remaining = %v, want 1", got)
	}

	unknownResult := map[string]any{
		"status":         "ok",
		"json_responses": nil,
		"_visible_text":  "Weekly usage limit 80% remaining Resets 9:30 PM",
	}
	if got := summarizeChatGPTCodex(unknownResult)["banked_resets_remaining"]; got != nil {
		t.Errorf("banked_resets_remaining = %v, want nil", got)
	}
}

func TestSummarizeCodexWeeklyAndCredits(t *testing.T) {
	result := map[string]any{
		"status":         "ok",
		"json_responses": []CapturedJSONResponse{},
		"_visible_text":  "Weekly usage limit 80% remaining Resets Sep 1, 2026 9:30 PM\nCredits remaining 1,234.50\nTurns 1,000",
	}

	summary := summarizeChatGPTCodex(result)
	weekly, isMap := summary["weekly"].(map[string]any)
	if !isMap {
		t.Fatalf("weekly missing: %#v", summary)
	}
	if weekly["remaining_percent"] != 80 {
		t.Errorf("weekly remaining_percent = %v, want 80", weekly["remaining_percent"])
	}
	if weekly["reset"] != "2026-09-01 21:30 CST" {
		t.Errorf("weekly reset = %v, want 2026-09-01 21:30 CST", weekly["reset"])
	}
	if summary["credits_remaining"] != 1234.5 {
		t.Errorf("credits_remaining = %v, want 1234.5", summary["credits_remaining"])
	}
	if summary["turns"] != 1000 {
		t.Errorf("turns = %v, want 1000", summary["turns"])
	}
}

func TestCodexUsageSignal(t *testing.T) {
	plainResult := map[string]any{
		"status":         "ok",
		"json_responses": []CapturedJSONResponse{},
		"_visible_text":  "Some unrelated page",
	}
	if codexUsageSignal(plainResult) {
		t.Error("non-codex page should not signal usage")
	}

	usageResult := map[string]any{
		"status":         "ok",
		"json_responses": []CapturedJSONResponse{},
		"_visible_text":  "Codex weekly usage limit 80% remaining",
	}
	if !codexUsageSignal(usageResult) {
		t.Error("codex usage page should signal usage")
	}

	bankedResult := map[string]any{
		"status":         "ok",
		"json_responses": []CapturedJSONResponse{},
		"_visible_text":  "2 banked resets available",
	}
	if !codexUsageSignal(bankedResult) {
		t.Error("banked resets text should signal usage")
	}
}
