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

// AllToolSpecsForLLM returns Anthropic custom-tool definitions.
func AllToolSpecsForLLM() []map[string]any {
	specs := make([]map[string]any, 0, len(READ_ONLY_TOOLS))
	for _, tool := range READ_ONLY_TOOLS {
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
