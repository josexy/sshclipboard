package locales

import (
	"strings"
	"testing"
	"time"
)

// Spec "Plurals".
func TestPlurals(t *testing.T) {
	for n, want := range map[int]string{1: "1 file uploaded", 2: "2 files uploaded", 0: "0 files uploaded"} {
		if got := SummaryUploaded(n); got != want {
			t.Errorf("SummaryUploaded(%d) = %q, want %q", n, got, want)
		}
	}
	if got := SummaryFailed(0); got != "No failures" {
		t.Errorf("%q", got)
	}
	if got := SummaryFailed(1); got != "1 failure" {
		t.Errorf("%q", got)
	}
	if got := QuitMessage(2); !strings.HasPrefix(got, "2 uploads are in progress") {
		t.Errorf("%q", got)
	}
}

func TestLiteralBracesAndArguments(t *testing.T) {
	if got := CopyTemplateHint(); !strings.Contains(got, "{path}") || !strings.Contains(got, "{profile}") {
		t.Errorf("%q", got)
	}
	if got := TokensHelp(); !strings.Contains(got, "{name}") || !strings.Contains(got, "{date:FORMAT}") || !strings.Contains(got, "{{") {
		t.Errorf("%q", got)
	}
	if got := ErrUnknownToken("{nmae}", 1); got != "{nmae} is an unknown token (position 1)" {
		t.Errorf("%q", got)
	}
	if got := Progress(30, "2.1 MB"); got != "30% · 2.1 MB/s" {
		t.Errorf("%q", got)
	}
	if got := TimeToday(time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)); !strings.Contains(got, "3:30") {
		t.Errorf("%q", got)
	}
}
