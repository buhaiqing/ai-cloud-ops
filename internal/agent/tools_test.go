package agent

import (
	"strings"
	"testing"
)

func TestReadOnlyToolsAreAllReadOnly(t *testing.T) {
	for _, tool := range READ_ONLY_TOOLS {
		if tool.Category != ReadOnly {
			t.Errorf("%s is not ReadOnly", tool.Name)
		}
	}
}

func TestWhitelistHas10Tools(t *testing.T) {
	if got := len(READ_ONLY_TOOLS); got != 10 {
		t.Errorf("got %d tools, want 10", got)
	}
}

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"describe_ecs_instances", true},
		{"describe_rds_slow_logs", true},
		{"lookup_actiontrail_events", true},
		{"delete_ecs_instance", false},
		{"reboot_rds", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := IsAllowed(tc.name); got != tc.want {
			t.Errorf("IsAllowed(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestGetReturnsToolOrError(t *testing.T) {
	tool, err := Get("describe_ecs_instances")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tool.AliyunService != "ECS" || tool.APIAction != "DescribeInstances" {
		t.Errorf("got %+v", tool)
	}

	_, err = Get("delete_ecs_instance")
	if err == nil {
		t.Fatal("expected error for non-whitelisted tool")
	}
	if _, ok := err.(*ToolNotAllowedError); !ok {
		t.Errorf("expected ToolNotAllowedError, got %T", err)
	}
}

func TestAllToolSpecsForLLM(t *testing.T) {
	specs := AllToolSpecsForLLM()
	if len(specs) != len(READ_ONLY_TOOLS) {
		t.Errorf("got %d specs, want %d", len(specs), len(READ_ONLY_TOOLS))
	}
	for i, spec := range specs {
		if _, ok := spec["name"].(string); !ok {
			t.Errorf("spec[%d] missing name", i)
		}
		if _, ok := spec["description"].(string); !ok {
			t.Errorf("spec[%d] missing description", i)
		}
		schema, ok := spec["input_schema"].(map[string]any)
		if !ok {
			t.Errorf("spec[%d] missing input_schema", i)
			continue
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Errorf("spec[%d] input_schema missing properties", i)
			continue
		}
		if _, ok := props["region"]; !ok {
			t.Errorf("spec[%d] input_schema.properties missing region", i)
		}
	}
}

func TestNoMutatingActionsInWhitelist(t *testing.T) {
	mutating := []string{"delete", "reboot", "release", "destroy", "drop", "terminate"}
	for _, tool := range READ_ONLY_TOOLS {
		desc := strings.ToLower(tool.Description)
		for _, m := range mutating {
			if strings.Contains(desc, m) {
				t.Errorf("%s description contains mutating verb %q", tool.Name, m)
			}
		}
		if containsMutatingAction(tool) != strings.ContainsAny(desc, "") {
			t.Errorf("containsMutatingAction inconsistent for %s", tool.Name)
		}
	}
}