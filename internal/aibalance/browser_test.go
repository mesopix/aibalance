package aibalance

import "testing"

// TestURLOrigin covers the origin extraction used to claim persistent
// service tabs: scheme and host must match, non-web URLs claim nothing.
func TestURLOrigin(t *testing.T) {
	cases := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{"plain https", "https://z.ai/usage", "https://z.ai"},
		{"with port", "http://127.0.0.1:9222/json/list", "http://127.0.0.1:9222"},
		{"query and hash", "https://bigmodel.cn/coding-plan?a=1#top", "https://bigmodel.cn"},
		{"about blank", "about:blank", ""},
		{"chrome internal", "chrome://newtab/", ""},
		{"empty", "", ""},
	}
	for _, testCase := range cases {
		if actual := urlOrigin(testCase.rawURL); actual != testCase.expected {
			t.Errorf("%s: urlOrigin(%q) = %q, want %q", testCase.name, testCase.rawURL, actual, testCase.expected)
		}
	}
}

// TestSameDocumentTarget covers the reload decision: equal scheme, host,
// and path count as the same document regardless of query or hash, while
// any path difference requires a real navigation.
func TestSameDocumentTarget(t *testing.T) {
	sameCases := []struct {
		name       string
		currentURL string
		targetURL  string
	}{
		{"identical", "https://z.ai/usage", "https://z.ai/usage"},
		{"added query", "https://z.ai/usage?tab=quota", "https://z.ai/usage"},
		{"added hash", "https://z.ai/usage#limits", "https://z.ai/usage"},
	}
	for _, testCase := range sameCases {
		if !sameDocumentTarget(testCase.currentURL, testCase.targetURL) {
			t.Errorf("%s: sameDocumentTarget(%q, %q) = false, want true",
				testCase.name, testCase.currentURL, testCase.targetURL)
		}
	}

	differentCases := []struct {
		name       string
		currentURL string
		targetURL  string
	}{
		{"different path", "https://z.ai/settings", "https://z.ai/usage"},
		{"trailing slash", "https://z.ai/usage/", "https://z.ai/usage"},
		{"different host", "https://bigmodel.cn/usage", "https://z.ai/usage"},
		{"different scheme", "http://z.ai/usage", "https://z.ai/usage"},
		{"blank page", "about:blank", "https://z.ai/usage"},
		{"codex candidates", "https://chatgpt.com/codex/usage", "https://chatgpt.com/codex/cloud"},
	}
	for _, testCase := range differentCases {
		if sameDocumentTarget(testCase.currentURL, testCase.targetURL) {
			t.Errorf("%s: sameDocumentTarget(%q, %q) = true, want false",
				testCase.name, testCase.currentURL, testCase.targetURL)
		}
	}
}
