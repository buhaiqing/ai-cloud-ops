package agent

import "fmt"

// Category classifies tools by whether they can change cloud resources.
type Category string

const (
	ReadOnly Category = "read_only"
	Write    Category = "write"
)

// Tool describes an Aliyun OpenAPI action exposed to the model.
type Tool struct {
	Name          string
	Category      Category
	AliyunService string
	APIAction     string
	Description   string
	// M3 write-tool safety metadata (zero-valued for read-only tools).
	Risk             string // low | medium | high
	Rollback         string // how to undo; "n/a..." when not reversible
	RateLimitPerHour int    // per-account execution cap
}

// READ_ONLY_TOOLS is the M1 tool whitelist. No mutating action is exposed.
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

// WRITE_TOOLS is the M3-5 execution whitelist. These tools are NEVER executed
// directly: DiagnoseDryRun intercepts them into PlannedActions, and only an
// approved exec plan may run them through the (stub) executor.
// Source of truth: audit-results/contract-m3-5.md write_tools_whitelist.
var WRITE_TOOLS = []Tool{
	{
		Name:             "restart_ecs_instance",
		Category:         Write,
		AliyunService:    "ECS",
		APIAction:        "RebootInstance",
		Description:      "Reboot an ECS instance to recover from OS-level hangs.",
		Risk:             "medium",
		Rollback:         "n/a (transient reboot)",
		RateLimitPerHour: 5,
	},
	{
		Name:             "scale_rds_instance",
		Category:         Write,
		AliyunService:    "RDS",
		APIAction:        "ModifyDBInstanceSpec",
		Description:      "Scale an RDS instance spec up/down to relieve saturation.",
		Risk:             "high",
		Rollback:         "ModifyDBInstanceSpec (downgrade)",
		RateLimitPerHour: 2,
	},
	{
		Name:             "restart_rds_instance",
		Category:         Write,
		AliyunService:    "RDS",
		APIAction:        "RestartDBInstance",
		Description:      "Restart an RDS instance to reset connections / clear stalls.",
		Risk:             "medium",
		Rollback:         "n/a",
		RateLimitPerHour: 3,
	},
	{
		Name:             "remove_ecs_from_slb",
		Category:         Write,
		AliyunService:    "SLB",
		APIAction:        "RemoveBackendServers",
		Description:      "Remove a backend ECS from an SLB to stop routing traffic to it.",
		Risk:             "medium",
		Rollback:         "AddBackendServers",
		RateLimitPerHour: 5,
	},
}

// ToolNotAllowedError reports an attempted call outside the whitelist.
type ToolNotAllowedError struct {
	Name string
}

func (e ToolNotAllowedError) Error() string {
	return fmt.Sprintf("tool not in whitelist: %s", e.Name)
}

// IsAllowed reports whether name belongs to the ten-tool M1 whitelist.
func IsAllowed(name string) bool {
	for _, tool := range READ_ONLY_TOOLS {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// Get returns a whitelisted tool by name.
func Get(name string) (Tool, bool) {
	for _, tool := range READ_ONLY_TOOLS {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// IsWriteAllowed reports whether name belongs to the M3-5 WRITE_TOOLS whitelist.
func IsWriteAllowed(name string) bool {
	_, ok := GetWriteTool(name)
	return ok
}

// GetWriteTool returns a whitelisted write tool by name.
func GetWriteTool(name string) (Tool, bool) {
	for _, tool := range WRITE_TOOLS {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

// AllToolSpecsForLLM returns Anthropic custom-tool definitions for the
// read-only M1 surface (used by Diagnose; unchanged).
func AllToolSpecsForLLM() []map[string]any {
	return toolSpecs(READ_ONLY_TOOLS)
}

// AllToolSpecsForLLMWithWrite returns read-only + WRITE_TOOLS specs. Used by
// DiagnoseDryRun so the model may propose whitelisted write actions, which
// are intercepted into PlannedActions instead of executed.
func AllToolSpecsForLLMWithWrite() []map[string]any {
	all := make([]Tool, 0, len(READ_ONLY_TOOLS)+len(WRITE_TOOLS))
	all = append(all, READ_ONLY_TOOLS...)
	all = append(all, WRITE_TOOLS...)
	return toolSpecs(all)
}

func toolSpecs(tools []Tool) []map[string]any {
	specs := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"region": map[string]any{
						"type":        "string",
						"description": "Aliyun region ID",
					},
					"resource_id": map[string]any{
						"type":        "string",
						"description": "Specific resource ID (optional)",
					},
				},
				"required": []string{"region"},
			},
		})
	}
	return specs
}
