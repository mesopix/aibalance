package aibalance

import (
	"regexp"
	"strings"
)

// qwenTokenPlanURL mirrors QWEN_TOKEN_PLAN_URL in ai_balance.py.
const qwenTokenPlanURL = "https://bailian.console.aliyun.com/cn-beijing?tab=plan#/efm/subscription/token-plan/personal"

// qwenRequiredResponses are the APIs summarizeQwenTokenPlan reads.
var qwenRequiredResponses = []string{
	"/tokenplan/personal/api/v2/subscription",
	"/tokenplan/personal/api/v2/usage",
}

// qwenPlanPattern matches "Lite/Standard/Pro 套餐/Plan" lines.
var qwenPlanPattern = regexp.MustCompile(`(?i)^(Lite|Standard|Pro)\s*(?:套餐|Plan)$`)

// qwenDigitsPattern extracts the leading digit run of a field value.
var qwenDigitsPattern = regexp.MustCompile(`(\d+)`)

// qwenUsedPercentPattern matches "42.5% 已用/used" lines.
var qwenUsedPercentPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*%\s*(?:已用|used)`)

// qwenResetTimePattern matches "2026-08-28 22:04" style timestamps.
var qwenResetTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?`)

// summarizeQwenTokenPlan mirrors summarize_qwen_token_plan in ai_balance.py.
func summarizeQwenTokenPlan(result map[string]any) map[string]any {
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

	visibleText, _ := result["_visible_text"].(string)
	for fieldName, fieldValue := range parseQwenVisibleUsage(visibleText) {
		summary[fieldName] = fieldValue
	}

	subscriptionData := findQwenTokenPlanData(result, "/tokenplan/personal/api/v2/subscription")
	if subscriptionData != nil {
		if specCode, isString := subscriptionData["specCode"].(string); isString && specCode != "" {
			summary["plan"] = capitalize(specCode)
		}
		if planStatus, isString := subscriptionData["status"].(string); isString && planStatus != "" {
			summary["plan_status"] = planStatus
		}
		if remainingDays := ToInt(subscriptionData["remainingDays"]); remainingDays != nil {
			summary["remaining_days"] = *remainingDays
		}
		if startedAt := FormatEpochMillis(subscriptionData["startTime"]); startedAt != nil {
			summary["subscription_started_at"] = startedAt
		}
		if validUntil := FormatEpochMillis(subscriptionData["endTime"]); validUntil != nil {
			summary["subscription_valid_until"] = validUntil
		}
		switch autoRenew := subscriptionData["autoRenewFlag"].(type) {
		case bool:
			summary["auto_renew"] = autoRenew
		default:
			if autoRenew != nil {
				if value := ToInt(autoRenew); value != nil {
					summary["auto_renew"] = *value == 1
				}
			}
		}
	}

	usageData := findQwenTokenPlanData(result, "/tokenplan/personal/api/v2/usage")
	if fiveHour := qwenQuotaFromAPI(usageData,
		[]string{"per5HoursPercentage", "per5HourPercentage"},
		[]string{"per5HoursResetTime", "per5HourResetTime"}); len(fiveHour) > 0 {
		summary["five_hour"] = fiveHour
	}
	if weekly := qwenQuotaFromAPI(usageData,
		[]string{"per1WeekPercentage", "perWeekPercentage"},
		[]string{"per1WeekResetTime", "perWeekResetTime"}); len(weekly) > 0 {
		summary["weekly"] = weekly
	}

	return summary
}

// findQwenTokenPlanData mirrors find_qwen_token_plan_data: unwraps the
// aliyun gateway envelope payload.data.DataV2.data.data.
func findQwenTokenPlanData(result map[string]any, urlPart string) map[string]any {
	payload := findJSONResponse(result, urlPart)
	if payload == nil {
		return nil
	}
	gatewayData, _ := payload["data"].(map[string]any)
	if gatewayData == nil {
		return nil
	}
	dataV2, _ := gatewayData["DataV2"].(map[string]any)
	if dataV2 == nil {
		return nil
	}
	apiResponse, _ := dataV2["data"].(map[string]any)
	if apiResponse == nil || apiResponse["success"] == false {
		return nil
	}
	responseData, _ := apiResponse["data"].(map[string]any)
	return responseData
}

// qwenUsedPercentFromRatio mirrors qwen_used_percent_from_ratio.
func qwenUsedPercentFromRatio(value any) *float64 {
	ratio := ToFloat(value)
	if ratio == nil {
		return nil
	}
	clamped := *ratio
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 1 {
		clamped = 1
	}
	percent := roundTo2(clamped * 100)
	return &percent
}

// qwenQuotaFromAPI mirrors qwen_quota_from_api.
func qwenQuotaFromAPI(usageData map[string]any, percentageKeys []string, resetKeys []string) map[string]any {
	if usageData == nil {
		return nil
	}
	var percentageValue any
	for _, key := range percentageKeys {
		if value, exists := usageData[key]; exists {
			percentageValue = value
			break
		}
	}
	var resetValue any
	for _, key := range resetKeys {
		if value, exists := usageData[key]; exists {
			resetValue = value
			break
		}
	}

	usedPercent := qwenUsedPercentFromRatio(percentageValue)
	reset := FormatEpochMillis(resetValue)
	if usedPercent == nil && reset == nil {
		return nil
	}

	quota := map[string]any{}
	if usedPercent != nil {
		quota["used_percent"] = *usedPercent
		remaining := roundTo2(100 - *usedPercent)
		quota["remaining_percent"] = remaining
	}
	if reset != nil {
		quota["reset"] = reset
	}
	return quota
}

// parseQwenVisibleUsage mirrors parse_qwen_token_plan_visible_usage.
func parseQwenVisibleUsage(visibleText string) map[string]any {
	lines := make([]string, 0, 32)
	for _, rawLine := range strings.Split(visibleText, "\n") {
		normalized := NormalizeLine(rawLine)
		if normalized != "" {
			lines = append(lines, normalized)
		}
	}
	summary := map[string]any{}

	for _, line := range lines {
		if match := qwenPlanPattern.FindStringSubmatch(line); match != nil {
			summary["plan"] = capitalize(match[1])
			break
		}
	}

	fieldLabels := map[string]string{
		"剩余天数": "remaining_days",
		"开始时间": "subscription_started_at",
		"结束时间": "subscription_valid_until",
		"自动续费": "auto_renew",
	}
	for lineIndex, line := range lines {
		fieldName, isLabel := fieldLabels[line]
		if !isLabel || lineIndex+1 >= len(lines) {
			continue
		}
		fieldValue := lines[lineIndex+1]
		switch fieldName {
		case "remaining_days":
			if match := qwenDigitsPattern.FindStringSubmatch(fieldValue); match != nil {
				if days := ToInt(match[1]); days != nil {
					summary[fieldName] = *days
				}
			}
		case "subscription_started_at", "subscription_valid_until":
			summary[fieldName] = FormatZAIDatetime(fieldValue)
		case "auto_renew":
			switch fieldValue {
			case "已开启", "开启", "On":
				summary[fieldName] = true
			case "未开启", "关闭", "Off":
				summary[fieldName] = false
			}
		}
	}

	quotaLabelToKey := map[string]string{
		"5小时限额":         "five_hour",
		"5 Hours Quota": "five_hour",
		"5-hour limit":  "five_hour",
		"7天限额":          "weekly",
		"7 Days Quota":  "weekly",
		"7-day limit":   "weekly",
	}
	quotaLabels := make(map[string]string, len(quotaLabelToKey))
	for label, key := range quotaLabelToKey {
		quotaLabels[strings.ToLower(label)] = key
	}
	for lineIndex, line := range lines {
		quotaKey, isLabel := quotaLabels[strings.ToLower(line)]
		if !isLabel {
			continue
		}

		quotaEntry := map[string]any{}
		scanEnd := min(len(lines), lineIndex+10)
		for scanIndex := lineIndex + 1; scanIndex < scanEnd; scanIndex++ {
			candidate := lines[scanIndex]
			if _, isNextLabel := quotaLabels[strings.ToLower(candidate)]; isNextLabel {
				break
			}

			loweredCandidate := strings.ToLower(candidate)
			if strings.Contains(candidate, "取消限额") ||
				strings.Contains(loweredCandidate, "unlimited") ||
				strings.Contains(loweredCandidate, "no limit") {
				quotaEntry["unlimited"] = true
				quotaEntry["detail"] = "Limited-time unlimited"
			}

			if usedMatch := qwenUsedPercentPattern.FindStringSubmatch(candidate); usedMatch != nil {
				if usedPercent := ToFloat(usedMatch[1]); usedPercent != nil {
					quotaEntry["used_percent"] = *usedPercent
					remaining := 100 - *usedPercent
					if remaining < 0 {
						remaining = 0
					}
					if remaining > 100 {
						remaining = 100
					}
					quotaEntry["remaining_percent"] = roundTo2(remaining)
				}
			}

			if strings.Contains(candidate, "重置") || strings.Contains(loweredCandidate, "reset") {
				if resetMatch := qwenResetTimePattern.FindString(candidate); resetMatch != "" {
					quotaEntry["reset"] = FormatZAIDatetime(resetMatch)
				}
			}
		}

		if len(quotaEntry) > 0 {
			summary[quotaKey] = quotaEntry
		}
	}

	return summary
}

// capitalize upper-cases the first rune, like Python's str.capitalize().
func capitalize(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
