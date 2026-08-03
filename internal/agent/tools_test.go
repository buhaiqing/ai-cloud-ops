package agent

import "testing"

func TestIsAllowed(t *testing.T) {
	if !IsAllowed("describe_ecs_instances") {
		t.Fatal("describe_ecs_instances should be allowed")
	}
	if IsAllowed("delete_ecs_instance") {
		t.Fatal("delete_ecs_instance should not be allowed")
	}
}

func TestReadOnlyToolsContainsExactlyTenTools(t *testing.T) {
	if got := len(READ_ONLY_TOOLS); got != 10 {
		t.Fatalf("READ_ONLY_TOOLS has %d tools, want 10", got)
	}
	for _, tool := range READ_ONLY_TOOLS {
		if tool.Category != ReadOnly {
			t.Fatalf("%s category = %q, want %q", tool.Name, tool.Category, ReadOnly)
		}
	}
}

func TestAllToolSpecsForLLM(t *testing.T) {
	specs := AllToolSpecsForLLM()
	if got := len(specs); got != 10 {
		t.Fatalf("AllToolSpecsForLLM returned %d specs, want 10", got)
	}
	for _, spec := range specs {
		if spec["name"] == "" || spec["description"] == "" {
			t.Fatalf("spec missing name or description: %#v", spec)
		}
		schema, ok := spec["input_schema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("invalid input_schema: %#v", spec["input_schema"])
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok || properties["region"] == nil {
			t.Fatalf("input_schema missing region property: %#v", schema)
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) != 1 || required[0] != "region" {
			t.Fatalf("input_schema required = %#v, want [region]", schema["required"])
		}
	}
}
