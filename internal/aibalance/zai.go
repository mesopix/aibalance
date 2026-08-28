package aibalance

import (
	"fmt"
	"regexp"
	"strings"
)

// zAIUsageURL mirrors Z_AI_CODING_PLAN_URL in ai_balance.py.
const zAIUsageURL = "https://z.ai/manage-apikey/coding-plan/personal/usage"

// bigmodelUsageURL mirrors BIGMODEL_CODING_PLAN_URL in ai_balance.py.
const bigmodelUsageURL = "https://bigmodel.cn/coding-plan/personal/usage"

// resetUnusedText mirrors RESET_UNUSED_TEXT in ai_balance.py.
const resetUnusedText = "Unused, no reset yet"

// zaiRequiredResponses are the APIs summarizeZAIWithHost reads; the host
// differs between z.ai and BigModel, which share the parser.
func zaiRequiredResponses(apiHost string) []string {
	return []string{
		apiHost + "/api/monitor/usage/quota/limit",
		apiHost + "/api/monitor/usage/model-usage",
		apiHost + "/api/monitor/credit-usage/activity",
		apiHost + "/api/biz/subscription/list",
		"usageType=MODEL",
		"usageType=MCP",
	}
}

// summarizeBigModel reuses the z.ai parser with the bigmodel.cn API host,
// mirroring the summarize_z_ai(api_host="bigmodel.cn") call in Python.
func summarizeBigModel(result map[string]any) map[string]any {
	return summarizeZAIWithHost(result, "bigmodel.cn")
}

// summarizeZAI mirrors summarize_z_ai in ai_balance.py.
func summarizeZAI(result map[string]any) map[string]any {
	return summarizeZAIWithHost(result, "api.z.ai")
}

// summarizeZAIWithHost is summarizeZAI with a configurable API host so the
// BigModel service (same response shapes, bigmodel.cn) can reuse it.
func summarizeZAIWithHost(result map[string]any, apiHost string) map[string]any {
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

	summarizeZAIQuota(result, apiHost, summary)
	summarizeZAIModelUsage(result, apiHost, summary)
	summarizeZAIActivity(result, apiHost, summary)
	summarizeZAICreditUsage(result, summary)
	summarizeZAIMCPUsage(result, summary)
	summarizeZAISubscription(result, apiHost, summary)
	applyZAIVisibleUsage(result, summary)
	annotateZAIUnusedLimits(summary)

	return summary
}

// summarizeZAIQuota parses the quota/limit API into five_hour / weekly /
// monthly_tools entries.
func summarizeZAIQuota(result map[string]any, apiHost string, summary map[string]any) {
	quotaPayload := findJSONResponse(result, apiHost+"/api/monitor/usage/quota/limit")
	quotaData, _ := quotaPayload["data"].(map[string]any)
	if quotaData == nil {
		return
	}
	summary["plan_level"] = quotaData["level"]

	limits, _ := quotaData["limits"].([]any)
	for _, limitItem := range limits {
		limit, isMap := limitItem.(map[string]any)
		if !isMap {
			continue
		}
		limitKey := zAILimitKey(limit)
		if limitKey == "" {
			continue
		}

		usedPercent := ToInt(limit["percentage"])
		entry := map[string]any{
			"used_percent":      deref(usedPercent),
			"reset":             FormatZAIDatetime(limit["nextResetTime"]),
			"remaining_percent": deref(LeftPercentFromUsedPercent(usedPercent)),
		}
		currentValue := ToInt(limit["currentValue"])
		remaining := ToInt(limit["remaining"])
		usage := ToInt(limit["usage"])
		if currentValue != nil {
			entry["current_value"] = *currentValue
			entry["used"] = *currentValue
		}
		if remaining != nil {
			entry["remaining"] = *remaining
		}
		if usage != nil {
			entry["usage"] = *usage
			entry["limit"] = *usage
		}

		usageDetails := []any{}
		details, _ := limit["usageDetails"].([]any)
		for _, detailItem := range details {
			detail, isDetailMap := detailItem.(map[string]any)
			if !isDetailMap {
				continue
			}
			detailUsage := ToInt(detail["usage"])
			detailName, hasName := detail["modelCode"].(string)
			if hasName && detailName != "" && detailUsage != nil {
				usageDetails = append(usageDetails, map[string]any{
					"name": detailName,
					"used": *detailUsage,
				})
			}
		}
		if len(usageDetails) > 0 {
			entry["usage_details"] = usageDetails
		}
		summary[limitKey] = entry
	}
}

// zAILimitKey mirrors z_ai_limit_key in ai_balance.py.
func zAILimitKey(limit map[string]any) string {
	limitType, _ := limit["type"].(string)
	unit := ToInt(limit["unit"])
	isCreditOrTokens := limitType == "CREDIT_LIMIT" || limitType == "TOKENS_LIMIT"
	switch {
	case isCreditOrTokens && unit != nil && *unit == 3:
		return "five_hour"
	case isCreditOrTokens && unit != nil && *unit == 6:
		return "weekly"
	case limitType == "TIME_LIMIT" && unit != nil && *unit == 5:
		return "monthly_tools"
	default:
		return ""
	}
}

// summarizeZAIModelUsage parses the model-usage API.
func summarizeZAIModelUsage(result map[string]any, apiHost string, summary map[string]any) {
	modelUsagePayload := findJSONResponse(result, apiHost+"/api/monitor/usage/model-usage")
	modelUsageData, _ := modelUsagePayload["data"].(map[string]any)
	if modelUsageData == nil {
		return
	}

	totalUsage, _ := modelUsageData["totalUsage"].(map[string]any)
	if totalUsage != nil {
		if totalTokens := ToInt(totalUsage["totalTokensUsage"]); totalTokens != nil {
			summary["total_tokens"] = *totalTokens
		}
		if totalModelCalls := ToInt(totalUsage["totalModelCallCount"]); totalModelCalls != nil {
			summary["total_model_calls"] = *totalModelCalls
		}

		modelSummaries, _ := totalUsage["modelSummaryList"].([]any)
		if modelSummaries == nil {
			modelSummaries, _ = modelUsageData["modelSummaryList"].([]any)
		}
		normalizedModels := []any{}
		for _, modelItem := range modelSummaries {
			model, isMap := modelItem.(map[string]any)
			if !isMap {
				continue
			}
			modelName, hasName := model["modelName"].(string)
			modelTokens := ToInt(model["totalTokens"])
			if hasName && modelName != "" && modelTokens != nil {
				normalizedModels = append(normalizedModels, map[string]any{
					"name":   modelName,
					"tokens": *modelTokens,
				})
			}
		}
		if len(normalizedModels) > 0 {
			summary["model_usage"] = normalizedModels
		}
	}

	timeBuckets, timeOK := modelUsageData["x_time"].([]any)
	if !timeOK {
		timeBuckets, timeOK = modelUsageData["xTime"].([]any)
	}
	tokenBuckets, tokenOK := modelUsageData["tokensUsage"].([]any)
	callBuckets, callOK := modelUsageData["modelCallCount"].([]any)
	if timeOK && tokenOK && callOK {
		dailyUsage := map[string]map[string]any{}
		var dailyOrder []string
		for bucketIndex, timeItem := range timeBuckets {
			bucketLabel := strings.TrimSpace(stringify(timeItem))
			dateLabel := extractZAILabelDate(bucketLabel)
			if dateLabel == "" {
				continue
			}

			dailyEntry, exists := dailyUsage[dateLabel]
			if !exists {
				dailyEntry = map[string]any{"date": dateLabel, "tokens": 0, "calls": 0}
				dailyUsage[dateLabel] = dailyEntry
				dailyOrder = append(dailyOrder, dateLabel)
			}
			if bucketIndex < len(tokenBuckets) {
				if tokens := ToInt(tokenBuckets[bucketIndex]); tokens != nil {
					dailyEntry["tokens"] = dailyEntry["tokens"].(int) + *tokens
				}
			}
			if bucketIndex < len(callBuckets) {
				if calls := ToInt(callBuckets[bucketIndex]); calls != nil {
					dailyEntry["calls"] = dailyEntry["calls"].(int) + *calls
				}
			}
		}
		if len(dailyUsage) > 0 {
			orderedEntries := make([]any, 0, len(dailyOrder))
			for _, dateLabel := range dailyOrder {
				orderedEntries = append(orderedEntries, dailyUsage[dateLabel])
			}
			summary["daily_usage"] = orderedEntries
		}
	}
}

// zAIDatePattern matches the leading YYYY-MM-DD of a bucket label.
var zAIDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// extractZAILabelDate returns the date prefix of a bucket label.
func extractZAILabelDate(label string) string {
	if match := zAIDatePattern.FindString(label); match != "" {
		return match
	}
	return label
}

// summarizeZAIActivity parses the credit-usage activity API.
func summarizeZAIActivity(result map[string]any, apiHost string, summary map[string]any) {
	activityPayload := findJSONResponse(result, apiHost+"/api/monitor/credit-usage/activity")
	activityData, _ := activityPayload["data"].(map[string]any)
	if activityData == nil {
		return
	}

	activitySummary, _ := activityData["summary"].(map[string]any)
	if activitySummary != nil {
		if totalTokens := ToInt(activitySummary["totalTokens"]); totalTokens != nil {
			summary["total_tokens"] = *totalTokens
		}
		if peakTokens := ToInt(activitySummary["peakDailyTokens"]); peakTokens != nil {
			summary["peak_tokens"] = *peakTokens
		}
		if durationMillis := ToInt(activitySummary["totalUsageDurationMs"]); durationMillis != nil {
			summary["total_usage_minutes"] = *durationMillis / 60_000
		}
		if streakDays := ToInt(activitySummary["currentStreakDays"]); streakDays != nil {
			summary["current_streak_days"] = *streakDays
		}
		if longestStreak := ToInt(activitySummary["longestStreakDays"]); longestStreak != nil {
			summary["longest_streak_days"] = *longestStreak
		}
		if peakDate, hasDate := activitySummary["peakDailyTokensDate"]; hasDate && peakDate != nil {
			summary["peak_tokens_date"] = stringify(peakDate)
		}
	}

	activeDays := []any{}
	series, _ := activityData["series"].([]any)
	for _, seriesItem := range series {
		dailyEntry, isMap := seriesItem.(map[string]any)
		if !isMap || dailyEntry["date"] == nil || dailyEntry["date"] == "" {
			continue
		}
		dailyTokens := ToInt(dailyEntry["totalTokens"])
		dailyCredits := ToFloat(dailyEntry["totalCredits"])
		dailyMCPCalls := ToInt(dailyEntry["mcpCalls"])
		tokensValue, creditsValue, callsValue := 0, 0.0, 0
		if dailyTokens != nil {
			tokensValue = *dailyTokens
		}
		if dailyCredits != nil {
			creditsValue = *dailyCredits
		}
		if dailyMCPCalls != nil {
			callsValue = *dailyMCPCalls
		}
		if tokensValue == 0 && creditsValue == 0.0 && callsValue == 0 {
			continue
		}
		activeDays = append(activeDays, map[string]any{
			"date":      stringify(dailyEntry["date"]),
			"tokens":    tokensValue,
			"credits":   creditsValue,
			"mcp_calls": callsValue,
		})
	}
	if len(activeDays) > 0 {
		summary["daily_usage"] = activeDays
	}
}

// summarizeZAICreditUsage parses the usageType=MODEL credit usage API.
func summarizeZAICreditUsage(result map[string]any, summary map[string]any) {
	creditPayload := findJSONResponse(result, "usageType=MODEL")
	creditData, _ := creditPayload["data"].(map[string]any)
	if creditData == nil {
		return
	}

	creditSummary, _ := creditData["summary"].(map[string]any)
	if creditSummary != nil {
		creditField := func(fieldName string) any {
			field, isMap := creditSummary[fieldName].(map[string]any)
			if !isMap {
				return nil
			}
			return field["value"]
		}
		if cacheHitRate := ToFloat(creditField("cacheHitRate")); cacheHitRate != nil {
			summary["cache_hit_percent"] = roundTo2(*cacheHitRate * 100)
		}
		if offPeakRate := ToFloat(creditField("offPeakUsageRate")); offPeakRate != nil {
			summary["off_peak_usage_percent"] = roundTo2(*offPeakRate * 100)
		}
		if totalCredits := ToFloat(creditField("totalCredits")); totalCredits != nil {
			summary["total_credits"] = *totalCredits
		}
		if averageDaily := ToFloat(creditField("averageDailyCredits")); averageDaily != nil {
			summary["average_daily_credits"] = *averageDaily
		}
	}

	creditModelUsage, _ := creditData["modelUsage"].(map[string]any)
	if creditModelUsage != nil {
		creditTotalUsage, _ := creditModelUsage["totalUsage"].(map[string]any)
		if creditTotalUsage != nil {
			if totalTokens := ToInt(creditTotalUsage["totalTokens"]); totalTokens != nil {
				summary["total_tokens"] = *totalTokens
			}
			if totalCredits := ToFloat(creditTotalUsage["totalCredits"]); totalCredits != nil {
				summary["total_credits"] = *totalCredits
			}
		}

		normalizedModels := []any{}
		modelSummaries, _ := creditModelUsage["modelSummaryList"].([]any)
		for _, modelItem := range modelSummaries {
			model, isMap := modelItem.(map[string]any)
			if !isMap {
				continue
			}
			modelName, hasName := model["modelName"].(string)
			if !hasName || modelName == "" {
				continue
			}
			normalizedModels = append(normalizedModels, map[string]any{
				"name":    modelName,
				"tokens":  deref(ToInt(model["totalTokens"])),
				"credits": deref(ToFloat(model["totalCredits"])),
			})
		}
		if len(normalizedModels) > 0 {
			summary["model_usage"] = normalizedModels
		}
	}
}

// summarizeZAIMCPUsage parses the usageType=MCP usage API.
func summarizeZAIMCPUsage(result map[string]any, summary map[string]any) {
	mcpPayload := findJSONResponse(result, "usageType=MCP")
	mcpData, _ := mcpPayload["data"].(map[string]any)
	if mcpData == nil {
		return
	}
	mcpUsage, _ := mcpData["mcpUsage"].(map[string]any)
	mcpTotalUsage, _ := mcpUsage["totalUsage"].(map[string]any)
	if mcpTotalUsage == nil {
		return
	}
	if totalMCPCalls := ToInt(mcpTotalUsage["totalMcpCalls"]); totalMCPCalls != nil {
		summary["total_mcp_calls"] = *totalMCPCalls
	}
	if totalMCPCredits := ToFloat(mcpTotalUsage["totalCredits"]); totalMCPCredits != nil {
		summary["total_mcp_credits"] = *totalMCPCredits
	}
}

// summarizeZAISubscription parses the subscription list API.
func summarizeZAISubscription(result map[string]any, apiHost string, summary map[string]any) {
	subscriptionPayload := findJSONResponse(result, apiHost+"/api/biz/subscription/list")
	entries, _ := subscriptionPayload["data"].([]any)

	for _, entryItem := range entries {
		entry, isMap := entryItem.(map[string]any)
		if !isMap || entry["status"] != "VALID" || entry["inCurrentPeriod"] != true {
			continue
		}

		// nextRenewTime is the current period's end, matching the
		// "Valid to" date on the z.ai page.
		if validUntil := FormatZAIDatetime(entry["nextRenewTime"]); validUntil != nil {
			summary["subscription_valid_until"] = validUntil
		}
		switch autoRenew := entry["autoRenew"].(type) {
		case bool:
			summary["auto_renew"] = autoRenew
		default:
			if autoRenew != nil {
				if value := ToInt(autoRenew); value != nil {
					summary["auto_renew"] = *value == 1
				}
			}
		}
		return
	}
}

// applyZAIVisibleUsage overlays usage parsed from the visible page text.
func applyZAIVisibleUsage(result map[string]any, summary map[string]any) {
	visibleText, _ := result["_visible_text"].(string)
	visibleSummary := parseZAIVisibleUsage(visibleText)

	for _, limitKey := range []string{"five_hour", "weekly", "monthly_tools"} {
		visibleLimit, _ := visibleSummary[limitKey].(map[string]any)
		if len(visibleLimit) == 0 {
			continue
		}
		existingLimit, exists := summary[limitKey].(map[string]any)
		if !exists {
			existingLimit = map[string]any{}
			summary[limitKey] = existingLimit
		}
		// The page percent only fills a gap: BigModel renders the ring as a
		// bare "0" (background tabs never run its count-up animation), which
		// must not clobber the API percentage.
		if usedPercent, has := visibleLimit["used_percent"]; has && usedPercent != nil && existingLimit["used_percent"] == nil {
			existingLimit["used_percent"] = usedPercent
			if percentValue := ToInt(usedPercent); percentValue != nil {
				existingLimit["remaining_percent"] = deref(LeftPercentFromUsedPercent(percentValue))
			}
		}
		if reset, has := visibleLimit["reset"]; has && reset != nil {
			existingLimit["reset"] = reset
		}
	}

	for _, fieldName := range []string{"last_updated", "total_tokens"} {
		if value, has := visibleSummary[fieldName]; has && value != nil {
			summary[fieldName] = value
		}
	}
}

// annotateZAIUnusedLimits adds the friendly note for unused quotas.
func annotateZAIUnusedLimits(summary map[string]any) {
	for _, limitKey := range []string{"five_hour", "weekly", "monthly_tools"} {
		limitEntry, isMap := summary[limitKey].(map[string]any)
		if !isMap {
			continue
		}
		usedPercent, isInt := limitEntry["used_percent"].(int)
		if isInt && usedPercent == 0 && limitEntry["reset"] == nil {
			limitEntry["reset"] = resetUnusedText
		}
	}
}

// parseZAIVisibleUsage mirrors parse_z_ai_visible_usage in ai_balance.py.
func parseZAIVisibleUsage(visibleText string) map[string]any {
	lines := make([]string, 0, 32)
	for _, rawLine := range strings.Split(visibleText, "\n") {
		normalized := NormalizeLine(rawLine)
		if normalized != "" {
			lines = append(lines, normalized)
		}
	}
	summary := map[string]any{}

	labelToKey := map[string]string{
		"5 Hours Quota": "five_hour",
		"5小时额度":         "five_hour",
		"Weekly Quota":  "weekly",
		"周额度":           "weekly",
		"Total Monthly Web Search / Reader / Zread Quota": "monthly_tools",
	}
	labels := map[string]bool{}
	for label := range labelToKey {
		labels[label] = true
	}

	for lineIndex, line := range lines {
		limitKey, isLabel := labelToKey[line]
		if !isLabel {
			continue
		}

		entry := map[string]any{}
		scanEnd := min(len(lines), lineIndex+8)
		for scanIndex := lineIndex + 1; scanIndex < scanEnd; scanIndex++ {
			if labels[lines[scanIndex]] {
				break
			}
			// Percent lines are whole numbers; a fractional ring value like
			// "0.86" would truncate to 0, so skip lines with a decimal point.
			if strings.Contains(lines[scanIndex], ".") {
				continue
			}
			if percentValue := ToInt(lines[scanIndex]); percentValue != nil {
				entry["used_percent"] = *percentValue
				break
			}
		}

		resetEnd := min(len(lines), lineIndex+14)
		for scanIndex := lineIndex + 1; scanIndex < resetEnd; scanIndex++ {
			if labels[lines[scanIndex]] {
				break
			}
			if resetText, matched := matchZAIResetLine(lines[scanIndex]); matched {
				entry["reset"] = FormatZAIDatetime(resetText)
				break
			}
		}

		summary[limitKey] = entry
	}

	for lineIndex, line := range lines {
		if updatedText, matched := matchZAILastUpdatedLine(line); matched {
			summary["last_updated"] = FormatZAIDatetime(strings.ReplaceAll(updatedText, ".", "-"))
		}
		if line == "Total Tokens" && lineIndex+1 < len(lines) {
			if totalTokens := ToInt(lines[lineIndex+1]); totalTokens != nil {
				summary["total_tokens"] = *totalTokens
			}
		}
	}

	return summary
}

// zAIResetPattern matches "Reset Time: ..." and its Chinese variant.
var zAIResetPattern = regexp.MustCompile(`(?i)^(?:Reset Time|重置时间)[:：]\s*(.+)$`)

// matchZAIResetLine extracts the reset timestamp from a line.
func matchZAIResetLine(line string) (string, bool) {
	match := zAIResetPattern.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// zAILastUpdatedPattern matches "Last Updated: ..." and its Chinese variant.
var zAILastUpdatedPattern = regexp.MustCompile(`(?i)^(?:Last Updated|最近刷新时间)[:：]\s*(.+)$`)

// matchZAILastUpdatedLine extracts the last-updated timestamp from a line.
func matchZAILastUpdatedLine(line string) (string, bool) {
	match := zAILastUpdatedPattern.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// deref returns the pointed-to value or nil for a nil pointer.
func deref[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

// stringify renders a scalar the way Python's str() would.
func stringify(value any) string {
	switch typedValue := value.(type) {
	case string:
		return typedValue
	case nil:
		return ""
	default:
		return fmt.Sprint(typedValue)
	}
}
