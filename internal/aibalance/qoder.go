package aibalance

// qoderUsageURL mirrors QODER_USAGE_URL in ai_balance.py.
const qoderUsageURL = "https://qoder.com/account/usage"

// qoderRequiredResponses are the APIs summarizeQoder reads.
var qoderRequiredResponses = []string{
	"/api/v2/me/usages/big_model_credits",
	"/api/v1/me/userplan",
	"/api/v1/organizations/",
}

// summarizeQoder mirrors summarize_qoder in ai_balance.py.
func summarizeQoder(result map[string]any) map[string]any {
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

	creditsPayload := findJSONResponse(result, "/api/v2/me/usages/big_model_credits")
	if creditsPayload == nil {
		creditsPayload = map[string]any{}
	}
	totalQuota, _ := creditsPayload["total_quota"].(map[string]any)
	if totalQuota == nil {
		totalQuota, _ = creditsPayload["plan_quota"].(map[string]any)
	}
	if quotaSummary, isMap := totalQuota["quota_summary"].(map[string]any); isMap {
		quotaLimit := ToInt(quotaSummary["limit_value"])
		quotaUsed := ToInt(quotaSummary["used_value"])
		quotaRemaining := ToInt(quotaSummary["remaining_value"])
		quotaUsedPercent := ToInt(quotaSummary["usage_percentage"])
		quotaRemainingPercent := Percent(quotaRemaining, quotaLimit)
		if quotaRemainingPercent == nil {
			quotaRemainingPercent = LeftPercentFromUsedPercent(quotaUsedPercent)
		}

		summary["plan_quota"] = map[string]any{
			"limit":             deref(quotaLimit),
			"used":              deref(quotaUsed),
			"remaining":         deref(quotaRemaining),
			"used_percent":      deref(quotaUsedPercent),
			"remaining_percent": deref(quotaRemainingPercent),
			"unit":              quotaSummary["unit"],
		}
	}

	if resourcePackage, isMap := creditsPayload["resource_package_quota"].(map[string]any); isMap {
		if resourceSummary, isSummaryMap := resourcePackage["quota_summary"].(map[string]any); isSummaryMap {
			addOnLimit := ToInt(resourceSummary["limit_value"])
			addOnUsed := ToInt(resourceSummary["used_value"])
			addOnRemaining := ToInt(resourceSummary["remaining_value"])
			addOnUsedPercent := ToInt(resourceSummary["usage_percentage"])
			addOnRemainingPercent := Percent(addOnRemaining, addOnLimit)
			if addOnRemainingPercent == nil {
				addOnRemainingPercent = LeftPercentFromUsedPercent(addOnUsedPercent)
			}

			summary["add_on_credits"] = map[string]any{
				"limit":             deref(addOnLimit),
				"used":              deref(addOnUsed),
				"remaining":         deref(addOnRemaining),
				"used_percent":      deref(addOnUsedPercent),
				"remaining_percent": deref(addOnRemainingPercent),
			}
		}
	}

	if userPlan := findJSONResponse(result, "/api/v1/me/userplan"); userPlan != nil {
		if planTier := userPlan["plan_tier_name"]; planTier != nil && planTier != "" {
			summary["plan"] = planTier
		} else if planTier = userPlan["plan_tier"]; planTier != nil && planTier != "" {
			summary["plan"] = planTier
		}
		if nextReset := FormatEpochMillis(userPlan["next_refresh_date"]); nextReset != nil {
			summary["next_reset"] = nextReset
		}
	}

	if organizationPlan := findJSONResponse(result, "/api/v1/organizations/"); organizationPlan != nil {
		summary["team_seats"] = map[string]any{
			"used":  organizationPlan["seat_count_used"],
			"limit": organizationPlan["seat_count"],
		}
	}

	return summary
}
