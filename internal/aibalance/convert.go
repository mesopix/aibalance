package aibalance

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// roundTo2 mirrors Python's round(value, 2).
func roundTo2(value float64) float64 {
	return math.Round(value*100) / 100
}

// The conversion helpers below mirror utils.py: lenient parsing that
// accepts numbers, numeric strings, and thousands separators, returning
// nil for anything unparsable (including booleans, like Python's
// int(float(str(True))) failure path).

// ToInt mirrors to_int in utils.py.
func ToInt(value any) *int {
	parsedFloat := toFloatValue(value)
	if parsedFloat == nil {
		return nil
	}
	parsedInt := int(*parsedFloat)
	return &parsedInt
}

// ToFloat mirrors to_float in utils.py.
func ToFloat(value any) *float64 {
	parsedFloat := toFloatValue(value)
	if parsedFloat == nil {
		return nil
	}
	return parsedFloat
}

// toFloatValue implements the shared lenient float parsing.
func toFloatValue(value any) *float64 {
	var normalized string
	switch typedValue := value.(type) {
	case float64:
		return &typedValue
	case int:
		converted := float64(typedValue)
		return &converted
	case int64:
		converted := float64(typedValue)
		return &converted
	case string:
		normalized = strings.ReplaceAll(typedValue, ",", "")
	case bool, nil:
		return nil
	default:
		return nil
	}

	parsed, parseErr := strconv.ParseFloat(normalized, 64)
	if parseErr != nil {
		return nil
	}
	return &parsed
}

// Percent mirrors percent in utils.py: round(used/limit*100).
func Percent(used *int, limit *int) *int {
	if used == nil || limit == nil || *limit == 0 {
		return nil
	}
	computed := int(float64(*used) / float64(*limit) * 100)
	return &computed
}

// RemainingFromLimitAndUsed mirrors remaining_from_limit_and_used.
func RemainingFromLimitAndUsed(limit *int, used *int) *int {
	if limit == nil || used == nil {
		return nil
	}
	remaining := *limit - *used
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

// LeftPercentFromUsedPercent mirrors left_percent_from_used_percent.
func LeftPercentFromUsedPercent(usedPercent *int) *int {
	if usedPercent == nil {
		return nil
	}
	remaining := 100 - *usedPercent
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 100 {
		remaining = 100
	}
	return &remaining
}

// NormalizeLine mirrors normalize_line: collapse whitespace, trim.
var whitespacePattern = regexp.MustCompile(`\s+`)

func NormalizeLine(line string) string {
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(line, " "))
}
