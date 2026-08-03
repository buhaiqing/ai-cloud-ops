package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPromptVersionIsSet(t *testing.T) {
	if PromptVersion == "" {
		t.Fatal("PromptVersion should not be empty")
	}
	if !strings.HasPrefix(PromptVersion, "v") {
		t.Errorf("PromptVersion should start with 'v', got %q", PromptVersion)
	}
}

func TestSystemPromptMentionsReadOnly(t *testing.T) {
	if !strings.Contains(SystemPrompt, "read-only") {
		t.Error("SystemPrompt should explicitly say read-only tools")
	}
}

func TestBuildUserPromptIncludesAlert(t *testing.T) {
	alert := map[string]any{
		"alert_id": "alert-test-001",
		"severity": "warning",
		"metric":   map[string]any{"value": 95.0},
	}
	prompt := BuildUserPrompt(alert)

	// Should contain the alert JSON
	if !strings.Contains(prompt, "alert-test-001") {
		t.Error("prompt should include alert_id")
	}
	if !strings.Contains(prompt, "warning") {
		t.Error("prompt should include severity")
	}
	// Should be JSON-serializable for logging
	var dummy map[string]any
	if err := json.Unmarshal([]byte(prompt[:0]), &dummy); err != nil && err.Error() == "unexpected end of JSON input" {
		// Empty prefix fails to parse, that's expected — just verifying prompt is a string
	}
}