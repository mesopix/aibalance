package aibalance

import (
	"fmt"
)

// ServiceView is the display-ready representation of one service summary,
// mirroring the per-service format_*_lines functions in formatting.py.
// Rendering (characters, colors) is left to the caller.
type ServiceView struct {
	ServiceID string // registry key, e.g. "kimi_coding_plan"; "" for ad-hoc views
	Name      string
	Status    string // OK / NEEDS_LOGIN / SKIPPED / PARTIAL / ERROR
	Detail    string // failure detail when not OK
	Quotas    []QuotaView
	Facts     []string
}

// QuotaView is one quota row: a labeled usage bar with amounts and reset.
type QuotaView struct {
	Label       string
	Remaining   any
	Limit       any
	Used        any
	PercentLeft any // remaining percent; nil means unknown
	Reset       string
	Unlimited   bool
	Detail      string // replaces the bar when set (e.g. unlimited note)
}

// StatusLabel mirrors status_label in formatting.py.
func StatusLabel(result map[string]any) string {
	if result == nil {
		return "SKIPPED"
	}
	switch result["status"] {
	case "ok":
		return "OK"
	case "needs_login":
		return "NEEDS_LOGIN"
	case "skipped":
		return "SKIPPED"
	case "partial":
		return "PARTIAL"
	default:
		return "ERROR"
	}
}

// FormatSummaryViews converts a summary document into display views in
// canonical service order, mirroring format_human_summary's assembly.
func FormatSummaryViews(summary map[string]any) []ServiceView {
	accounts, _ := summary["accounts"].(map[string]any)
	views := make([]ServiceView, 0, len(ServiceOrder))
	for _, serviceName := range ServiceOrder {
		rawAccount, exists := accounts[serviceName]
		if !exists {
			continue
		}
		account, isMap := rawAccount.(map[string]any)
		if !isMap {
			continue
		}
		views = append(views, formatServiceView(serviceName, account))
	}
	return views
}

// formatServiceView dispatches to the per-service view builder.
func formatServiceView(serviceName string, account map[string]any) ServiceView {
	view := ServiceView{
		ServiceID: serviceName,
		Name:      ServiceDisplayName(serviceName),
		Status:    StatusLabel(account),
	}
	if view.Status != "OK" {
		view.Detail = failureDetail(account)
		return view
	}

	switch serviceName {
	case "deepseek_api":
		formatDeepSeekView(&view, account)
	case "kimi_coding_plan":
		formatKimiView(&view, account)
	case "qoder_team_credit":
		formatQoderView(&view, account)
	case "chatgpt_codex":
		formatCodexView(&view, account)
	case "qwen_token_plan":
		formatQwenView(&view, account)
	case "z_ai_coding_plan", "z_ai_coding_plan_2", "bigmodel_coding_plan", "bigmodel_coding_plan_2":
		formatZAIView(&view, account)
	}
	return view
}

// failureDetail mirrors failure_detail in formatting.py.
func failureDetail(result map[string]any) string {
	if result == nil {
		return "not selected"
	}
	if result["reason"] == "missing_env" {
		return "missing DEEPSEEK_API_KEY"
	}
	if result["reason"] == "needs_login" {
		return "run --login-setup"
	}
	if errorText, isString := result["error"].(string); isString && errorText != "" {
		return errorText
	}
	if reasonText, isString := result["reason"].(string); isString && reasonText != "" {
		return reasonText
	}
	return "no data"
}

// quotaViewFromMap builds a QuotaView from a summary quota entry.
func quotaViewFromMap(label string, quota map[string]any) QuotaView {
	reset, _ := quota["reset"].(string)
	return QuotaView{
		Label:       label,
		Remaining:   quota["remaining"],
		Limit:       quota["limit"],
		Used:        quota["used"],
		PercentLeft: quota["remaining_percent"],
		Reset:       reset,
		Unlimited:   quota["unlimited"] == true,
	}
}

// formatDeepSeekView mirrors format_deepseek_lines.
func formatDeepSeekView(view *ServiceView, account map[string]any) {
	balances, _ := account["balances"].([]any)
	if len(balances) == 0 {
		view.Facts = append(view.Facts, "no balance info")
		return
	}
	balance, isMap := balances[0].(map[string]any)
	if !isMap {
		view.Facts = append(view.Facts, "no balance info")
		return
	}
	view.Facts = append(view.Facts, fmt.Sprintf("%v total %v | top-up %v | grant %v",
		balance["currency"], balance["total_balance"],
		balance["topped_up_balance"], balance["granted_balance"]))
}

// formatKimiView builds the Kimi view. The 300-minute window is the 5h
// quota and weekly is the 7d quota; the shorter window renders first.
func formatKimiView(view *ServiceView, account map[string]any) {
	if window, isMap := account["window"].(map[string]any); isMap {
		view.Quotas = append(view.Quotas, quotaViewFromMap("5h", window))
	}
	if weekly, isMap := account["weekly"].(map[string]any); isMap {
		view.Quotas = append(view.Quotas, quotaViewFromMap("7d", weekly))
	}
	if periodEnd, isString := account["current_period_end"].(string); isString && periodEnd != "" {
		view.Facts = append(view.Facts, "period end "+FormatShortTime(periodEnd))
	}
}

// formatQoderView mirrors format_qoder_lines.
func formatQoderView(view *ServiceView, account map[string]any) {
	if planQuota, isMap := account["plan_quota"].(map[string]any); isMap {
		quota := quotaViewFromMap("all", planQuota)
		quota.Reset = stringValue(account["next_reset"])
		view.Quotas = append(view.Quotas, quota)
	}
}

// formatCodexView mirrors format_chatgpt_lines.
func formatCodexView(view *ServiceView, account map[string]any) {
	if weekly, isMap := account["weekly"].(map[string]any); isMap {
		view.Quotas = append(view.Quotas, quotaViewFromMap("7d", weekly))
	}
}

// formatQwenView mirrors format_qwen_token_plan_lines.
func formatQwenView(view *ServiceView, account map[string]any) {
	if fiveHour, isMap := account["five_hour"].(map[string]any); isMap {
		if fiveHour["unlimited"] == true {
			view.Quotas = append(view.Quotas, QuotaView{
				Label:     "5h",
				Unlimited: true,
				Detail:    "unlimited (limited-time offer)",
			})
		} else {
			view.Quotas = append(view.Quotas, quotaViewFromMap("5h", fiveHour))
		}
	}
	if weekly, isMap := account["weekly"].(map[string]any); isMap {
		view.Quotas = append(view.Quotas, quotaViewFromMap("7d", weekly))
	}
	if validUntil := stringValue(account["subscription_valid_until"]); validUntil != "" {
		view.Facts = append(view.Facts, "valid until "+FormatShortTime(validUntil))
	}
}

// formatZAIView builds the z.ai / BigModel view; quota rows go from the
// shortest window up: 5h, 7d, monthly tools.
func formatZAIView(view *ServiceView, account map[string]any) {
	quotaSpecs := []struct {
		key   string
		label string
	}{
		{"five_hour", "5h"},
		{"weekly", "7d"},
		{"monthly_tools", "monthly tools"},
	}
	for _, spec := range quotaSpecs {
		quota, isMap := account[spec.key].(map[string]any)
		if !isMap || len(quota) == 0 {
			continue
		}
		view.Quotas = append(view.Quotas, quotaViewFromMap(spec.label, quota))
	}
	if len(view.Quotas) == 0 {
		if level := account["plan_level"]; level != nil && level != "" {
			view.Facts = append(view.Facts, "level "+trimAny(level))
		} else {
			view.Facts = append(view.Facts, "no quota data")
		}
	}
}

// stringValue renders a value as a string, empty when nil.
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, isString := value.(string); isString {
		return text
	}
	return trimAny(value)
}

// trimAny renders a scalar compactly (no trailing .0 on whole floats).
func trimAny(value any) string {
	switch typedValue := value.(type) {
	case float64:
		return trimFloat(typedValue)
	default:
		return fmt.Sprint(value)
	}
}

// trimFloat drops the fraction of whole floats: 300.0 -> "300".
func trimFloat(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprint(int64(value))
	}
	return fmt.Sprint(value)
}
