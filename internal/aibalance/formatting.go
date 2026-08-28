package aibalance

import (
	"strings"
	"time"
)

// shanghaiZone mirrors shanghai_zone in formatting.py (UTC+8, named CST).
var shanghaiZone = time.FixedZone("CST", 8*3600)

// shanghaiLayout is the human-facing timestamp format used everywhere.
const shanghaiLayout = "2006-01-02 15:04 CST"

// zoneAwareLayouts carry an explicit UTC offset; parsed offsets win.
var zoneAwareLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
}

// naiveLayouts have no offset; Python's fromisoformat treats them as UTC.
var naiveLayouts = []string{
	"2006-01-02T15:04:05.999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// FormatISODatetime parses an ISO datetime string and renders it in
// Shanghai time, mirroring format_iso_datetime in formatting.py.
// Returns nil when the value is missing or unparseable.
func FormatISODatetime(value any) any {
	text, isString := value.(string)
	if !isString || text == "" {
		return nil
	}

	normalizedValue := strings.ReplaceAll(text, "Z", "+00:00")
	for _, layout := range zoneAwareLayouts {
		if parsed, parseErr := time.Parse(layout, normalizedValue); parseErr == nil {
			return parsed.In(shanghaiZone).Format(shanghaiLayout)
		}
	}
	for _, layout := range naiveLayouts {
		if parsed, parseErr := time.Parse(layout, normalizedValue); parseErr == nil {
			return parsed.UTC().In(shanghaiZone).Format(shanghaiLayout)
		}
	}
	return nil
}

// FormatShortTime compacts a timestamp for card display: "today 22:56"
// on the current day, "08-28 22:04" within the year, date-only across
// years; unparseable text is returned unchanged.
func FormatShortTime(value string) string {
	parsed, isTime := ParseCachedTimestamp(value)
	if !isTime {
		return value
	}
	now := time.Now().In(parsed.Location())
	switch {
	case parsed.Format("2006-01-02") == now.Format("2006-01-02"):
		return parsed.Format("today 15:04")
	case parsed.Year() == now.Year():
		return parsed.Format("01-02 15:04")
	default:
		return parsed.Format("2006-01-02")
	}
}

// ParseCachedTimestamp parses a summary generated_at value back into a
// time.Time: the Shanghai display form written by SummarizeOutput, or a raw
// ISO string. Returns false when the value is missing or unparseable.
func ParseCachedTimestamp(value any) (time.Time, bool) {
	text, isString := value.(string)
	if !isString || text == "" {
		return time.Time{}, false
	}
	if parsed, parseErr := time.ParseInLocation(shanghaiLayout, text, shanghaiZone); parseErr == nil {
		return parsed, true
	}
	normalized := strings.ReplaceAll(text, "Z", "+00:00")
	for _, layout := range zoneAwareLayouts {
		if parsed, parseErr := time.Parse(layout, normalized); parseErr == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// FormatEpochMillis renders epoch milliseconds in Shanghai time, mirroring
// format_epoch_millis in formatting.py. Returns nil for invalid input.
func FormatEpochMillis(value any) any {
	var epochMillis float64
	switch typedValue := value.(type) {
	case float64:
		epochMillis = typedValue
	case int64:
		epochMillis = float64(typedValue)
	case int:
		epochMillis = float64(typedValue)
	default:
		return nil
	}
	if epochMillis <= 0 {
		return nil
	}
	return time.UnixMilli(int64(epochMillis)).UTC().In(shanghaiZone).Format(shanghaiLayout)
}

// FormatZAIDatetime mirrors format_z_ai_datetime: epoch millis first, then
// naive Shanghai-local layouts, then ISO, then the raw string.
func FormatZAIDatetime(value any) any {
	if formatted := FormatEpochMillis(value); formatted != nil {
		return formatted
	}

	text, isString := value.(string)
	if !isString || strings.TrimSpace(text) == "" {
		return nil
	}

	cleaned := strings.TrimSpace(text)
	for _, layout := range zAINaiveLayouts {
		if parsed, parseErr := time.ParseInLocation(layout, cleaned, shanghaiZone); parseErr == nil {
			return parsed.In(shanghaiZone).Format(shanghaiLayout)
		}
	}

	if formatted := FormatISODatetime(cleaned); formatted != nil {
		return formatted
	}
	return cleaned
}

// zAINaiveLayouts are the z.ai page/API date formats without offsets.
var zAINaiveLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}
