package aibalance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// deepseekPayload mirrors the live DeepSeek balance API response shape.
const deepseekPayload = `{
  "is_available": true,
  "balance_infos": [
    {
      "currency": "CNY",
      "total_balance": "6.59",
      "granted_balance": "0.00",
      "topped_up_balance": "6.59"
    }
  ]
}`

// withDeepSeekTestServer points the service at a local test server and
// restores the real URL afterwards.
func withDeepSeekTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	originalURL := deepseekBalanceURL
	deepseekBalanceURL = server.URL
	t.Cleanup(func() {
		deepseekBalanceURL = originalURL
		server.Close()
	})
}

func TestRunDeepSeekServiceSuccessSchema(t *testing.T) {
	withDeepSeekTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(deepseekPayload))
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	result := runDeepSeekService(context.Background(), RunOptions{DeepSeekTimeoutSeconds: 5})
	if result["status"] != "ok" {
		t.Fatalf("status = %v, want ok; full result: %v", result["status"], result)
	}
	if result["is_available"] != true {
		t.Errorf("is_available = %v, want true", result["is_available"])
	}
	balances, isList := result["balances"].([]any)
	if !isList || len(balances) != 1 {
		t.Fatalf("balances = %#v, want one entry", result["balances"])
	}
	balance := balances[0].(map[string]any)
	if balance["currency"] != "CNY" || balance["total_balance"] != "6.59" {
		t.Errorf("balance = %#v, want CNY 6.59", balance)
	}
	if _, hasRaw := result["raw"]; !hasRaw {
		t.Error("raw payload missing from result")
	}
}

// TestSummarizeDeepSeekMatchesPython pins the summarized schema against the
// real Python CLI output captured from the live DeepSeek API.
func TestSummarizeDeepSeekMatchesPython(t *testing.T) {
	withDeepSeekTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(deepseekPayload))
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	output, runErr := Run(context.Background(), []string{"deepseek_api"}, RunOptions{DeepSeekTimeoutSeconds: 5}, nil)
	if runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}
	summary := SummarizeOutput(output)

	encoded, encodeErr := json.Marshal(summary["accounts"])
	if encodeErr != nil {
		t.Fatalf("marshal accounts: %v", encodeErr)
	}
	wantAccounts := `{"deepseek_api":{"balances":[{"currency":"CNY","granted_balance":"0.00","topped_up_balance":"6.59","total_balance":"6.59"}],"is_available":true,"status":"ok"}}`
	if string(encoded) != wantAccounts {
		t.Errorf("accounts JSON:\n got %s\nwant %s", encoded, wantAccounts)
	}

	if _, hasGeneratedAt := summary["generated_at"]; !hasGeneratedAt {
		t.Error("summary missing generated_at")
	}
}

func TestRunDeepSeekServiceMissingKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	result := runDeepSeekService(context.Background(), RunOptions{})
	want := map[string]any{
		"status": "skipped",
		"reason": "missing_env",
		"env":    "DEEPSEEK_API_KEY",
	}
	encodedGot, _ := json.Marshal(result)
	encodedWant, _ := json.Marshal(want)
	if string(encodedGot) != string(encodedWant) {
		t.Errorf("result = %s, want %s", encodedGot, encodedWant)
	}
}

func TestRunDeepSeekServiceHTTPError(t *testing.T) {
	withDeepSeekTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		writer.Write([]byte(`{"error": "Authentication Fails for key sk-abcdefghijklmnop"}`))
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	result := runDeepSeekService(context.Background(), RunOptions{DeepSeekTimeoutSeconds: 5})
	if result["status"] != "error" {
		t.Fatalf("status = %v, want error", result["status"])
	}
	if result["http_status"] != http.StatusUnauthorized {
		t.Errorf("http_status = %v, want 401", result["http_status"])
	}
	errorText, _ := result["error"].(string)
	if errorText == "" || errorText == `{"error": "Authentication Fails for key sk-abcdefghijklmnop"}` {
		t.Errorf("error body not redacted: %q", errorText)
	}
}

func TestRunDeepSeekServiceInvalidJSON(t *testing.T) {
	withDeepSeekTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("not json"))
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	result := runDeepSeekService(context.Background(), RunOptions{DeepSeekTimeoutSeconds: 5})
	if result["status"] != "error" {
		t.Fatalf("status = %v, want error; result: %v", result["status"], result)
	}
}
