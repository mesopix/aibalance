package aibalance

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ServiceOrder mirrors SERVICE_ORDER in ai_balance.py and drives both the
// execution order and the account ordering in summarized output.
var ServiceOrder = []string{
	"qwen_token_plan",
	"bigmodel_coding_plan",
	"bigmodel_coding_plan_2",
	"z_ai_coding_plan",
	"z_ai_coding_plan_2",
	"chatgpt_codex",
	"kimi_coding_plan",
	"qoder_team_credit",
	"deepseek_api",
}

// serviceDisplayNames mirrors SERVICE_DISPLAY_NAMES in ai_balance.py.
var serviceDisplayNames = map[string]string{
	"bigmodel_coding_plan":   "BigModel Coding",
	"bigmodel_coding_plan_2": "BigModel Coding #2",
	"deepseek_api":           "DeepSeek API",
	"kimi_coding_plan":       "Kimi Coding",
	"qwen_token_plan":        "Qwen Token Plan",
	"qoder_team_credit":      "Qoder Team",
	"chatgpt_codex":          "ChatGPT Codex",
	"z_ai_coding_plan":       "Z.ai Coding",
	"z_ai_coding_plan_2":     "Z.ai Coding #2",
}

// RunOptions carries CLI settings into service runners.
type RunOptions struct {
	DeepSeekTimeoutSeconds int
	// CDPURL drives the primary automation Chrome (shared web services);
	// CDPURL2 drives the second account Chrome (Z.ai #2, BigModel #2).
	CDPURL    string
	CDPURL2   string
	TimeoutMS int
	WaitMS    int
}

// ProgressEmitter receives streaming progress events; nil disables them.
type ProgressEmitter func(event string, payload map[string]any)

// ServiceRunner scrapes one service and returns its raw result map.
type ServiceRunner func(ctx context.Context, options RunOptions) map[string]any

// ServiceSummarizer reduces a raw result map to the public summary map.
type ServiceSummarizer func(result map[string]any) map[string]any

// Browser endpoint selectors for ServiceDefinition.BrowserEndpoint: which
// CDP Chrome a runner needs, or none for Chrome-free services.
const (
	BrowserEndpointNone      = ""
	BrowserEndpointPrimary   = "primary"
	BrowserEndpointSecondary = "secondary"
)

// ServiceDefinition bundles the runner and summarizer for one service.
type ServiceDefinition struct {
	DisplayName string
	Run         ServiceRunner
	Summarize   ServiceSummarizer
	// BrowserEndpoint is BrowserEndpointPrimary, BrowserEndpointSecondary,
	// or BrowserEndpointNone for services that never touch a CDP Chrome.
	BrowserEndpoint string
	// TargetURL is the dashboard page this service scrapes; it doubles as
	// the page opened for interactive login. Empty for Chrome-free services.
	TargetURL string
}

// serviceRegistry holds every implemented service. Services migrate here
// step by step from ai_balance.py; selecting an unregistered service fails
// loudly so partial coverage is never mistaken for complete data.
var serviceRegistry = map[string]ServiceDefinition{
	"deepseek_api": {
		DisplayName:     "DeepSeek API",
		Run:             runDeepSeekService,
		Summarize:       summarizeDeepSeek,
		BrowserEndpoint: BrowserEndpointNone,
	},
	"z_ai_coding_plan": {
		DisplayName:     "Z.ai Coding",
		Run:             makeWebDashboardRunner(zAIUsageURL, zaiRequiredResponses("api.z.ai"), func(options RunOptions) string { return options.CDPURL }),
		Summarize:       summarizeZAI,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       zAIUsageURL,
	},
	"z_ai_coding_plan_2": {
		DisplayName:     "Z.ai Coding #2",
		Run:             makeWebDashboardRunner(zAIUsageURL, zaiRequiredResponses("api.z.ai"), func(options RunOptions) string { return options.CDPURL2 }),
		Summarize:       summarizeZAI,
		BrowserEndpoint: BrowserEndpointSecondary,
		TargetURL:       zAIUsageURL,
	},
	"bigmodel_coding_plan": {
		DisplayName:     "BigModel Coding",
		Run:             makeWebDashboardRunner(bigmodelUsageURL, zaiRequiredResponses("bigmodel.cn"), func(options RunOptions) string { return options.CDPURL }),
		Summarize:       summarizeBigModel,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       bigmodelUsageURL,
	},
	"bigmodel_coding_plan_2": {
		DisplayName:     "BigModel Coding #2",
		Run:             makeWebDashboardRunner(bigmodelUsageURL, zaiRequiredResponses("bigmodel.cn"), func(options RunOptions) string { return options.CDPURL2 }),
		Summarize:       summarizeBigModel,
		BrowserEndpoint: BrowserEndpointSecondary,
		TargetURL:       bigmodelUsageURL,
	},
	"qwen_token_plan": {
		DisplayName:     "Qwen Token Plan",
		Run:             makeWebDashboardRunner(qwenTokenPlanURL, qwenRequiredResponses, func(options RunOptions) string { return options.CDPURL }),
		Summarize:       summarizeQwenTokenPlan,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       qwenTokenPlanURL,
	},
	"kimi_coding_plan": {
		DisplayName:     "Kimi Coding",
		Run:             makeWebDashboardRunner(kimiCodingPlanURL, kimiRequiredResponses, func(options RunOptions) string { return options.CDPURL }),
		Summarize:       summarizeKimi,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       kimiCodingPlanURL,
	},
	"qoder_team_credit": {
		DisplayName:     "Qoder Team",
		Run:             makeWebDashboardRunner(qoderUsageURL, qoderRequiredResponses, func(options RunOptions) string { return options.CDPURL }),
		Summarize:       summarizeQoder,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       qoderUsageURL,
	},
	"chatgpt_codex": {
		DisplayName:     "ChatGPT Codex",
		Run:             runCodexService,
		Summarize:       summarizeChatGPTCodex,
		BrowserEndpoint: BrowserEndpointPrimary,
		TargetURL:       codexURLCandidates[0],
	},
}

// ServiceDisplayName returns the human-facing name for a service ID.
func ServiceDisplayName(serviceName string) string {
	if displayName, exists := serviceDisplayNames[serviceName]; exists {
		return displayName
	}
	return serviceName
}

// LoginTargetURL returns the page opened for interactive login — the
// dashboard URL, which the site redirects to its login flow when the
// session is gone. Empty for services without a browser dashboard.
func LoginTargetURL(serviceName string) string {
	return serviceRegistry[serviceName].TargetURL
}

// ParseServices mirrors parse_services in ai_balance.py: "all" selects
// every service; unknown names are rejected.
func ParseServices(values []string) ([]string, error) {
	if len(values) == 0 {
		return ServiceOrder, nil
	}

	selected := make(map[string]bool)
	for _, value := range values {
		if value == "all" {
			return ServiceOrder, nil
		}
		if _, known := serviceDisplayNames[value]; !known {
			return nil, fmt.Errorf("unknown service %q", value)
		}
		selected[value] = true
	}
	return OrderedServices(selected), nil
}

// OrderedServices filters ServiceOrder down to the selected set, keeping
// the canonical order.
func OrderedServices(selected map[string]bool) []string {
	ordered := make([]string, 0, len(selected))
	for _, serviceName := range ServiceOrder {
		if selected[serviceName] {
			ordered = append(ordered, serviceName)
		}
	}
	return ordered
}

// Run executes the selected services sequentially and returns the raw
// output map (generated_at + accounts), mirroring the assembly in main()
// of ai_balance.py. The TUI runs each service through its own Run call,
// which is where concurrency now lives.
func Run(ctx context.Context, selectedServices []string, options RunOptions, emitProgress ProgressEmitter) (map[string]any, error) {
	for _, serviceName := range selectedServices {
		if _, implemented := serviceRegistry[serviceName]; !implemented {
			return nil, fmt.Errorf("service %q is not implemented yet", serviceName)
		}
	}
	if emitProgress == nil {
		emitProgress = func(string, map[string]any) {}
	}

	totalServices := len(selectedServices)
	emitProgress("start", map[string]any{"total": totalServices})

	output := map[string]any{
		"generated_at": time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"),
		"accounts":     map[string]any{},
	}
	accounts, _ := output["accounts"].(map[string]any)

	for serviceIndex, serviceName := range selectedServices {
		accounts[serviceName] = runServiceWithProgress(ctx, serviceName,
			serviceIndex, totalServices, options, emitProgress)
	}
	return output, nil
}

// runServiceWithProgress runs one registered service and emits its
// service_start / service_finish events with the payload shapes the
// original sequential loop produced.
func runServiceWithProgress(ctx context.Context, serviceName string, serviceIndex int,
	totalServices int, options RunOptions, emitProgress ProgressEmitter) map[string]any {

	definition := serviceRegistry[serviceName]
	progressPayload := map[string]any{
		"service": serviceName,
		"name":    definition.DisplayName,
		"index":   serviceIndex + 1,
		"total":   totalServices,
	}
	emitProgress("service_start", progressPayload)

	serviceResult := definition.Run(ctx, options)

	finishPayload := map[string]any{
		"service": serviceName,
		"name":    definition.DisplayName,
		"index":   serviceIndex + 1,
		"total":   totalServices,
		"status":  serviceResult["status"],
	}
	if definition.Summarize != nil {
		finishPayload["summary"] = definition.Summarize(serviceResult)
	}
	emitProgress("service_finish", finishPayload)
	return serviceResult
}

// SummarizeOutput reduces a raw output map to the public summary,
// mirroring summarize_output in ai_balance.py.
func SummarizeOutput(output map[string]any) map[string]any {
	accounts, _ := output["accounts"].(map[string]any)
	summaryAccounts := map[string]any{}

	for _, serviceName := range ServiceOrder {
		rawResult, exists := accounts[serviceName]
		if !exists {
			continue
		}
		result, isMap := rawResult.(map[string]any)
		if !isMap {
			continue
		}

		definition, implemented := serviceRegistry[serviceName]
		if implemented && definition.Summarize != nil {
			summaryAccounts[serviceName] = definition.Summarize(result)
		} else {
			summaryAccounts[serviceName] = map[string]any{"status": result["status"]}
		}
	}

	generatedAt := FormatISODatetime(output["generated_at"])
	if generatedAt == nil {
		generatedAt = output["generated_at"]
	}
	return map[string]any{
		"generated_at": generatedAt,
		"accounts":     summaryAccounts,
	}
}

// ChromeEndpointsForServices reports which CDP endpoints the given services
// need, so the launcher only starts Chromes the refresh batch will use.
func ChromeEndpointsForServices(services []string) (primary bool, secondary bool) {
	for _, serviceName := range services {
		definition, implemented := serviceRegistry[serviceName]
		if !implemented {
			continue
		}
		switch definition.BrowserEndpoint {
		case BrowserEndpointPrimary:
			primary = true
		case BrowserEndpointSecondary:
			secondary = true
		}
	}
	return primary, secondary
}

// FilterSummaryServices returns a copy of summary whose accounts keep only
// the named services; the other top-level keys pass through.
func FilterSummaryServices(summary map[string]any, services []string) map[string]any {
	accounts, _ := summary["accounts"].(map[string]any)
	keep := make(map[string]bool, len(services))
	for _, serviceName := range services {
		keep[serviceName] = true
	}
	filteredAccounts := make(map[string]any, len(services))
	for serviceName, account := range accounts {
		if keep[serviceName] {
			filteredAccounts[serviceName] = account
		}
	}
	return map[string]any{
		"generated_at": summary["generated_at"],
		"accounts":     filteredAccounts,
	}
}

// ScrubDebugOutput drops private diagnostic keys (leading underscore) and
// redacts every retained value, mirroring scrub_debug_output.
func ScrubDebugOutput(value any) any {
	return RedactData(dropPrivateKeys(value), "")
}

// dropPrivateKeys recursively removes keys starting with an underscore,
// mirroring _drop_private_keys in ai_balance.py.
func dropPrivateKeys(value any) any {
	switch typedValue := value.(type) {
	case map[string]any:
		dropped := make(map[string]any, len(typedValue))
		for itemKey, itemValue := range typedValue {
			if strings.HasPrefix(itemKey, "_") {
				continue
			}
			dropped[itemKey] = dropPrivateKeys(itemValue)
		}
		return dropped
	case []any:
		dropped := make([]any, len(typedValue))
		for itemIndex, itemValue := range typedValue {
			dropped[itemIndex] = dropPrivateKeys(itemValue)
		}
		return dropped
	default:
		return value
	}
}
