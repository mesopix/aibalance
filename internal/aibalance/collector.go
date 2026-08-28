package aibalance

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// responseURLKeywords mirrors RESPONSE_URL_KEYWORDS in ai_balance.py.
var responseURLKeywords = []string{
	"balance", "billing", "codex", "credit", "credits", "entitlement",
	"invite", "limit", "limits", "monitor", "plan", "promotion", "quota",
	"referral", "reset", "reward", "subscription", "usage",
}

// maxResponseBytes mirrors the 1 MB body cap in JsonResponseCollector.
const maxResponseBytes = 1_000_000

// bodyFetchTimeout bounds one GetResponseBody call. A held-open response
// (e.g. a long-poll whose headers arrived but body never did) would
// otherwise block until the refresh pass context expires.
const bodyFetchTimeout = 5 * time.Second

// CapturedJSONResponse is one entry of the json_responses list, mirroring
// the item shapes produced by JsonResponseCollector in ai_balance.py.
type CapturedJSONResponse struct {
	URL          string         `json:"url"`
	Status       int            `json:"status"`
	JSON         map[string]any `json:"json,omitempty"`
	CaptureError string         `json:"capture_error,omitempty"`
	Skipped      string         `json:"skipped,omitempty"`
	Bytes        int            `json:"bytes,omitempty"`
}

// pendingCapture holds event metadata; the body is fetched later, once
// Chrome has reported the transfer finished.
type pendingCapture struct {
	requestID proto.NetworkRequestID
	url       string
	status    int
}

// responseCollector accumulates JSON API responses from a page.
type responseCollector struct {
	mutex sync.Mutex
	// inFlight holds captures whose headers arrived but whose body is still
	// transferring; GetResponseBody only answers once loading finished.
	inFlight map[proto.NetworkRequestID]pendingCapture
	pending  []pendingCapture
}

// collectResponses attaches a Network response listener to the page.
// Unlike Playwright's page.on(), rod only dispatches events while the wait
// function returned by EachEvent runs, so it is driven in a goroutine.
func collectResponses(page *rod.Page) *responseCollector {
	collector := &responseCollector{inFlight: map[proto.NetworkRequestID]pendingCapture{}}
	listener := page.EachEvent(func(event *proto.NetworkResponseReceived) {
		response := event.Response
		if response == nil || !matchesResponseKeyword(response.URL) {
			return
		}
		if !isJSONContentType(response.Headers) {
			return
		}
		collector.mutex.Lock()
		collector.inFlight[event.RequestID] = pendingCapture{
			requestID: event.RequestID,
			url:       response.URL,
			status:    response.Status,
		}
		collector.mutex.Unlock()
	}, func(event *proto.NetworkLoadingFinished) {
		collector.promote(event.RequestID)
	}, func(event *proto.NetworkLoadingFailed) {
		collector.mutex.Lock()
		delete(collector.inFlight, event.RequestID)
		collector.mutex.Unlock()
	})
	go listener()
	return collector
}

// promote moves a capture to the readable list once Chrome finished loading
// its body.
func (collector *responseCollector) promote(requestID proto.NetworkRequestID) {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	capture, tracked := collector.inFlight[requestID]
	if !tracked {
		return
	}
	delete(collector.inFlight, requestID)
	collector.pending = append(collector.pending, capture)
}

// progress reports how many readable captures exist and whether every URL
// fragment has one. The count lets the caller notice that captures stopped
// arriving, which is the only signal available when a dashboard never calls
// one of the named endpoints.
func (collector *responseCollector) progress(urlParts []string) (captured int, ready bool) {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	for _, urlPart := range urlParts {
		matched := false
		for _, capture := range collector.pending {
			if strings.Contains(capture.url, urlPart) {
				matched = true
				break
			}
		}
		if !matched {
			return len(collector.pending), false
		}
	}
	return len(collector.pending), true
}

// matchesResponseKeyword reports whether the URL contains a tracked keyword.
func matchesResponseKeyword(rawURL string) bool {
	loweredURL := strings.ToLower(rawURL)
	for _, keyword := range responseURLKeywords {
		if strings.Contains(loweredURL, keyword) {
			return true
		}
	}
	return false
}

// isJSONContentType reports whether the headers advertise JSON.
func isJSONContentType(headers proto.NetworkHeaders) bool {
	for headerName, headerValue := range headers {
		if strings.EqualFold(headerName, "Content-Type") &&
			strings.Contains(strings.ToLower(headerValue.Str()), "json") {
			return true
		}
	}
	return false
}

// results fetches response bodies and returns the json_responses list,
// mirroring the capture semantics of JsonResponseCollector.
func (collector *responseCollector) results(page *rod.Page) []CapturedJSONResponse {
	collector.mutex.Lock()
	pending := append([]pendingCapture(nil), collector.pending...)
	collector.mutex.Unlock()

	entries := make([]CapturedJSONResponse, 0, len(pending))
	for _, pendingItem := range pending {
		entry := CapturedJSONResponse{
			URL:    pendingItem.url,
			Status: pendingItem.status,
		}
		bodyResult, bodyErr := proto.NetworkGetResponseBody{
			RequestID: pendingItem.requestID,
		}.Call(page.Timeout(bodyFetchTimeout))
		if bodyErr != nil {
			entry.CaptureError = RedactText(bodyErr.Error())
			entries = append(entries, entry)
			continue
		}
		if bodyResult == nil {
			entry.CaptureError = "<empty response body>"
			entries = append(entries, entry)
			continue
		}
		if len(bodyResult.Body) > maxResponseBytes {
			entry.Skipped = "response_too_large"
			entry.Bytes = len(bodyResult.Body)
			entries = append(entries, entry)
			continue
		}
		var payload map[string]any
		if jsonErr := json.Unmarshal([]byte(bodyResult.Body), &payload); jsonErr != nil || payload == nil {
			// Unparseable bodies are dropped silently, like Python's
			// json.JSONDecodeError return.
			continue
		}
		entry.JSON = payload
		entries = append(entries, entry)
	}
	return entries
}

// findJSONResponse mirrors find_json_response: first captured response
// whose URL contains urlPart and whose body parsed to a JSON object.
func findJSONResponse(result map[string]any, urlPart string) map[string]any {
	responses, _ := result["json_responses"].([]CapturedJSONResponse)
	for _, response := range responses {
		if strings.Contains(response.URL, urlPart) && response.JSON != nil {
			return response.JSON
		}
	}
	return nil
}
