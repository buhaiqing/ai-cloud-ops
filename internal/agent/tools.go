// Package agent implements the AI diagnosis agent for ai-cloud-ops.
//
// The agent takes a CloudMonitor alert, uses Claude with a tool-use loop
// to gather context (via the read-only whitelist), and returns a structured
// diagnosis (root cause + recommendations + evidence chains).
//
// This is the Go equivalent of src/ai_cloud_ops/agent/{client,tools,prompt}.py.
package agent

import (
	"fmt"
	"strings"
)

// ToolCategory marks whether a tool is read-only or mutating.
// M1 only exposes ReadOnly. The Write category is reserved for M3+ execute
// tools (reboot, scale) and never appears in the M1 whitelist.
type ToolCategory string

const (
	ReadOnly ToolCategory = "read_only"
	Write    ToolCategory = "write"
)

// Tool is a whitelisted Aliyun OpenAPI tool the AI Agent may invoke.
type Tool struct {
	Name          string
	Category      ToolCategory
	AliyunService string
	APIAction     string
	Description   string
}

// READ_ONLY_TOOLS is the M1 whitelist. Mirrors src/ai_cloud_ops/agent/tools.py.
// Single source of truth: keep in sync with Python (until deprecation).
var READ_ONLY_TOOLS = []Tool{
	{
		Name:          "describe_ecs_instances",
		Category:      ReadOnly,
		AliyunService: "ECS",
		APIAction:     "DescribeInstances",
		Description:   "List ECS instances with status, tags, network info in a region.",
	},
	{
		Name:          "describe_ecs_instance_status",
		Category:      ReadOnly,
		AliyunService: "ECS",
		APIAction:     "DescribeInstanceStatus",
		Description:   "Get ECS instance health/status for a specific instance ID.",
	},
	{
		Name:          "describe_ecs_monitor_data",
		Category:      ReadOnly,
		AliyunService: "ECS",
		APIAction:     "DescribeInstanceMonitorData",
		Description:   "Pull CPU/memory/disk/network metrics for an ECS instance over a time range.",
	},
	{
		Name:          "describe_rds_instances",
		Category:      ReadOnly,
		AliyunService: "RDS",
		APIAction:     "DescribeDBInstances",
		Description:   "List RDS instances with status, connection count, QPS.",
	},
	{
		Name:          "describe_rds_slow_logs",
		Category:      ReadOnly,
		AliyunService: "RDS",
		APIAction:     "DescribeSlowLogs",
		Description:   "Get slow query logs for an RDS instance over a time range.",
	},
	{
		Name:          "describe_slb_load_balancers",
		Category:      ReadOnly,
		AliyunService: "SLB",
		APIAction:     "DescribeLoadBalancers",
		Description:   "List SLB instances with backend server health.",
	},
	{
		Name:          "describe_cms_metric_list",
		Category:      ReadOnly,
		AliyunService: "CMS",
		APIAction:     "DescribeMetricList",
		Description:   "Pull raw metric datapoints for any resource over a time range.",
	},
	{
		Name:          "lookup_actiontrail_events",
		Category:      ReadOnly,
		AliyunService: "ActionTrail",
		APIAction:     "LookupEvents",
		Description:   "Look up recent API calls / changes to a resource. Key for root cause.",
	},
	{
		Name:          "list_tag_resources",
		Category:      ReadOnly,
		AliyunService: "Common",
		APIAction:     "ListTagResources",
		Description:   "List tags on a resource — for environment/owner correlation.",
	},
	{
		Name:          "describe_oss_buckets",
		Category:      ReadOnly,
		AliyunService: "OSS",
		APIAction:     "GetBucketInfo",
		Description:   "Get OSS bucket metadata (size, location, ACL).",
	},
}

// ToolNotAllowedError is returned for any tool outside the whitelist.
type ToolNotAllowedError struct {
	Name string
}

func (e *ToolNotAllowedError) Error() string {
	return fmt.Sprintf("tool not in whitelist: %s", e.Name)
}

// IsAllowed checks whether a tool name is in the M1 whitelist.
func IsAllowed(name string) bool {
	for _, t := range READ_ONLY_TOOLS {
		if t.Name == name {
			return true
		}
	}
	return false
}

// Get returns the tool spec by name. Returns (*ToolNotAllowedError) if missing.
func Get(name string) (Tool, error) {
	for _, t := range READ_ONLY_TOOLS {
		if t.Name == name {
			return t, nil
		}
	}
	return Tool{}, &ToolNotAllowedError{Name: name}
}

// AllToolSpecsForLLM returns tool specs in Anthropic's tool-use format.
// See https://docs.anthropic.com/en/docs/build-with-claude/tool-use
func AllToolSpecsForLLM() []map[string]any {
	out := make([]map[string]any, 0, len(READ_ONLY_TOOLS))
	for _, t := range READ_ONLY_TOOLS {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"region":      map[string]any{"type": "string", "description": "Aliyun region ID"},
					"resource_id": map[string]any{"type": "string", "description": "Specific resource ID (optional)"},
				},
				"required": []string{"region"},
			},
		})
	}
	return out
}

// AllReadOnly are the tools guaranteed safe (read-only). For M1 this is
// the full whitelist. M3+ will add a separate `Write` slice.
func AllReadOnly() []Tool {
	return READ_ONLY_TOOLS
}

// containsMutatingAction is a static safety check used by tests.
func containsMutatingAction(t Tool) bool {
	mutating := []string{"delete", "reboot", "release", "destroy", "drop", "terminate"}
	desc := strings.ToLower(t.Description)
	for _, m := range mutating {
		if strings.Contains(desc, m) {
			return true
		}
	}
	return false
}