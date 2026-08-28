package aibalance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LatestSummaryPath returns the cached summary path inside the user data
// directory, matching the location the C++ GUI used.
func LatestSummaryPath() string {
	return filepath.Join(UserDataDirectory(), "latest_summary.json")
}

// LoadLatestSummary reads the cached summary document, if present.
func LoadLatestSummary() (map[string]any, error) {
	summaryBytes, readErr := os.ReadFile(LatestSummaryPath())
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil
		}
		return nil, readErr
	}
	var summary map[string]any
	if decodeErr := json.Unmarshal(summaryBytes, &summary); decodeErr != nil {
		return nil, decodeErr
	}
	return summary, nil
}

// SaveLatestSummary writes the summary document atomically (temp file +
// rename) so a crash never leaves a truncated cache behind.
func SaveLatestSummary(summary map[string]any) error {
	encoded, encodeErr := json.MarshalIndent(summary, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	return writeUserDataFile(LatestSummaryPath(), encoded)
}

// GeneratedNow returns a fresh generated_at timestamp in the canonical
// UTC ISO form used by the raw output documents.
func GeneratedNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00")
}
