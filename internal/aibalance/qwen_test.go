package aibalance

import "testing"

// makeGatewayResponse mirrors make_gateway_response in
// tests/test_qwen_token_plan.py: the aliyun gateway envelope.
func makeGatewayResponse(urlSuffix string, responseData map[string]any) CapturedJSONResponse {
	return CapturedJSONResponse{
		URL:    "https://bailian-cs.console.aliyun.com/data/api.json?api=" + urlSuffix,
		Status: 200,
		JSON: map[string]any{
			"data": map[string]any{
				"DataV2": map[string]any{
					"data": map[string]any{
						"success": true,
						"data":    responseData,
					},
				},
			},
		},
	}
}

func TestSummarizeQwenPrefersAPIAndKeepsFractionalPercent(t *testing.T) {
	startTime := int64(1_786_430_181_000)
	endTime := int64(1_789_142_400_000)
	weeklyResetTime := int64(1_787_035_140_000)
	result := map[string]any{
		"status": "ok",
		"json_responses": []CapturedJSONResponse{
			makeGatewayResponse("/tokenplan/personal/api/v2/subscription", map[string]any{
				"specCode":      "lite",
				"remainingDays": 31,
				"startTime":     float64(startTime),
				"endTime":       float64(endTime),
				"autoRenewFlag": false,
				"status":        "VALID",
			}),
			makeGatewayResponse("/tokenplan/personal/api/v2/usage", map[string]any{
				"per1WeekPercentage": 0.02435088,
				"per1WeekResetTime":  float64(weeklyResetTime),
			}),
		},
		"_visible_text": "5小时限额\n限时取消限额\n7天限额\n2.44%已用",
	}

	summary := summarizeQwenTokenPlan(result)

	if summary["plan"] != "Lite" {
		t.Errorf("plan = %v, want Lite", summary["plan"])
	}
	if summary["remaining_days"] != 31 {
		t.Errorf("remaining_days = %v, want 31", summary["remaining_days"])
	}
	if summary["subscription_started_at"] != FormatEpochMillis(float64(startTime)) {
		t.Errorf("subscription_started_at = %v, want %v", summary["subscription_started_at"], FormatEpochMillis(float64(startTime)))
	}
	if summary["subscription_valid_until"] != FormatEpochMillis(float64(endTime)) {
		t.Errorf("subscription_valid_until = %v, want %v", summary["subscription_valid_until"], FormatEpochMillis(float64(endTime)))
	}
	if summary["auto_renew"] != false {
		t.Errorf("auto_renew = %v, want false", summary["auto_renew"])
	}
	fiveHour, isMap := summary["five_hour"].(map[string]any)
	if !isMap || fiveHour["unlimited"] != true {
		t.Errorf("five_hour = %#v, want unlimited", summary["five_hour"])
	}
	weekly := summary["weekly"].(map[string]any)
	if weekly["used_percent"] != 2.44 {
		t.Errorf("weekly used_percent = %v, want 2.44", weekly["used_percent"])
	}
	if weekly["remaining_percent"] != 97.56 {
		t.Errorf("weekly remaining_percent = %v, want 97.56", weekly["remaining_percent"])
	}
	if weekly["reset"] != FormatEpochMillis(float64(weeklyResetTime)) {
		t.Errorf("weekly reset = %v, want %v", weekly["reset"], FormatEpochMillis(float64(weeklyResetTime)))
	}
}

func TestSummarizeQwenUsesVisibleTextWhenAPIMissing(t *testing.T) {
	result := map[string]any{
		"status":         "ok",
		"json_responses": []CapturedJSONResponse{},
		"_visible_text": "Lite 套餐\n剩余天数\n31天\n开始时间\n2026-08-11 14:36:21\n结束时间\n" +
			"2026-09-12 00:00:00\n自动续费\n未开启\n5小时限额\n限时取消限额\n7天限额\n" +
			"将于 2026-08-18 14:39:00 (UTC+8) 重置刷新\n2.44%已用",
	}

	summary := summarizeQwenTokenPlan(result)

	if summary["plan"] != "Lite" {
		t.Errorf("plan = %v, want Lite", summary["plan"])
	}
	if summary["subscription_started_at"] != "2026-08-11 14:36 CST" {
		t.Errorf("subscription_started_at = %v, want 2026-08-11 14:36 CST", summary["subscription_started_at"])
	}
	if summary["subscription_valid_until"] != "2026-09-12 00:00 CST" {
		t.Errorf("subscription_valid_until = %v, want 2026-09-12 00:00 CST", summary["subscription_valid_until"])
	}
	if summary["auto_renew"] != false {
		t.Errorf("auto_renew = %v, want false", summary["auto_renew"])
	}
	fiveHour, isMap := summary["five_hour"].(map[string]any)
	if !isMap || fiveHour["unlimited"] != true {
		t.Errorf("five_hour = %#v, want unlimited", summary["five_hour"])
	}
	weekly := summary["weekly"].(map[string]any)
	if weekly["remaining_percent"] != 97.56 {
		t.Errorf("weekly remaining_percent = %v, want 97.56", weekly["remaining_percent"])
	}
	if weekly["reset"] != "2026-08-18 14:39 CST" {
		t.Errorf("weekly reset = %v, want 2026-08-18 14:39 CST", weekly["reset"])
	}
}
