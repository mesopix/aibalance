package aibalance

import "testing"

// TestDashboardReadinessChecks pins each service's readiness marker to its
// real summarizer: an empty capture set must never count as ready, while the
// minimal display-critical payload alone must.
func TestDashboardReadinessChecks(t *testing.T) {
	cases := []struct {
		name         string
		summarize    ServiceSummarizer
		summaryReady func(summary map[string]any) bool
		responseURL  string
		responseJSON map[string]any
	}{
		{
			name:         "z_ai",
			summarize:    summarizeZAI,
			summaryReady: zaiSummaryReady,
			responseURL:  "https://api.z.ai/api/monitor/usage/quota/limit",
			responseJSON: map[string]any{"data": map[string]any{
				"level": "PRO",
				"limits": []any{
					map[string]any{"type": "CREDIT_LIMIT", "unit": 3, "percentage": 42,
						"usage": 100, "currentValue": 42, "remaining": 58,
						"nextResetTime": "2026-09-24 18:00:00"},
					map[string]any{"type": "CREDIT_LIMIT", "unit": 6, "percentage": 20,
						"usage": 500, "currentValue": 100, "remaining": 400,
						"nextResetTime": "2026-09-28 00:00:00"},
				},
			}},
		},
		{
			name:         "bigmodel",
			summarize:    summarizeBigModel,
			summaryReady: zaiSummaryReady,
			responseURL:  "https://bigmodel.cn/api/monitor/usage/quota/limit",
			responseJSON: map[string]any{"data": map[string]any{
				"level": "LITE",
				"limits": []any{
					map[string]any{"type": "CREDIT_LIMIT", "unit": 3, "percentage": 7,
						"usage": 100, "currentValue": 7, "remaining": 93,
						"nextResetTime": "2026-09-24 18:00:00"},
				},
			}},
		},
		{
			name:         "qwen",
			summarize:    summarizeQwenTokenPlan,
			summaryReady: qwenSummaryReady,
			responseURL:  "https://bailian.console.aliyun.com/tokenplan/personal/api/v2/usage",
			responseJSON: map[string]any{"data": map[string]any{"DataV2": map[string]any{"data": map[string]any{
				"success": true,
				"data":    map[string]any{"per5HoursPercentage": 0.42},
			}}}},
		},
		{
			name:         "kimi",
			summarize:    summarizeKimi,
			summaryReady: kimiSummaryReady,
			responseURL:  "https://www.kimi.com/api/BillingService/GetUsages",
			responseJSON: map[string]any{"usages": []any{
				map[string]any{"scope": "FEATURE_CODING", "detail": map[string]any{
					"limit": 300, "used": 120, "remaining": 180,
					"resetTime": "2026-09-24 18:00:00",
				}},
			}},
		},
		{
			name:         "qoder",
			summarize:    summarizeQoder,
			summaryReady: qoderSummaryReady,
			responseURL:  "https://qoder.com/api/v2/me/usages/big_model_credits",
			responseJSON: map[string]any{"total_quota": map[string]any{"quota_summary": map[string]any{
				"limit_value": 600, "used_value": 100, "remaining_value": 500, "usage_percentage": 16,
			}}},
		},
		{
			name:         "tencent",
			summarize:    summarizeTencentTokenPlan,
			summaryReady: tencentSummaryReady,
			responseURL:  "https://console.cloud.tencent.com/api?cmd=DescribeTokenPlanUsage",
			responseJSON: map[string]any{"data": map[string]any{"data": map[string]any{"Response": map[string]any{
				"TokenPlanUsageList": []any{
					map[string]any{
						"TokenPlanPackage":  map[string]any{"Plan": "tp_lite", "ExpireTime": "2026-10-24 00:00:00"},
						"TokenPlanResource": map[string]any{"CycleRemainCredits": "50", "CycleCapacityCredits": "100"},
					},
				},
			}}}},
		},
	}

	for _, testCase := range cases {
		emptySummary := testCase.summarize(map[string]any{
			"status":         "ok",
			"json_responses": []CapturedJSONResponse{},
		})
		if testCase.summaryReady(emptySummary) {
			t.Errorf("%s: readiness accepted an empty summary: %#v", testCase.name, emptySummary)
		}

		trialSummary := testCase.summarize(map[string]any{
			"status": "ok",
			"json_responses": []CapturedJSONResponse{
				{URL: testCase.responseURL, JSON: testCase.responseJSON},
			},
		})
		if !testCase.summaryReady(trialSummary) {
			t.Errorf("%s: readiness rejected a summary carrying display data: %#v",
				testCase.name, trialSummary)
		}
	}
}
