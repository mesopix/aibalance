package aibalance

// kimiCodingPlanURL mirrors KIMI_CODING_PLAN_URL in ai_balance.py.
const kimiCodingPlanURL = "https://www.kimi.com/code/console"

// kimiRequiredResponses are the APIs summarizeKimi reads.
var kimiRequiredResponses = []string{
	"MembershipService/GetSubscription",
	"BillingService/GetUsages",
}

// summarizeKimi mirrors summarize_kimi in ai_balance.py.
func summarizeKimi(result map[string]any) map[string]any {
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

	subscriptionPayload := findJSONResponse(result, "MembershipService/GetSubscription")
	if subscriptionPayload != nil {
		subscription, _ := subscriptionPayload["subscription"].(map[string]any)
		if subscription == nil {
			subscription, _ = subscriptionPayload["purchaseSubscription"].(map[string]any)
		}
		if goods, isMap := subscription["goods"].(map[string]any); isMap {
			summary["plan"] = goods["title"]
		}
		summary["subscription_status"] = subscription["status"]
		if currentEnd := FormatISODatetime(subscription["currentEndTime"]); currentEnd != nil {
			summary["current_period_end"] = currentEnd
		}
	}

	usagePayload := findJSONResponse(result, "BillingService/GetUsages")
	var usage map[string]any
	if usages, isList := usagePayload["usages"].([]any); isList {
		for _, usageItem := range usages {
			usageEntry, isMap := usageItem.(map[string]any)
			if isMap && usageEntry["scope"] == "FEATURE_CODING" {
				usage = usageEntry
				break
			}
		}
	}

	if usage != nil {
		detail, _ := usage["detail"].(map[string]any)
		weeklyLimit := ToInt(detail["limit"])
		weeklyUsed := ToInt(detail["used"])
		weeklyRemaining := ToInt(detail["remaining"])
		if weeklyRemaining == nil {
			weeklyRemaining = RemainingFromLimitAndUsed(weeklyLimit, weeklyUsed)
		}
		summary["weekly"] = map[string]any{
			"limit":             deref(weeklyLimit),
			"used":              deref(weeklyUsed),
			"remaining":         deref(weeklyRemaining),
			"remaining_percent": deref(Percent(weeklyRemaining, weeklyLimit)),
			"reset":             FormatISODatetime(detail["resetTime"]),
		}

		if limits, isList := usage["limits"].([]any); isList && len(limits) > 0 {
			if limitEntry, isMap := limits[0].(map[string]any); isMap {
				window, _ := limitEntry["window"].(map[string]any)
				windowDetail, _ := limitEntry["detail"].(map[string]any)
				windowLimit := ToInt(windowDetail["limit"])
				windowUsed := ToInt(windowDetail["used"])
				windowRemaining := ToInt(windowDetail["remaining"])
				if windowRemaining == nil {
					windowRemaining = RemainingFromLimitAndUsed(windowLimit, windowUsed)
				}
				summary["window"] = map[string]any{
					"duration_minutes":  window["duration"],
					"limit":             deref(windowLimit),
					"used":              deref(windowUsed),
					"remaining":         deref(windowRemaining),
					"remaining_percent": deref(Percent(windowRemaining, windowLimit)),
					"reset":             FormatISODatetime(windowDetail["resetTime"]),
				}
			}
		}
	}

	if totalQuota, isMap := usagePayload["totalQuota"].(map[string]any); isMap {
		totalLimit := ToInt(totalQuota["limit"])
		totalUsed := ToInt(totalQuota["used"])
		totalRemaining := ToInt(totalQuota["remaining"])
		if totalRemaining == nil {
			totalRemaining = RemainingFromLimitAndUsed(totalLimit, totalUsed)
		}
		summary["total_quota"] = map[string]any{
			"limit":             deref(totalLimit),
			"used":              deref(totalUsed),
			"remaining":         deref(totalRemaining),
			"remaining_percent": deref(Percent(totalRemaining, totalLimit)),
		}
	}

	return summary
}
