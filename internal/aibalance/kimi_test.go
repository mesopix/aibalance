package aibalance

import (
	"encoding/json"
	"os"
	"testing"
)

// convertRawJSONResponses converts a JSON-decoded json_responses list
// ([]any of maps) into the native []CapturedJSONResponse form.
func convertRawJSONResponses(raw any) []CapturedJSONResponse {
	rawList, _ := raw.([]any)
	converted := make([]CapturedJSONResponse, 0, len(rawList))
	for _, rawItem := range rawList {
		item, _ := rawItem.(map[string]any)
		responseURL, _ := item["url"].(string)
		payload, _ := item["json"].(map[string]any)
		converted = append(converted, CapturedJSONResponse{
			URL:  responseURL,
			JSON: payload,
		})
	}
	return converted
}

// TestSummarizeKimiAgainstPythonDebug feeds a raw probe result captured by
// the Python CLI into the Go summarizer and compares against the Python
// summarize_kimi output for the same input. Skipped unless KIMI_DEBUG_JSON
// points at a Python --debug capture.
func TestSummarizeKimiAgainstPythonDebug(t *testing.T) {
	debugPath := os.Getenv("KIMI_DEBUG_JSON")
	if debugPath == "" {
		t.Skip("set KIMI_DEBUG_JSON to a Python --debug capture to run")
	}

	debugBytes, readErr := os.ReadFile(debugPath)
	if readErr != nil {
		t.Fatalf("read debug capture: %v", readErr)
	}
	var debugDocument map[string]any
	if decodeErr := json.Unmarshal(debugBytes, &debugDocument); decodeErr != nil {
		t.Fatalf("decode debug capture: %v", decodeErr)
	}
	accounts, _ := debugDocument["accounts"].(map[string]any)
	rawResult, _ := accounts["kimi_coding_plan"].(map[string]any)
	if rawResult == nil {
		t.Fatal("capture has no kimi_coding_plan account")
	}

	result := map[string]any{}
	for resultKey, resultValue := range rawResult {
		result[resultKey] = resultValue
	}
	result["json_responses"] = convertRawJSONResponses(rawResult["json_responses"])

	summary := summarizeKimi(result)
	encoded, encodeErr := json.MarshalIndent(summary, "", "  ")
	if encodeErr != nil {
		t.Fatalf("marshal summary: %v", encodeErr)
	}
	t.Logf("go summary:\n%s", encoded)

	// Structural assertions on the fields Python produced for this input.
	if summary["plan"] != "Allegretto" {
		t.Errorf("plan = %v, want Allegretto", summary["plan"])
	}
	if summary["subscription_status"] != "SUBSCRIPTION_STATUS_ACTIVE" {
		t.Errorf("subscription_status = %v, want SUBSCRIPTION_STATUS_ACTIVE", summary["subscription_status"])
	}
	if summary["current_period_end"] != "2026-09-11 08:00 CST" {
		t.Errorf("current_period_end = %v, want 2026-09-11 08:00 CST", summary["current_period_end"])
	}
	window, isMap := summary["window"].(map[string]any)
	if !isMap {
		t.Fatalf("window missing: %#v", summary)
	}
	if window["duration_minutes"] != float64(300) {
		t.Errorf("window duration_minutes = %#v, want 300", window["duration_minutes"])
	}
	if window["limit"] != 100 || window["remaining"] != 100 {
		t.Errorf("window = %#v, want limit=100 remaining=100", window)
	}
	weekly, isMap := summary["weekly"].(map[string]any)
	if !isMap {
		t.Fatalf("weekly missing: %#v", summary)
	}
	if weekly["limit"] != 100 || weekly["used"] != 13 || weekly["remaining"] != 87 {
		t.Errorf("weekly = %#v, want limit=100 used=13 remaining=87", weekly)
	}
	if weekly["reset"] != "2026-09-01 13:56 CST" {
		t.Errorf("weekly reset = %v, want 2026-09-01 13:56 CST", weekly["reset"])
	}
	totalQuota, isMap := summary["total_quota"].(map[string]any)
	if !isMap {
		t.Fatalf("total_quota missing: %#v", summary)
	}
	if totalQuota["limit"] != 100 || totalQuota["used"] != 49 || totalQuota["remaining"] != 51 {
		t.Errorf("total_quota = %#v, want limit=100 used=49 remaining=51", totalQuota)
	}
}
