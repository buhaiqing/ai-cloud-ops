package agent

import (
	"strings"
	"testing"
)

func TestBuildUserPromptIncludesAlertJSON(t *testing.T) {
	prompt := BuildUserPrompt(map[string]any{
		"alert_id": "alert-1",
		"severity": "critical",
	})
	for _, want := range []string{`"alert_id": "alert-1"`, `"severity": "critical"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildUserPrompt() missing %s:\n%s", want, prompt)
		}
	}
}

func TestPromptVersionIsNonEmpty(t *testing.T) {
	if PromptVersion == "" {
		t.Fatal("PromptVersion must not be empty")
	}
}
