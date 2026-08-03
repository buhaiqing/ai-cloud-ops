"""Unit tests for AI Agent tool whitelist (T2)."""

from __future__ import annotations

import pytest

from ai_cloud_ops.agent.tools import (
    READ_ONLY_TOOLS,
    ToolCategory,
    ToolNotAllowedError,
    all_tool_specs_for_llm,
    get_tool,
    is_allowed,
)


def test_read_only_tools_are_all_read_only() -> None:
    """No write/mutate tools in M1 whitelist."""
    for tool in READ_ONLY_TOOLS:
        assert tool.category == ToolCategory.READ_ONLY, f"{tool.name} is not read-only"


def test_whitelist_has_10_plus_tools() -> None:
    """Design requirement: broad whitelist of 10+ read tools."""
    assert len(READ_ONLY_TOOLS) >= 10


def test_is_allowed_returns_true_for_whitelisted() -> None:
    assert is_allowed("describe_ecs_instances") is True
    assert is_allowed("describe_rds_slow_logs") is True


def test_is_allowed_returns_false_for_unknown() -> None:
    assert is_allowed("delete_ecs_instance") is False
    assert is_allowed("reboot_rds") is False
    assert is_allowed("") is False


def test_get_tool_returns_tool_spec() -> None:
    tool = get_tool("describe_ecs_instances")
    assert tool.name == "describe_ecs_instances"
    assert tool.aliyun_service == "ECS"
    assert tool.api_action == "DescribeInstances"


def test_get_tool_raises_for_unknown() -> None:
    with pytest.raises(ToolNotAllowedError):
        get_tool("delete_ecs_instance")


def test_tool_specs_for_llm_have_required_fields() -> None:
    """Anthropic tool-use format requires name + description + input_schema."""
    specs = all_tool_specs_for_llm()
    assert len(specs) == len(READ_ONLY_TOOLS)
    for spec in specs:
        assert "name" in spec
        assert "description" in spec
        assert "input_schema" in spec
        assert spec["input_schema"]["type"] == "object"
        assert "region" in spec["input_schema"]["properties"]
        assert "region" in spec["input_schema"]["required"]


def test_no_destroy_action_in_whitelist() -> None:
    """No tool description should mention destructive verbs."""
    forbidden = ["delete", "reboot", "release", "destroy", "drop", "terminate"]
    for tool in READ_ONLY_TOOLS:
        desc_lower = tool.description.lower()
        for verb in forbidden:
            assert verb not in desc_lower, f"{tool.name} description contains '{verb}'"


def test_actiontrail_registered_for_m3() -> None:
    """ActionTrail is registered now even though only consumed in M3 (T16)."""
    assert is_allowed("lookup_actiontrail_events") is True