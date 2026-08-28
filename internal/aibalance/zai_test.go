package aibalance

import (
	"encoding/json"
	"testing"
)

// zaiTestResult builds a probe result from raw JSON response fixtures,
// mirroring the inline fixtures of tests/test_bigmodel_coding_plan.py.
func zaiTestResult(t *testing.T, rawResponses string, visibleText string) map[string]any {
	t.Helper()
	var rawList []struct {
		URL  string          `json:"url"`
		JSON json.RawMessage `json:"json"`
	}
	if unmarshalErr := json.Unmarshal([]byte(rawResponses), &rawList); unmarshalErr != nil {
		t.Fatalf("unmarshal fixtures: %v", unmarshalErr)
	}

	captured := make([]CapturedJSONResponse, 0, len(rawList))
	for _, rawItem := range rawList {
		var payload map[string]any
		if unmarshalErr := json.Unmarshal(rawItem.JSON, &payload); unmarshalErr != nil {
			t.Fatalf("unmarshal payload for %s: %v", rawItem.URL, unmarshalErr)
		}
		captured = append(captured, CapturedJSONResponse{
			URL:    rawItem.URL,
			Status: 200,
			JSON:   payload,
		})
	}

	result := map[string]any{
		"status":         "ok",
		"json_responses": captured,
	}
	if visibleText != "" {
		result["_visible_text"] = visibleText
	}
	return result
}

// bigmodelUsageFixture mirrors test_summarize_bigmodel_usage.
const bigmodelUsageFixture = `[
  {"url": "https://bigmodel.cn/api/monitor/usage/quota/limit", "json": {
    "data": {"level": "pro", "limits": [
      {"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "usage": 1000, "currentValue": 250, "remaining": 750, "percentage": 25, "nextResetTime": 1910000000000},
      {"type": "TOKENS_LIMIT", "unit": 6, "number": 1, "usage": 5000, "currentValue": 1000, "remaining": 4000, "percentage": 20, "nextResetTime": 1910500000000},
      {"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 100, "currentValue": 12, "remaining": 88, "percentage": 12,
       "usageDetails": [{"modelCode": "search-prime", "usage": 7}, {"modelCode": "web-reader", "usage": 5}]}
    ]}}},
  {"url": "https://bigmodel.cn/api/monitor/usage/model-usage?startTime=2026-08-16", "json": {
    "data": {
      "x_time": ["2026-08-16 23:00:00", "2026-08-17 00:00:00", "2026-08-17 01:00:00"],
      "tokensUsage": [100, 200, 300],
      "modelCallCount": [1, 2, 3],
      "totalUsage": {"totalTokensUsage": 600, "totalModelCallCount": 6,
        "modelSummaryList": [{"modelName": "glm-5", "totalTokens": 500}, {"modelName": "glm-4.7", "totalTokens": 100}]}
    }}}
]`

func TestSummarizeZAIQuotaAndModelUsage(t *testing.T) {
	result := zaiTestResult(t, bigmodelUsageFixture, "")
	summary := summarizeZAIWithHost(result, "bigmodel.cn")

	if summary["plan_level"] != "pro" {
		t.Errorf("plan_level = %v, want pro", summary["plan_level"])
	}
	fiveHour := summary["five_hour"].(map[string]any)
	if fiveHour["used"] != 250 || fiveHour["limit"] != 1000 || fiveHour["remaining"] != 750 {
		t.Errorf("five_hour = %#v, want used=250 limit=1000 remaining=750", fiveHour)
	}
	if fiveHour["remaining_percent"] != 75 {
		t.Errorf("five_hour remaining_percent = %v, want 75", fiveHour["remaining_percent"])
	}
	monthlyTools := summary["monthly_tools"].(map[string]any)
	wantDetails := []any{
		map[string]any{"name": "search-prime", "used": 7},
		map[string]any{"name": "web-reader", "used": 5},
	}
	if !reflectEqual(monthlyTools["usage_details"], wantDetails) {
		t.Errorf("monthly_tools usage_details = %#v, want %#v", monthlyTools["usage_details"], wantDetails)
	}
	if summary["total_tokens"] != 600 {
		t.Errorf("total_tokens = %v, want 600", summary["total_tokens"])
	}
	if summary["total_model_calls"] != 6 {
		t.Errorf("total_model_calls = %v, want 6", summary["total_model_calls"])
	}
	wantDaily := []any{
		map[string]any{"date": "2026-08-16", "tokens": 100, "calls": 1},
		map[string]any{"date": "2026-08-17", "tokens": 500, "calls": 5},
	}
	if !reflectEqual(summary["daily_usage"], wantDaily) {
		t.Errorf("daily_usage = %#v, want %#v", summary["daily_usage"], wantDaily)
	}
	wantModels := []any{
		map[string]any{"name": "glm-5", "tokens": 500},
		map[string]any{"name": "glm-4.7", "tokens": 100},
	}
	if !reflectEqual(summary["model_usage"], wantModels) {
		t.Errorf("model_usage = %#v, want %#v", summary["model_usage"], wantModels)
	}
}

// bigmodelCreditFixture mirrors test_summarize_bigmodel_v3_credit_usage.
const bigmodelCreditFixture = `[
  {"url": "https://bigmodel.cn/api/monitor/usage/quota/limit", "json": {
    "data": {"level": "lite", "limits": [
      {"type": "CREDIT_LIMIT", "unit": 3, "number": 5, "usage": 2000, "currentValue": 0, "remaining": 2000, "percentage": 0},
      {"type": "CREDIT_LIMIT", "unit": 6, "number": 1, "usage": 10000, "currentValue": 2004, "remaining": 7995, "percentage": 20, "nextResetTime": 1787321070959}
    ]}}},
  {"url": "https://bigmodel.cn/api/monitor/credit-usage/activity?granularity=DAY", "json": {
    "data": {
      "summary": {"totalTokens": 19628701, "peakDailyTokens": 19628701, "peakDailyTokensDate": "2026-08-14",
        "totalUsageDurationMs": 3411155, "currentStreakDays": 0, "longestStreakDays": 1},
      "series": [
        {"date": "2026-08-13", "totalCredits": "0", "totalTokens": 0, "mcpCalls": 0},
        {"date": "2026-08-14", "totalCredits": "2004.6278", "totalTokens": 19628701, "mcpCalls": 0}
      ]
    }}},
  {"url": "https://bigmodel.cn/api/monitor/credit-usage/usage-detail?usageType=MODEL", "json": {
    "data": {
      "summary": {
        "cacheHitRate": {"value": "0.9738"}, "offPeakUsageRate": {"value": "1.0000"},
        "totalCredits": {"value": "2004.6278"}, "averageDailyCredits": {"value": "286.3754"}},
      "modelUsage": {
        "totalUsage": {"totalTokens": 19628701, "totalCredits": "2004.6278"},
        "modelSummaryList": [{"modelName": "GLM-5.3", "totalTokens": 19628701, "totalCredits": "2004.6278"}]}
    }}},
  {"url": "https://bigmodel.cn/api/monitor/credit-usage/usage-detail?usageType=MCP", "json": {
    "data": {"mcpUsage": {"totalUsage": {"totalMcpCalls": 0, "totalCredits": "0.0000"}}}}}
]`

func TestSummarizeZAICreditUsage(t *testing.T) {
	result := zaiTestResult(t, bigmodelCreditFixture, "最近刷新时间：2026.08.17 15:06")
	summary := summarizeZAIWithHost(result, "bigmodel.cn")

	if summary["plan_level"] != "lite" {
		t.Errorf("plan_level = %v, want lite", summary["plan_level"])
	}
	fiveHour := summary["five_hour"].(map[string]any)
	if fiveHour["remaining_percent"] != 100 {
		t.Errorf("five_hour remaining_percent = %v, want 100", fiveHour["remaining_percent"])
	}
	if fiveHour["reset"] != resetUnusedText {
		t.Errorf("five_hour reset = %v, want %q", fiveHour["reset"], resetUnusedText)
	}
	weekly := summary["weekly"].(map[string]any)
	if weekly["remaining_percent"] != 80 {
		t.Errorf("weekly remaining_percent = %v, want 80", weekly["remaining_percent"])
	}
	if weekly["reset"] != "2026-08-21 22:04 CST" {
		t.Errorf("weekly reset = %v, want 2026-08-21 22:04 CST", weekly["reset"])
	}
	if _, exists := summary["monthly_tools"]; exists {
		t.Error("monthly_tools should be absent")
	}
	if summary["total_tokens"] != 19628701 {
		t.Errorf("total_tokens = %v, want 19628701", summary["total_tokens"])
	}
	if summary["total_credits"] != 2004.6278 {
		t.Errorf("total_credits = %v, want 2004.6278", summary["total_credits"])
	}
	if summary["total_usage_minutes"] != 56 {
		t.Errorf("total_usage_minutes = %v, want 56", summary["total_usage_minutes"])
	}
	if summary["peak_tokens"] != 19628701 {
		t.Errorf("peak_tokens = %v, want 19628701", summary["peak_tokens"])
	}
	if summary["peak_tokens_date"] != "2026-08-14" {
		t.Errorf("peak_tokens_date = %v, want 2026-08-14", summary["peak_tokens_date"])
	}
	if summary["current_streak_days"] != 0 {
		t.Errorf("current_streak_days = %v, want 0", summary["current_streak_days"])
	}
	if summary["longest_streak_days"] != 1 {
		t.Errorf("longest_streak_days = %v, want 1", summary["longest_streak_days"])
	}
	if summary["cache_hit_percent"] != 97.38 {
		t.Errorf("cache_hit_percent = %v, want 97.38", summary["cache_hit_percent"])
	}
	if summary["off_peak_usage_percent"] != 100.0 {
		t.Errorf("off_peak_usage_percent = %v, want 100.0", summary["off_peak_usage_percent"])
	}
	if summary["average_daily_credits"] != 286.3754 {
		t.Errorf("average_daily_credits = %v, want 286.3754", summary["average_daily_credits"])
	}
	if summary["total_mcp_calls"] != 0 {
		t.Errorf("total_mcp_calls = %v, want 0", summary["total_mcp_calls"])
	}
	if summary["total_mcp_credits"] != 0.0 {
		t.Errorf("total_mcp_credits = %v, want 0.0", summary["total_mcp_credits"])
	}
	if summary["last_updated"] != "2026-08-17 15:06 CST" {
		t.Errorf("last_updated = %v, want 2026-08-17 15:06 CST", summary["last_updated"])
	}
	wantModels := []any{
		map[string]any{"name": "GLM-5.3", "tokens": 19628701, "credits": 2004.6278},
	}
	if !reflectEqual(summary["model_usage"], wantModels) {
		t.Errorf("model_usage = %#v, want %#v", summary["model_usage"], wantModels)
	}
	wantDaily := []any{
		map[string]any{"date": "2026-08-14", "tokens": 19628701, "credits": 2004.6278, "mcp_calls": 0},
	}
	if !reflectEqual(summary["daily_usage"], wantDaily) {
		t.Errorf("daily_usage = %#v, want %#v", summary["daily_usage"], wantDaily)
	}
}

// bigmodelZeroRingFixture mirrors the live BigModel page: the quota API
// reports percentage 85 for the weekly window while the page ring still
// renders a bare "0" (background tabs never run the count-up animation).
const bigmodelZeroRingFixture = `[
  {"url": "https://bigmodel.cn/api/monitor/usage/quota/limit", "json": {
    "data": {"level": "lite", "limits": [
      {"type": "CREDIT_LIMIT", "unit": 3, "number": 5, "usage": 2000, "currentValue": 0, "remaining": 2000, "percentage": 0},
      {"type": "CREDIT_LIMIT", "unit": 6, "number": 1, "usage": 10000, "currentValue": 8588, "remaining": 1411, "percentage": 85, "nextResetTime": 1787925870998}
    ]}}}
]`

// bigmodelZeroRingVisibleText is the visible text captured from the live
// BigModel usage page around the two quota cards.
const bigmodelZeroRingVisibleText = "5小时额度\n0\n%\n已使用\n积分\n0\n/\n2,000\n周额度\n0\n%\n已使用\n积分\n8,588\n/\n1万\n重置时间：2026-08-28 22:04\n"

func TestSummarizeZAIOverlayDoesNotClobberAPIPercent(t *testing.T) {
	result := zaiTestResult(t, bigmodelZeroRingFixture, bigmodelZeroRingVisibleText)
	summary := summarizeZAIWithHost(result, "bigmodel.cn")

	weekly := summary["weekly"].(map[string]any)
	if weekly["used_percent"] != 85 {
		t.Errorf("weekly used_percent = %v, want 85 (API value, not the page ring's 0)", weekly["used_percent"])
	}
	if weekly["remaining_percent"] != 15 {
		t.Errorf("weekly remaining_percent = %v, want 15", weekly["remaining_percent"])
	}
	if weekly["remaining"] != 1411 || weekly["limit"] != 10000 {
		t.Errorf("weekly remaining/limit = %v/%v, want 1411/10000", weekly["remaining"], weekly["limit"])
	}
	fiveHour := summary["five_hour"].(map[string]any)
	if fiveHour["used_percent"] != 0 || fiveHour["remaining_percent"] != 100 {
		t.Errorf("five_hour percents = %v/%v, want 0/100", fiveHour["used_percent"], fiveHour["remaining_percent"])
	}
}

func TestSummarizeZAIOverlayFillsMissingAPIPercent(t *testing.T) {
	visibleText := "5 Hours Quota\n42\nReset Time: 2026-08-21 22:04\nWeekly Quota\n7\n"
	result := zaiTestResult(t, "[]", visibleText)
	summary := summarizeZAIWithHost(result, "bigmodel.cn")

	fiveHour := summary["five_hour"].(map[string]any)
	if fiveHour["used_percent"] != 42 || fiveHour["remaining_percent"] != 58 {
		t.Errorf("five_hour percents = %v/%v, want 42/58", fiveHour["used_percent"], fiveHour["remaining_percent"])
	}
	weekly := summary["weekly"].(map[string]any)
	if weekly["used_percent"] != 7 || weekly["remaining_percent"] != 93 {
		t.Errorf("weekly percents = %v/%v, want 7/93", weekly["used_percent"], weekly["remaining_percent"])
	}
}

func TestParseZAIVisibleUsageSkipsDecimalPercent(t *testing.T) {
	parsed := parseZAIVisibleUsage("Weekly Quota\n0.86\n%\n7\n")
	weekly := parsed["weekly"].(map[string]any)
	if weekly["used_percent"] != 7 {
		t.Errorf("weekly used_percent = %v, want 7 (fractional ring value skipped)", weekly["used_percent"])
	}

	decimalOnly := parseZAIVisibleUsage("Weekly Quota\n0.86\n%\n")
	if _, has := decimalOnly["weekly"].(map[string]any)["used_percent"]; has {
		t.Errorf("decimal-only ring value should not produce used_percent: %#v", decimalOnly["weekly"])
	}
}

func TestSummarizeZAINeedsLogin(t *testing.T) {
	result := map[string]any{"status": "needs_login"}
	summary := summarizeZAI(result)
	if summary["status"] != "needs_login" || summary["reason"] != "needs_login" {
		t.Errorf("summary = %#v, want needs_login", summary)
	}
}

func TestSummarizeZAIError(t *testing.T) {
	result := map[string]any{"status": "error", "error": "boom"}
	summary := summarizeZAI(result)
	if summary["status"] != "error" || summary["error"] != "boom" {
		t.Errorf("summary = %#v, want error boom", summary)
	}
}

func TestSummarizeZAISubscription(t *testing.T) {
	fixture := `[
  {"url": "https://api.z.ai/api/biz/subscription/list", "json": {
    "data": [
      {"status": "EXPIRED", "inCurrentPeriod": false, "autoRenew": true},
      {"status": "VALID", "inCurrentPeriod": true, "autoRenew": 1, "nextRenewTime": 1787321070959}
    ]}}
]`
	result := zaiTestResult(t, fixture, "")
	summary := summarizeZAI(result)
	if summary["subscription_valid_until"] != "2026-08-21 22:04 CST" {
		t.Errorf("subscription_valid_until = %v, want 2026-08-21 22:04 CST", summary["subscription_valid_until"])
	}
	if summary["auto_renew"] != true {
		t.Errorf("auto_renew = %v, want true", summary["auto_renew"])
	}
}

func TestParseZAIVisibleUsage(t *testing.T) {
	visibleText := "5 Hours Quota\n42\nReset Time: 2026-08-21 22:04\nWeekly Quota\n7\nTotal Monthly Web Search / Reader / Zread Quota\n0\nLast Updated: 2026-08-17 15:06\nTotal Tokens\n123456\n"
	parsed := parseZAIVisibleUsage(visibleText)

	fiveHour := parsed["five_hour"].(map[string]any)
	if fiveHour["used_percent"] != 42 {
		t.Errorf("five_hour used_percent = %v, want 42", fiveHour["used_percent"])
	}
	if fiveHour["reset"] != "2026-08-21 22:04 CST" {
		t.Errorf("five_hour reset = %v, want 2026-08-21 22:04 CST", fiveHour["reset"])
	}
	weekly := parsed["weekly"].(map[string]any)
	if weekly["used_percent"] != 7 {
		t.Errorf("weekly used_percent = %v, want 7", weekly["used_percent"])
	}
	if parsed["last_updated"] != "2026-08-17 15:06 CST" {
		t.Errorf("last_updated = %v, want 2026-08-17 15:06 CST", parsed["last_updated"])
	}
	if parsed["total_tokens"] != 123456 {
		t.Errorf("total_tokens = %v, want 123456", parsed["total_tokens"])
	}
}

func TestZAILimitKey(t *testing.T) {
	cases := []struct {
		limit map[string]any
		want  string
	}{
		{map[string]any{"type": "CREDIT_LIMIT", "unit": 3}, "five_hour"},
		{map[string]any{"type": "TOKENS_LIMIT", "unit": 6}, "weekly"},
		{map[string]any{"type": "TIME_LIMIT", "unit": 5}, "monthly_tools"},
		{map[string]any{"type": "CREDIT_LIMIT", "unit": 5}, ""},
		{map[string]any{"type": "UNKNOWN", "unit": 3}, ""},
	}
	for _, testCase := range cases {
		if got := zAILimitKey(testCase.limit); got != testCase.want {
			t.Errorf("zAILimitKey(%v) = %q, want %q", testCase.limit, got, testCase.want)
		}
	}
}

// reflectEqual compares values through their JSON encoding so map/slice
// literals built in tests compare structurally.
func reflectEqual(got any, want any) bool {
	gotEncoded, gotErr := json.Marshal(got)
	wantEncoded, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && string(gotEncoded) == string(wantEncoded)
}
