"""AI Agent tool whitelist (T2).

Per design.md decision T2:
- "View" tools (read-only): broad whitelist of 10+ Describe/Status/Metric/Tag tools
- "Action" tools: separate module, requires human approval (M3+ feature, not M1)

This module defines the whitelist and the registry that the Agent can
iterate over. Calls go through a single dispatch function that enforces
the whitelist — no way to call non-whitelisted APIs.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import Enum


class ToolCategory(str, Enum):
    READ_ONLY = "read_only"
    WRITE = "write"  # M3+ — not exposed in M1


@dataclass(frozen=True)
class Tool:
    """A whitelisted Aliyun OpenAPI tool the AI Agent may invoke."""

    name: str
    category: ToolCategory
    aliyun_service: str  # 'ECS', 'RDS', 'SLB', 'CMS', 'ActionTrail', ...
    api_action: str  # 'DescribeInstances', 'DescribeMetricList', ...
    description: str  # shown to the LLM in the tool schema


# Read-only tool whitelist (M1).
# Each entry maps to an Aliyun OpenAPI action that takes a region + account context.
# Design principle: only describe, never mutate.
READ_ONLY_TOOLS: tuple[Tool, ...] = (
    # ECS — compute
    Tool(
        name="describe_ecs_instances",
        category=ToolCategory.READ_ONLY,
        aliyun_service="ECS",
        api_action="DescribeInstances",
        description="List ECS instances with status, tags, network info in a region.",
    ),
    Tool(
        name="describe_ecs_instance_status",
        category=ToolCategory.READ_ONLY,
        aliyun_service="ECS",
        api_action="DescribeInstanceStatus",
        description="Get ECS instance health/status for a specific instance ID.",
    ),
    Tool(
        name="describe_ecs_monitor_data",
        category=ToolCategory.READ_ONLY,
        aliyun_service="ECS",
        api_action="DescribeInstanceMonitorData",
        description="Pull CPU/memory/disk/network metrics for an ECS instance over a time range.",
    ),
    # RDS — database
    Tool(
        name="describe_rds_instances",
        category=ToolCategory.READ_ONLY,
        aliyun_service="RDS",
        api_action="DescribeDBInstances",
        description="List RDS instances with status, connection count, QPS.",
    ),
    Tool(
        name="describe_rds_slow_logs",
        category=ToolCategory.READ_ONLY,
        aliyun_service="RDS",
        api_action="DescribeSlowLogs",
        description="Get slow query logs for an RDS instance over a time range.",
    ),
    # SLB — load balancer
    Tool(
        name="describe_slb_load_balancers",
        category=ToolCategory.READ_ONLY,
        aliyun_service="SLB",
        api_action="DescribeLoadBalancers",
        description="List SLB instances with backend server health.",
    ),
    # CMS — CloudMonitor (for context, not primary alert source — T4)
    Tool(
        name="describe_cms_metric_list",
        category=ToolCategory.READ_ONLY,
        aliyun_service="CMS",
        api_action="DescribeMetricList",
        description="Pull raw metric datapoints for any resource over a time range.",
    ),
    # ActionTrail — for change correlation (T16, M3 — but registered now)
    Tool(
        name="lookup_actiontrail_events",
        category=ToolCategory.READ_ONLY,
        aliyun_service="ActionTrail",
        api_action="LookupEvents",
        description="Look up recent API calls / changes to a resource. Key for root cause.",
    ),
    # Resource tagging (read)
    Tool(
        name="list_tag_resources",
        category=ToolCategory.READ_ONLY,
        aliyun_service="Common",
        api_action="ListTagResources",
        description="List tags on a resource — for environment/owner correlation.",
    ),
    # OSS — object storage
    Tool(
        name="describe_oss_buckets",
        category=ToolCategory.READ_ONLY,
        aliyun_service="OSS",
        api_action="GetBucketInfo",
        description="Get OSS bucket metadata (size, location, ACL).",
    ),
)


class ToolNotAllowedError(Exception):
    """Raised when the Agent tries to call a tool not in the whitelist."""


def is_allowed(tool_name: str) -> bool:
    """Check if a tool name is in the whitelist. Fast O(n) lookup; n=10."""
    return any(t.name == tool_name for t in READ_ONLY_TOOLS)


def get_tool(tool_name: str) -> Tool:
    """Return the tool spec by name, or raise ToolNotAllowedError."""
    for t in READ_ONLY_TOOLS:
        if t.name == tool_name:
            return t
    raise ToolNotAllowedError(f"tool not in whitelist: {tool_name}")


def all_tool_specs_for_llm() -> list[dict[str, str]]:
    """Return tool specs in Anthropic's tool-use format for LLM prompts."""
    return [
        {
            "name": t.name,
            "description": t.description,
            "input_schema": {
                "type": "object",
                "properties": {
                    "region": {"type": "string", "description": "Aliyun region ID"},
                    "resource_id": {
                        "type": "string",
                        "description": "Specific resource ID (optional)",
                    },
                },
                "required": ["region"],
            },
        }
        for t in READ_ONLY_TOOLS
    ]