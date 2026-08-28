package aibalance

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func TestParseServicesAll(t *testing.T) {
	services, err := ParseServices([]string{"all"})
	if err != nil {
		t.Fatalf("ParseServices(all) error: %v", err)
	}
	if len(services) != len(ServiceOrder) {
		t.Errorf("got %d services, want %d", len(services), len(ServiceOrder))
	}
}

func TestParseServicesUnknown(t *testing.T) {
	if _, err := ParseServices([]string{"bogus_service"}); err == nil {
		t.Error("ParseServices(bogus_service) should fail")
	}
}

func TestParseServicesDefaultIsAll(t *testing.T) {
	services, err := ParseServices(nil)
	if err != nil {
		t.Fatalf("ParseServices(nil) error: %v", err)
	}
	if len(services) != len(ServiceOrder) {
		t.Errorf("got %d services, want all %d", len(services), len(ServiceOrder))
	}
}

func TestOrderedServicesKeepsCanonicalOrder(t *testing.T) {
	selected := map[string]bool{"deepseek_api": true, "qwen_token_plan": true}
	ordered := OrderedServices(selected)
	if len(ordered) != 2 || ordered[0] != "qwen_token_plan" || ordered[1] != "deepseek_api" {
		t.Errorf("OrderedServices() = %v, want [qwen_token_plan deepseek_api]", ordered)
	}
}

func TestAllServicesRegistered(t *testing.T) {
	for _, serviceName := range ServiceOrder {
		definition, implemented := serviceRegistry[serviceName]
		if !implemented {
			t.Errorf("service %q listed in ServiceOrder but not registered", serviceName)
			continue
		}
		if definition.Run == nil || definition.Summarize == nil {
			t.Errorf("service %q has incomplete definition (Run/Summarize nil)", serviceName)
		}
	}
}

func TestRunEmitsProgressEvents(t *testing.T) {
	withDeepSeekTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(deepseekPayload))
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	var events []string
	emitter := func(event string, payload map[string]any) {
		events = append(events, event)
	}
	if _, runErr := Run(context.Background(), []string{"deepseek_api"}, RunOptions{DeepSeekTimeoutSeconds: 5}, emitter); runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}

	wantEvents := []string{"start", "service_start", "service_finish"}
	if len(events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	for eventIndex, wantEvent := range wantEvents {
		if events[eventIndex] != wantEvent {
			t.Errorf("events[%d] = %q, want %q", eventIndex, events[eventIndex], wantEvent)
		}
	}
}

func TestScrubDebugOutputDropsPrivateKeys(t *testing.T) {
	input := map[string]any{
		"_visible_text": "secret-inner",
		"status":        "ok",
		"nested":        map[string]any{"_private": "x", "public": "y"},
	}
	scrubbed := ScrubDebugOutput(input).(map[string]any)
	if _, exists := scrubbed["_visible_text"]; exists {
		t.Error("_visible_text should be dropped")
	}
	nested := scrubbed["nested"].(map[string]any)
	if _, exists := nested["_private"]; exists {
		t.Error("nested _private should be dropped")
	}
	if nested["public"] != "y" {
		t.Errorf("nested public = %v, want y", nested["public"])
	}
}

func TestFilterSummaryServicesKeepsOnlyNamed(t *testing.T) {
	summary := map[string]any{
		"generated_at": "2026-08-25 10:00:00",
		"accounts": map[string]any{
			"qwen_token_plan":    map[string]any{"status": "ok"},
			"chatgpt_codex":      map[string]any{"status": "ok"},
			"z_ai_coding_plan_2": map[string]any{"status": "ok"},
		},
	}

	filtered := FilterSummaryServices(summary, []string{"qwen_token_plan"})
	if filtered["generated_at"] != "2026-08-25 10:00:00" {
		t.Errorf("generated_at = %v, want passthrough", filtered["generated_at"])
	}
	accounts := filtered["accounts"].(map[string]any)
	if _, exists := accounts["qwen_token_plan"]; !exists {
		t.Error("named service should be kept")
	}
	if _, exists := accounts["chatgpt_codex"]; exists {
		t.Error("unnamed service should be dropped")
	}
	if _, exists := accounts["z_ai_coding_plan_2"]; exists {
		t.Error("unnamed service should be dropped")
	}
}

func TestChromeEndpointsForServices(t *testing.T) {
	testCases := []struct {
		name          string
		services      []string
		wantPrimary   bool
		wantSecondary bool
	}{
		{"chrome-free batch", []string{"deepseek_api"}, false, false},
		{"primary only", []string{"qwen_token_plan", "chatgpt_codex"}, true, false},
		{"secondary only", []string{"z_ai_coding_plan_2"}, false, true},
		{"both endpoints", []string{"z_ai_coding_plan", "z_ai_coding_plan_2"}, true, true},
		{"unknown service", []string{"bogus_service"}, false, false},
	}
	for _, testCase := range testCases {
		primary, secondary := ChromeEndpointsForServices(testCase.services)
		if primary != testCase.wantPrimary || secondary != testCase.wantSecondary {
			t.Errorf("%s: ChromeEndpointsForServices(%v) = (%v, %v), want (%v, %v)",
				testCase.name, testCase.services, primary, secondary,
				testCase.wantPrimary, testCase.wantSecondary)
		}
	}
}

func TestLoginTargetURL(t *testing.T) {
	if got := LoginTargetURL("deepseek_api"); got != "" {
		t.Errorf("LoginTargetURL(deepseek_api) = %q, want empty (API-key service)", got)
	}
	if got := LoginTargetURL("kimi_coding_plan"); got != kimiCodingPlanURL {
		t.Errorf("LoginTargetURL(kimi_coding_plan) = %q, want %q", got, kimiCodingPlanURL)
	}
	if got := LoginTargetURL("chatgpt_codex"); got != codexURLCandidates[0] {
		t.Errorf("LoginTargetURL(chatgpt_codex) = %q, want %q", got, codexURLCandidates[0])
	}
	if got := LoginTargetURL("bogus_service"); got != "" {
		t.Errorf("LoginTargetURL(bogus_service) = %q, want empty", got)
	}
}

// registerFakeService injects one synthetic service and restores the
// registry entry when the test ends. Fake names stay out of ServiceOrder:
// Run only consults the registry, never the canonical order list.
func registerFakeService(t *testing.T, serviceName string, run ServiceRunner) {
	t.Helper()
	previous, existed := serviceRegistry[serviceName]
	serviceRegistry[serviceName] = ServiceDefinition{
		DisplayName: serviceName,
		Run:         run,
		Summarize: func(result map[string]any) map[string]any {
			return map[string]any{"status": result["status"]}
		},
	}
	t.Cleanup(func() {
		if existed {
			serviceRegistry[serviceName] = previous
		} else {
			delete(serviceRegistry, serviceName)
		}
	})
}

func TestRunSequentialKeepsCanonicalEventOrder(t *testing.T) {
	serviceNames := []string{"fake_alpha", "fake_beta", "fake_gamma"}
	for _, serviceName := range serviceNames {
		registerFakeService(t, serviceName, func(ctx context.Context, options RunOptions) map[string]any {
			return map[string]any{"status": "ok", "marker": serviceName}
		})
	}

	var events []string
	emitter := func(event string, payload map[string]any) {
		serviceName, _ := payload["service"].(string)
		if serviceName == "" {
			events = append(events, event)
			return
		}
		events = append(events, event+":"+serviceName)
	}
	output, runErr := Run(context.Background(), serviceNames, RunOptions{}, emitter)
	if runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}

	wantEvents := []string{
		"start",
		"service_start:fake_alpha", "service_finish:fake_alpha",
		"service_start:fake_beta", "service_finish:fake_beta",
		"service_start:fake_gamma", "service_finish:fake_gamma",
	}
	if !slices.Equal(events, wantEvents) {
		t.Errorf("events = %v, want %v", events, wantEvents)
	}
	accounts, _ := output["accounts"].(map[string]any)
	for _, serviceName := range serviceNames {
		account, _ := accounts[serviceName].(map[string]any)
		if account == nil || account["marker"] != serviceName {
			t.Errorf("account %q missing its marker", serviceName)
		}
	}
}

func TestRunSingleServiceReturnsOnlyThatService(t *testing.T) {
	registerFakeService(t, "fake_alpha", func(ctx context.Context, options RunOptions) map[string]any {
		return map[string]any{"status": "ok", "marker": "alpha"}
	})
	registerFakeService(t, "fake_beta", func(ctx context.Context, options RunOptions) map[string]any {
		t.Error("fake_beta ran despite not being selected")
		return map[string]any{"status": "ok"}
	})

	output, runErr := Run(context.Background(), []string{"fake_alpha"}, RunOptions{}, nil)
	if runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}
	accounts, _ := output["accounts"].(map[string]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v, want exactly the selected service", accounts)
	}
	if account, _ := accounts["fake_alpha"].(map[string]any); account == nil || account["marker"] != "alpha" {
		t.Errorf("account fake_alpha = %v, want its marker", accounts["fake_alpha"])
	}
	if output["generated_at"] == nil {
		t.Error("output missing generated_at")
	}
}
