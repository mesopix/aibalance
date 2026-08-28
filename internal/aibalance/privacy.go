// Package aibalance implements the service balance scraper that replaces
// the Python ai_balance CLI. Output schemas stay field-compatible with the
// Python implementation so downstream consumers keep working.
package aibalance

import "regexp"

// Sensitive-key and text patterns mirror privacy.py. Go's regexp has no
// lookaround, so the JWT and API-token patterns capture their boundaries
// in groups instead and replacements preserve those groups. Go also caps
// repeat counts at 1000, so Python's {n,8192} bounds become open-ended
// {n,} — redaction only gets more aggressive, never less.
var (
	sensitiveKeyPattern = regexp.MustCompile(`(?i)(` +
		`api[_-]?key|authorization|cookie|credential|password|passwd|private[_-]?key|secret|session|` +
		`access[_-]?token|refresh[_-]?token|id[_-]?token|auth[_-]?token|^token$|` +
		`email|phone|mobile|user[_-]?id|account[_-]?id|customer(number|[_-]?id)?|` +
		`project[_-]?id|organization[_-]?id|workspace[_-]?id|subscription[_-]?id|` +
		`tenant[_-]?id|member[_-]?id|device[_-]?id|zuser[_-]?id|aliyun[_-]?uid|org[_-]?id)`)

	bearerTokenPattern = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)

	jwtTextPattern = regexp.MustCompile(
		`(^|[^A-Za-z0-9_-])(eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})($|[^A-Za-z0-9_-])`)

	apiTokenPattern = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(` +
		`sk-sp-[A-Za-z0-9._*~-]{8,512}|` +
		`sk-[A-Za-z0-9_-]{12,512}|` +
		`github_pat_[A-Za-z0-9_]{20,512}|` +
		`gh[pousr]_[A-Za-z0-9]{20,255}|` +
		`AIza[A-Za-z0-9_-]{30,64}|` +
		`xox[baprs]-[A-Za-z0-9-]{20,512}|` +
		`AKIA[0-9A-Z]{16})`)

	emailTextPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)

	uuidTextPattern = regexp.MustCompile(
		`(?i)[0-9A-F]{8}-[0-9A-F]{4}-[1-5][0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}`)

	sensitiveURLParameterPattern = regexp.MustCompile(`(?i)([?&](` +
		`api[_-]?key|authorization|credential|password|secret|session|` +
		`access[_-]?token|refresh[_-]?token|id[_-]?token|auth[_-]?token|token|` +
		`email|phone|mobile|user[_-]?id|account[_-]?id|customer[_-]?id|` +
		`project[_-]?id|organization[_-]?id|workspace[_-]?id|subscription[_-]?id|` +
		`tenant[_-]?id|member[_-]?id|device[_-]?id)=)[^&#\s"'<>]{1,}`)

	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)(` +
		`["']?(api[_-]?key|authorization|cookie|credential|password|passwd|private[_-]?key|secret|session|` +
		`access[_-]?token|refresh[_-]?token|id[_-]?token|auth[_-]?token|token|` +
		`email|phone|mobile|user[_-]?id|account[_-]?id|customer[_-]?id|` +
		`project[_-]?id|organization[_-]?id|workspace[_-]?id|subscription[_-]?id|` +
		`tenant[_-]?id|member[_-]?id|device[_-]?id)["']?\s*[:=]\s*["']?)` +
		`[^"'\s<>,;}]{1,}`)
)

// RedactText scrubs tokens, emails, and UUIDs from free text, mirroring
// redact_text in privacy.py.
func RedactText(value string) string {
	redactedValue := bearerTokenPattern.ReplaceAllString(value, `$1<redacted-token>`)
	redactedValue = jwtTextPattern.ReplaceAllString(redactedValue, `$1<redacted-jwt>$3`)
	redactedValue = apiTokenPattern.ReplaceAllString(redactedValue, `$1<redacted-api-token>`)
	redactedValue = emailTextPattern.ReplaceAllString(redactedValue, `<redacted-email>`)
	redactedValue = uuidTextPattern.ReplaceAllString(redactedValue, `<redacted-uuid>`)
	redactedValue = sensitiveURLParameterPattern.ReplaceAllString(redactedValue, `$1<redacted>`)
	redactedValue = sensitiveAssignmentPattern.ReplaceAllString(redactedValue, `$1<redacted>`)
	return redactedValue
}

// RedactData recursively redacts values whose keys look sensitive and
// scrubs every retained string, mirroring redact_data in privacy.py.
func RedactData(value any, keyName string) any {
	if keyName != "" && sensitiveKeyPattern.MatchString(keyName) {
		return "<redacted>"
	}

	switch typedValue := value.(type) {
	case map[string]any:
		redactedMap := make(map[string]any, len(typedValue))
		for itemKey, itemValue := range typedValue {
			redactedMap[itemKey] = RedactData(itemValue, itemKey)
		}
		return redactedMap
	case []any:
		redactedList := make([]any, len(typedValue))
		for itemIndex, itemValue := range typedValue {
			redactedList[itemIndex] = RedactData(itemValue, keyName)
		}
		return redactedList
	case string:
		return RedactText(typedValue)
	default:
		return value
	}
}
