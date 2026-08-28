package aibalance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

// deepseekBalanceURL mirrors DEEPSEEK_BALANCE_URL in ai_balance.py. It is a
// variable so tests can point it at a local httptest server.
var deepseekBalanceURL = "https://api.deepseek.com/user/balance"

// runDeepSeekService queries the DeepSeek balance API, mirroring
// query_deepseek_balance in ai_balance.py.
func runDeepSeekService(ctx context.Context, options RunOptions) map[string]any {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return map[string]any{
			"status": "skipped",
			"reason": "missing_env",
			"env":    "DEEPSEEK_API_KEY",
		}
	}

	timeoutSeconds := options.DeepSeekTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	httpClient := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, deepseekBalanceURL, nil)
	if requestErr != nil {
		return map[string]any{
			"status": "error",
			"error":  RedactText(requestErr.Error()),
		}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)

	response, responseErr := httpClient.Do(request)
	if responseErr != nil {
		return map[string]any{
			"status": "error",
			"error":  RedactText(responseErr.Error()),
		}
	}
	defer response.Body.Close()

	bodyBytes, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return map[string]any{
			"status": "error",
			"error":  RedactText(readErr.Error()),
		}
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return map[string]any{
			"status":      "error",
			"http_status": response.StatusCode,
			"error":       RedactText(string(bodyBytes)),
		}
	}

	var payload map[string]any
	if jsonErr := json.Unmarshal(bodyBytes, &payload); jsonErr != nil {
		return map[string]any{
			"status": "error",
			"error":  RedactText(jsonErr.Error()),
		}
	}

	normalizedBalances := []any{}
	balanceInfos, _ := payload["balance_infos"].([]any)
	for _, balanceItem := range balanceInfos {
		balanceInfo, isMap := balanceItem.(map[string]any)
		if !isMap {
			continue
		}
		normalizedBalances = append(normalizedBalances, map[string]any{
			"currency":          balanceInfo["currency"],
			"total_balance":     balanceInfo["total_balance"],
			"granted_balance":   balanceInfo["granted_balance"],
			"topped_up_balance": balanceInfo["topped_up_balance"],
		})
	}

	return map[string]any{
		"status":       "ok",
		"is_available": payload["is_available"],
		"balances":     normalizedBalances,
		"raw":          RedactData(payload, ""),
	}
}

// summarizeDeepSeek mirrors summarize_deepseek in ai_balance.py.
func summarizeDeepSeek(result map[string]any) map[string]any {
	balances, hasBalances := result["balances"]
	if !hasBalances || balances == nil {
		balances = []any{}
	}

	summary := map[string]any{
		"status":       result["status"],
		"is_available": result["is_available"],
		"balances":     balances,
	}
	if errorValue, hasError := result["error"]; hasError {
		summary["error"] = errorValue
	}
	if reasonValue, hasReason := result["reason"]; hasReason {
		summary["reason"] = reasonValue
	}
	return summary
}
