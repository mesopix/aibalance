package aibalance

import (
	"strings"
)

// tencentTokenPlanURL is the TokenHub Token Plan console page.
const tencentTokenPlanURL = "https://console.cloud.tencent.com/tokenhub/tokenplan"

// tencentRequiredResponses are the console gateway APIs
// summarizeTencentTokenPlan reads; both fire on every load of the page.
var tencentRequiredResponses = []string{
	"cmd=ListUserTokenPlans",
	"cmd=DescribeTokenPlanUsage",
}

// summarizeTencentTokenPlan reduces the TokenHub Token Plan console APIs to
// the public summary: one entry per active plan carrying its monthly credit
// quota (CycleRemainCredits / CycleCapacityCredits).
func summarizeTencentTokenPlan(result map[string]any) map[string]any {
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

	if plans := summarizeTencentPlans(result); len(plans) > 0 {
		summary["plans"] = plans
	}
	return summary
}

// summarizeTencentPlans builds one summary entry per plan.
// DescribeTokenPlanUsage is authoritative (package metadata plus cycle
// usage); ListUserTokenPlans backfills metadata-only entries when the
// usage list comes back empty.
func summarizeTencentPlans(result map[string]any) []any {
	usageList := consoleResponseList(
		findTencentConsoleResponse(result, "cmd=DescribeTokenPlanUsage"), "TokenPlanUsageList")
	// Entries are stored as []any so in-memory consumers see the same shape
	// a JSON round-trip through latest_summary.json produces.
	plans := make([]any, 0, len(usageList))
	for _, usageEntry := range usageList {
		planPackage, _ := usageEntry["TokenPlanPackage"].(map[string]any)
		planResource, _ := usageEntry["TokenPlanResource"].(map[string]any)
		plan := tencentPlanSummary(planPackage)
		if quota := tencentMonthlyQuota(planPackage, planResource); len(quota) > 0 {
			plan["monthly"] = quota
		}
		if len(plan) > 0 {
			plans = append(plans, plan)
		}
	}
	if len(plans) > 0 {
		return plans
	}

	planList := consoleResponseList(
		findTencentConsoleResponse(result, "cmd=ListUserTokenPlans"), "UserTokenPlanList")
	for _, planEntry := range planList {
		if plan := tencentPlanSummary(planEntry); len(plan) > 0 {
			plans = append(plans, plan)
		}
	}
	return plans
}

// tencentPlanSummary extracts the metadata fields both APIs report for one
// plan package. ExpireTime doubles as the monthly cycle reset.
func tencentPlanSummary(planPackage map[string]any) map[string]any {
	plan := map[string]any{}
	if planName, isString := planPackage["Plan"].(string); isString && planName != "" {
		plan["plan"] = planName
		plan["plan_level"] = tencentPlanLevel(planName)
	}
	if startedAt := FormatZAIDatetime(planPackage["StartTime"]); startedAt != nil {
		plan["subscription_started_at"] = startedAt
	}
	if validUntil := FormatZAIDatetime(planPackage["ExpireTime"]); validUntil != nil {
		plan["subscription_valid_until"] = validUntil
	}
	if renewFlag := ToInt(planPackage["RenewFlag"]); renewFlag != nil {
		plan["auto_renew"] = *renewFlag == 1
	}
	return plan
}

// tencentMonthlyQuota builds the monthly credit quota row. Credits arrive
// as decimal strings, so they stay floats rounded to 2 places; the percent
// follows the project-wide remaining ("left") semantics.
func tencentMonthlyQuota(planPackage map[string]any, planResource map[string]any) map[string]any {
	if planResource == nil {
		return nil
	}
	remaining := ToFloat(planResource["CycleRemainCredits"])
	limit := ToFloat(planResource["CycleCapacityCredits"])
	used := ToFloat(planResource["CycleTotalUsageCredits"])
	if remaining == nil && limit == nil && used == nil {
		return nil
	}

	quota := map[string]any{}
	if remaining != nil {
		quota["remaining"] = roundTo2(*remaining)
	}
	if limit != nil {
		quota["limit"] = roundTo2(*limit)
	}
	if used != nil {
		quota["used"] = roundTo2(*used)
	}
	if remaining != nil && limit != nil && *limit > 0 {
		quota["remaining_percent"] = clampPercent(*remaining / *limit * 100)
	}
	if reset := FormatZAIDatetime(planPackage["ExpireTime"]); reset != nil {
		quota["reset"] = reset
	}
	return quota
}

// clampPercent bounds a percentage to [0, 100] and rounds to 2 places.
func clampPercent(value float64) float64 {
	bounded := value
	if bounded < 0 {
		bounded = 0
	}
	if bounded > 100 {
		bounded = 100
	}
	return roundTo2(bounded)
}

// findTencentConsoleResponse unwraps the console gateway envelope
// data.data.Response for one captured API response.
func findTencentConsoleResponse(result map[string]any, urlPart string) map[string]any {
	envelope := findJSONResponse(result, urlPart)
	for range 2 {
		nextLayer, _ := envelope["data"].(map[string]any)
		if nextLayer == nil {
			return nil
		}
		envelope = nextLayer
	}
	response, _ := envelope["Response"].(map[string]any)
	return response
}

// consoleResponseList extracts a list of objects from one key of a console
// Response payload, tolerating shape drift.
func consoleResponseList(response map[string]any, key string) []map[string]any {
	rawList, _ := response[key].([]any)
	entries := make([]map[string]any, 0, len(rawList))
	for _, rawEntry := range rawList {
		if entry, isMap := rawEntry.(map[string]any); isMap {
			entries = append(entries, entry)
		}
	}
	return entries
}

// tencentPlanLevel renders the plan tier name: tp_hy_lite -> "Hy Lite".
func tencentPlanLevel(planName string) string {
	parts := strings.Split(strings.ToLower(planName), "_")
	if len(parts) > 0 && parts[0] == "tp" {
		parts = parts[1:]
	}
	for partIndex, part := range parts {
		if part != "" {
			parts[partIndex] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
