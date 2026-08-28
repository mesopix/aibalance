package aibalance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLiveBigmodelProbe drives the full probe pipeline (CDP connect, page
// navigation, JSON response capture, summarize) against the resident
// automation Chrome. It is skipped unless AIBALANCE_LIVE_CDP points at a
// live CDP endpoint, so normal test runs stay hermetic.
//
// BigModel shares the z.ai response shapes, so this exercises the same
// summarizeZAIWithHost code path that z_ai_coding_plan uses.
func TestLiveBigmodelProbe(t *testing.T) {
	cdpURL := os.Getenv("AIBALANCE_LIVE_CDP")
	if cdpURL == "" {
		t.Skip("set AIBALANCE_LIVE_CDP (e.g. http://127.0.0.1:9222) to run the live probe")
	}

	ctx := context.Background()
	browser, connectErr := connectCDP(ctx, cdpURL)
	if connectErr != nil {
		t.Fatalf("connect CDP: %v", connectErr)
	}

	page, acquireErr := acquireServicePage(browser, "https://bigmodel.cn/coding-plan/personal/usage")
	if acquireErr != nil {
		t.Fatalf("acquire page: %v", acquireErr)
	}

	result := probeWebDashboard(ctx, page, "https://bigmodel.cn/coding-plan/personal/usage", 30_000, 3_000,
		zaiRequiredResponses("bigmodel.cn"), nil)
	summary := summarizeZAIWithHost(result, "bigmodel.cn")

	if summary["status"] != "ok" {
		t.Fatalf("status = %v, want ok; raw result: %#v", summary["status"], result)
	}

	// Structural assertions on stable fields; live numbers may drift.
	if summary["plan_level"] == nil || summary["plan_level"] == "" {
		t.Errorf("plan_level missing: %#v", summary)
	}
	for _, limitKey := range []string{"five_hour", "weekly"} {
		limitEntry, isMap := summary[limitKey].(map[string]any)
		if !isMap {
			t.Errorf("%s missing from summary: %#v", limitKey, summary)
			continue
		}
		if _, hasLimit := limitEntry["limit"]; !hasLimit {
			t.Errorf("%s.limit missing: %#v", limitKey, limitEntry)
		}
		if _, hasReset := limitEntry["reset"]; !hasReset {
			t.Errorf("%s.reset missing: %#v", limitKey, limitEntry)
		}
	}
	if summary["total_tokens"] == nil {
		t.Errorf("total_tokens missing: %#v", summary)
	}

	// Dump the full summary for eyeball comparison against the Python CLI.
	encoded, encodeErr := json.MarshalIndent(summary, "", "  ")
	if encodeErr != nil {
		t.Fatalf("marshal summary: %v", encodeErr)
	}
	t.Logf("live summary:\n%s", encoded)

	// Dump the redacted visible text to diagnose visible-usage parsing.
	if visibleText, isString := result["_visible_text"].(string); isString && visibleText != "" {
		t.Logf("redacted visible text:\n%s", RedactText(visibleText))
	}

	// Dump every captured quota/limit payload to diagnose percentage drift.
	responses, _ := result["json_responses"].([]CapturedJSONResponse)
	for responseIndex, response := range responses {
		if !strings.Contains(response.URL, "quota/limit") {
			continue
		}
		encodedPayload, payloadErr := json.Marshal(response.JSON)
		if payloadErr != nil {
			continue
		}
		t.Logf("quota/limit response [%d]: %s", responseIndex, encodedPayload)
	}
}
