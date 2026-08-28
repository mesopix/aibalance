package aibalance

import "testing"

func TestRedactTextMasksBearerToken(t *testing.T) {
	// The assignment pattern also rewrites the "Authorization:" prefix, so
	// the final output matches the Python pipeline byte for byte.
	input := "Authorization: Bearer abcdefghijklmnop"
	want := "Authorization: <redacted> <redacted-token>"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactTextMasksJWT(t *testing.T) {
	// Header and claim segments long enough to match; signature is a dot away.
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c"
	input := "token=" + token
	want := "token=<redacted-jwt>"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactTextMasksAPIToken(t *testing.T) {
	input := "key sk-abcdefghijklmnopqrstuvwx failed"
	want := "key <redacted-api-token> failed"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactTextMasksEmail(t *testing.T) {
	input := "contact someone@example.com for details"
	want := "contact <redacted-email> for details"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactTextMasksUUID(t *testing.T) {
	// Only UUID versions 1-5 match, mirroring the Python pattern; the
	// time-ordered v7 form (019d...) stays visible in both pipelines.
	input := "org 550e8400-e29b-41d4-a716-446655440000 here"
	want := "org <redacted-uuid> here"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
	unmatched := RedactText("org 019d718e-28a7-7826-b911-82f23ec39daf here")
	if unmatched != "org 019d718e-28a7-7826-b911-82f23ec39daf here" {
		t.Errorf("v7 UUID should stay unredacted like Python, got %q", unmatched)
	}
}

func TestRedactTextMasksSensitiveURLParameter(t *testing.T) {
	input := "https://example.com/api?token=supersecret&x=1"
	want := "https://example.com/api?token=<redacted>&x=1"
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactTextMasksSensitiveAssignment(t *testing.T) {
	input := `{"api_key": "supersecretvalue"}`
	want := `{"api_key": "<redacted>"}`
	if got := RedactText(input); got != want {
		t.Errorf("RedactText() = %q, want %q", got, want)
	}
}

func TestRedactDataMasksSensitiveKeys(t *testing.T) {
	input := map[string]any{
		"session_id": "abc123",
		"plan":       "team",
		"nested":     map[string]any{"password": "hunter2"},
	}
	got := RedactData(input, "").(map[string]any)
	if got["session_id"] != "<redacted>" {
		t.Errorf("session_id = %v, want <redacted>", got["session_id"])
	}
	if got["plan"] != "team" {
		t.Errorf("plan = %v, want team", got["plan"])
	}
	nested := got["nested"].(map[string]any)
	if nested["password"] != "<redacted>" {
		t.Errorf("nested password = %v, want <redacted>", nested["password"])
	}
}

func TestRedactDataKeepsNumbersAndBooleans(t *testing.T) {
	input := map[string]any{"limit": 3000, "ok": true}
	got := RedactData(input, "").(map[string]any)
	if got["limit"] != 3000 || got["ok"] != true {
		t.Errorf("RedactData() = %v, want unchanged scalars", got)
	}
}
