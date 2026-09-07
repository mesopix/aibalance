package aibalance

import (
	"strings"
	"testing"
)

// tencentUsageFixture mirrors a live capture of the tokenplan console: the
// two gateway APIs with the real envelope shape (data.data.Response) and
// credit fields as decimal strings. Identifiers are anonymized.
const tencentUsageFixture = `[
  {"url": "https://console.cloud.tencent.com/cgi/capi?cmd=ListUserTokenPlans&serviceType=hunyuan", "json": {
    "code": 0,
    "data": {"cgwerrorCode": 0, "code": 0, "data": {"Response": {
      "RequestId": "fixture-list",
      "UserTokenPlanList": [
        {"Edition": "hunyuan", "ExpireTime": "2026-10-02 16:25:59", "Level": 1,
         "Plan": "tp_hy_lite", "QuotaStatus": 0, "RenewFlag": 0,
         "ResourceID": "sp_lmp_tokenplan-fixture-1", "StartTime": "2026-09-02 16:26:00"}
      ]
    }}},
    "errObj": {}, "mccode": 0
  }},
  {"url": "https://console.cloud.tencent.com/cgi/capi?cmd=DescribeTokenPlanUsage&serviceType=hunyuan", "json": {
    "code": 0,
    "data": {"cgwerrorCode": 0, "code": 0, "data": {"Response": {
      "RequestId": "fixture-usage",
      "TokenPlanUsageList": [
        {"TokenPlanPackage": {
           "ExpireTime": "2026-10-02 16:25:59", "Level": 1, "Plan": "tp_hy_lite",
           "QuotaStatus": 0, "RenewFlag": 0, "ResourceId": "sp_lmp_tokenplan-fixture-1",
           "StartTime": "2026-09-02 16:26:00"},
         "TokenPlanResource": {
           "CycleCapacity": "35000000", "CycleCapacityCredits": "560",
           "CycleInputUsage": "5746420", "CycleInputUsageCredits": "574.6059640384",
           "CycleOutputUsage": "80614", "CycleOutputUsageCredits": "15.9078880352",
           "CycleRemain": "0", "CycleRemainCredits": "0",
           "CycleTotalUsage": "35000000", "CycleTotalUsageCredits": "560",
           "DailyUsageList": [], "RemainCycles": "0", "TodayHourlyUsageList": []}}
      ]
    }}},
    "errObj": {}, "mccode": 0
  }}
]`

// tencentPlanListOnlyFixture exercises the fallback: usage reports no plans
// while the plan list still carries one.
const tencentPlanListOnlyFixture = `[
  {"url": "https://console.cloud.tencent.com/cgi/capi?cmd=ListUserTokenPlans&serviceType=hunyuan", "json": {
    "code": 0,
    "data": {"cgwerrorCode": 0, "code": 0, "data": {"Response": {
      "UserTokenPlanList": [
        {"Edition": "hunyuan", "ExpireTime": "2026-10-02 16:25:59",
         "Plan": "tp_hy_pro", "RenewFlag": 1,
         "ResourceID": "sp_lmp_tokenplan-fixture-2", "StartTime": "2026-09-02 16:26:00"}
      ]
    }}}
  }},
  {"url": "https://console.cloud.tencent.com/cgi/capi?cmd=DescribeTokenPlanUsage&serviceType=hunyuan", "json": {
    "code": 0,
    "data": {"cgwerrorCode": 0, "code": 0, "data": {"Response": {
      "TokenPlanUsageList": []
    }}}
  }}
]`

func TestSummarizeTencentTokenPlanUsage(t *testing.T) {
	result := zaiTestResult(t, tencentUsageFixture, "")
	summary := summarizeTencentTokenPlan(result)

	if summary["status"] != "ok" {
		t.Fatalf("status = %v, want ok", summary["status"])
	}
	rawPlans, isList := summary["plans"].([]any)
	if !isList || len(rawPlans) != 1 {
		t.Fatalf("plans = %#v, want one entry", summary["plans"])
	}
	plan, isMap := rawPlans[0].(map[string]any)
	if !isMap {
		t.Fatalf("plans[0] = %#v, want a map", rawPlans[0])
	}
	if plan["plan"] != "tp_hy_lite" || plan["plan_level"] != "Hy Lite" {
		t.Errorf("plan identity = %#v, want tp_hy_lite / Hy Lite", plan)
	}
	if plan["auto_renew"] != false {
		t.Errorf("auto_renew = %v, want false", plan["auto_renew"])
	}
	if plan["subscription_started_at"] != "2026-09-02 16:26 CST" {
		t.Errorf("subscription_started_at = %v", plan["subscription_started_at"])
	}
	if plan["subscription_valid_until"] != "2026-10-02 16:25 CST" {
		t.Errorf("subscription_valid_until = %v", plan["subscription_valid_until"])
	}

	monthly, isMap := plan["monthly"].(map[string]any)
	if !isMap {
		t.Fatalf("monthly missing: %#v", plan)
	}
	if monthly["remaining"] != float64(0) || monthly["limit"] != float64(560) ||
		monthly["used"] != float64(560) {
		t.Errorf("monthly amounts = %#v, want 0/560 used 560", monthly)
	}
	if monthly["remaining_percent"] != float64(0) {
		t.Errorf("remaining_percent = %v, want 0", monthly["remaining_percent"])
	}
	if monthly["reset"] != "2026-10-02 16:25 CST" {
		t.Errorf("reset = %v", monthly["reset"])
	}
}

func TestSummarizeTencentFallsBackToPlanList(t *testing.T) {
	result := zaiTestResult(t, tencentPlanListOnlyFixture, "")
	summary := summarizeTencentTokenPlan(result)

	rawPlans, isList := summary["plans"].([]any)
	if !isList || len(rawPlans) != 1 {
		t.Fatalf("plans = %#v, want one fallback entry", summary["plans"])
	}
	plan, isMap := rawPlans[0].(map[string]any)
	if !isMap {
		t.Fatalf("plans[0] = %#v, want a map", rawPlans[0])
	}
	if plan["plan_level"] != "Hy Pro" {
		t.Errorf("plan_level = %v, want Hy Pro", plan["plan_level"])
	}
	if plan["auto_renew"] != true {
		t.Errorf("auto_renew = %v, want true", plan["auto_renew"])
	}
	if _, hasQuota := plan["monthly"]; hasQuota {
		t.Errorf("monthly should be absent without usage data: %#v", plan)
	}
}

func TestSummarizeTencentNeedsLogin(t *testing.T) {
	summary := summarizeTencentTokenPlan(map[string]any{"status": "needs_login"})
	if summary["reason"] != "needs_login" {
		t.Errorf("reason = %v, want needs_login", summary["reason"])
	}
}

func TestFormatTencentView(t *testing.T) {
	// Feed the summarizer's in-memory output straight into the view layer:
	// both must agree on the []any list shape without a JSON round-trip.
	singleSummary := summarizeTencentTokenPlan(zaiTestResult(t, tencentUsageFixture, ""))

	view := formatServiceView("tencent_token_plan", singleSummary)
	if len(view.Quotas) != 1 || view.Quotas[0].Label != "monthly" {
		t.Errorf("single-plan quota labels = %#v, want [monthly]", view.Quotas)
	}
	if len(view.Facts) != 1 || !strings.HasPrefix(view.Facts[0], "Hy Lite · valid until ") {
		t.Errorf("facts = %#v, want a Hy Lite validity line", view.Facts)
	}

	dualSummary := map[string]any{
		"status": "ok",
		"plans": []any{
			map[string]any{
				"plan":       "tp_hy_lite",
				"plan_level": "Hy Lite",
				"monthly": map[string]any{
					"remaining": 100, "limit": 560, "remaining_percent": 17.86,
				},
			},
			map[string]any{
				"plan":       "tp_pro",
				"plan_level": "Pro",
				"monthly": map[string]any{
					"remaining": 3000, "limit": 5000, "remaining_percent": 60.0,
				},
			},
		},
	}

	dualView := formatServiceView("tencent_token_plan", dualSummary)
	wantLabels := []string{"hy monthly", "monthly"}
	if len(dualView.Quotas) != len(wantLabels) {
		t.Fatalf("dual-plan quotas = %#v, want %d rows", dualView.Quotas, len(wantLabels))
	}
	for quotaIndex, wantLabel := range wantLabels {
		if dualView.Quotas[quotaIndex].Label != wantLabel {
			t.Errorf("dual quota[%d] label = %q, want %q",
				quotaIndex, dualView.Quotas[quotaIndex].Label, wantLabel)
		}
	}
}
