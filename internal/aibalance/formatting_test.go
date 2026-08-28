package aibalance

import "testing"

func TestFormatISODatetimeWithOffset(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"utc micros", "2026-08-25T08:57:00.123456+00:00", "2026-08-25 16:57 CST"},
		{"z suffix", "2026-08-25T08:57:00Z", "2026-08-25 16:57 CST"},
		{"naive treated as utc", "2026-08-25T08:57:00", "2026-08-25 16:57 CST"},
		{"naive space form", "2026-08-25 08:57:00", "2026-08-25 16:57 CST"},
		{"date only", "2026-08-25", "2026-08-25 08:00 CST"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := FormatISODatetime(testCase.input); got != testCase.want {
				t.Errorf("FormatISODatetime(%q) = %v, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestFormatISODatetimeInvalid(t *testing.T) {
	for _, input := range []any{"", nil, "not-a-date", 42} {
		if got := FormatISODatetime(input); got != nil {
			t.Errorf("FormatISODatetime(%v) = %v, want nil", input, got)
		}
	}
}

func TestParseCachedTimestamp(t *testing.T) {
	// Shanghai display form written into latest_summary.json by SummarizeOutput.
	shanghaiForm, ok := ParseCachedTimestamp("2026-08-25 16:57 CST")
	if !ok {
		t.Fatalf("ParseCachedTimestamp(shanghai form) failed to parse")
	}
	if got := shanghaiForm.Format("2006-01-02T15:04:05Z07:00"); got != "2026-08-25T16:57:00+08:00" {
		t.Errorf("shanghai form parsed to %s, want 2026-08-25T16:57:00+08:00", got)
	}

	// Raw ISO form produced by Run().
	isoForm, ok := ParseCachedTimestamp("2026-08-25T08:57:00.123456+00:00")
	if !ok {
		t.Fatalf("ParseCachedTimestamp(iso form) failed to parse")
	}
	if got := isoForm.Format("2006-01-02T15:04:05Z07:00"); got != "2026-08-25T08:57:00Z" {
		t.Errorf("iso form parsed to %s, want 2026-08-25T08:57:00Z", got)
	}

	for _, input := range []any{"", nil, "not-a-date", 42} {
		if _, ok := ParseCachedTimestamp(input); ok {
			t.Errorf("ParseCachedTimestamp(%v) parsed, want failure", input)
		}
	}
}

func TestFormatEpochMillis(t *testing.T) {
	// 2026-08-25 08:57:00 UTC == 1787648220000 ms.
	if got := FormatEpochMillis(float64(1787648220000)); got != "2026-08-25 16:57 CST" {
		t.Errorf("FormatEpochMillis() = %v, want 2026-08-25 16:57 CST", got)
	}
	if got := FormatEpochMillis(int64(1787648220000)); got != "2026-08-25 16:57 CST" {
		t.Errorf("FormatEpochMillis(int64) = %v, want 2026-08-25 16:57 CST", got)
	}
}

func TestFormatEpochMillisInvalid(t *testing.T) {
	for _, input := range []any{0, -1, "abc", nil} {
		if got := FormatEpochMillis(input); got != nil {
			t.Errorf("FormatEpochMillis(%v) = %v, want nil", input, got)
		}
	}
}
