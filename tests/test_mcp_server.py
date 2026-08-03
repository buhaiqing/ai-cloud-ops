"""Unit tests for MCP server (REVIEW-3 checkpoint).

These tests verify:
1. The FastMCP server is registered with the right tool names
2. Tool functions delegate to the right underlying modules
3. Errors are returned as JSON (not raised) — MCP clients expect strings
"""

from __future__ import annotations

import json

import pytest


def test_mcp_server_has_all_tools() -> None:
    """Server exposes 6 tools (1 diagnose + 1 query + 3 describe + 1 list)."""
    from ai_cloud_ops.mcp_server import mcp

    tool_names = set(mcp._tool_manager._tools.keys())  # type: ignore[attr-defined]
    expected = {
        "diagnose_alert",
        "list_recent_alerts",
        "describe_ecs_instances",
        "describe_rds_instances",
        "describe_slb_load_balancers",
        "list_accounts",
    }
    assert expected.issubset(tool_names), f"missing: {expected - tool_names}"


def test_mcp_server_description_mentions_read_only() -> None:
    """Server metadata clarifies no mutating ops."""
    from ai_cloud_ops.mcp_server import mcp

    instructions = mcp._tool_manager._instructions if hasattr(mcp._tool_manager, "_instructions") else ""
    assert "read-only" in instructions.lower() or "no mutating" in instructions.lower()


def test_account_helper_raises_helpful_error() -> None:
    """_account() raises ValueError with available aliases listed."""
    from ai_cloud_ops.config import AccountConfig, Config
    from ai_cloud_ops.mcp_server import _account

    cfg = Config.model_validate(
        {
            "accounts": {
                "prod": {"role_arn": "acs:ram::1:role/x", "regions": ["cn-hangzhou"]},
            }
        }
    )
    with pytest.raises(ValueError, match="unknown account 'staging'"):
        _account(cfg, "staging")
    # The error should mention available aliases
    with pytest.raises(ValueError, match="prod"):
        _account(cfg, "staging")


def test_load_config_raises_with_helpful_message(tmp_path) -> None:
    """Missing config file produces a clear error."""
    from ai_cloud_ops.mcp_server import _load_config

    with pytest.raises(RuntimeError, match="accounts config not found"):
        _load_config()


@pytest.mark.asyncio
async def test_diagnose_alert_returns_json_on_missing() -> None:
    """When alert not found, return JSON error (not raise) — MCP strings only."""
    from unittest.mock import AsyncMock, patch

    from ai_cloud_ops.mcp_server import diagnose_alert

    # Mock the DB session to return no rows
    with patch("ai_cloud_ops.db.get_session") as mock_session:
        mock_session.return_value.__aenter__.return_value.execute = AsyncMock(
            return_value=AsyncMock(first=lambda: None)
        )

        result = await diagnose_alert.fn(  # type: ignore[attr-defined]
            alert_id="nonexistent", region="cn-hangzhou", account_alias="prod"
        )
    parsed = json.loads(result)
    assert "error" in parsed


@pytest.mark.asyncio
async def test_list_accounts_returns_json() -> None:
    """list_accounts tool returns valid JSON list of accounts."""
    from unittest.mock import patch

    from ai_cloud_ops.config import AccountConfig, Config
    from ai_cloud_ops.mcp_server import list_accounts

    cfg = Config.model_validate(
        {
            "accounts": {
                "prod": {"role_arn": "acs:ram::1:role/x", "regions": ["cn-hangzhou"]},
                "staging": {"role_arn": "acs:ram::2:role/y", "regions": ["cn-shanghai"]},
            }
        }
    )

    with patch("ai_cloud_ops.mcp_server._load_config", return_value=cfg):
        result = await list_accounts.fn()  # type: ignore[attr-defined]

    parsed = json.loads(result)
    assert "prod" in parsed
    assert "staging" in parsed
    assert parsed["prod"]["role_arn"] == "acs:ram::1:role/x"
    assert parsed["prod"]["regions"] == ["cn-hangzhou"]