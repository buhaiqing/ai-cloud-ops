"""MCP Server for ai-cloud-ops (REVIEW-3 — post-M1 checkpoint).

Exposes the AI Agent + Aliyun read-only tools via the Model Context Protocol
so users can interact with their Aliyun resources from Claude Desktop, Cline,
Cursor, or any other MCP-compatible AI client.

Per plan-eng-review REVIEW-3: this is an A/B test against the Next.js Dashboard.
If users prefer MCP, M2 architecture shifts; if they prefer a custom Dashboard,
MCP becomes an additional interface rather than the primary one.

Tools exposed (all read-only per T2):
- diagnose_alert         — AI Agent run on a CloudMonitor alert
- list_recent_alerts     — query alerts table
- describe_ecs_instances — list ECS in a region
- describe_rds_instances — list RDS in a region
- describe_slb_load_balancers — list SLB in a region
- list_accounts          — show configured accounts

Run: `uv run ai-cloud-ops-mcp` (stdio transport for Claude Desktop / Cline)
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from pathlib import Path
from typing import Any

from mcp.server.fastmcp import FastMCP

logger = logging.getLogger(__name__)

mcp = FastMCP(
    "ai-cloud-ops",
    instructions=(
        "AI-Native Alibaba Cloud multi-account ops console. "
        "All tools are read-only — they describe Aliyun resources and run AI diagnostics "
        "on CloudMonitor alerts. Use diagnose_alert for incident root-cause analysis."
    ),
)


def _load_config() -> Any:
    """Load account config from YAML. Lazy-loaded on first tool call."""
    from ai_cloud_ops.config import load_config

    config_path = Path(os.environ.get("ACCOUNTS_CONFIG_PATH", "./accounts.yaml"))
    if not config_path.exists():
        raise RuntimeError(
            f"accounts config not found at {config_path}; "
            f"set ACCOUNTS_CONFIG_PATH or create accounts.yaml"
        )
    return load_config(config_path)


def _account(config: Any, alias: str) -> Any:
    """Resolve account by alias; raise with helpful message if not found."""
    if alias not in config.accounts:
        available = ", ".join(config.accounts.keys())
        raise ValueError(f"unknown account {alias!r}; available: {available}")
    return config.accounts[alias]


# ---------------------------------------------------------------------------
# Read-only tools — all delegate to existing modules
# ---------------------------------------------------------------------------


@mcp.tool()
async def diagnose_alert(alert_id: str, region: str, account_alias: str) -> str:
    """Run AI diagnosis on a CloudMonitor alert.

    Looks up the alert in the local DB, calls the AI Agent, and returns
    the structured diagnosis (root_cause, recommendations, evidence_chains).
    """
    from ai_cloud_ops.agent.client import diagnose
    from ai_cloud_ops.db import get_session

    async with get_session() as session:
        result = await session.execute(
            """
            SELECT payload FROM alerts
            WHERE alert_id = :aid AND region = :r AND account_alias = :a
            ORDER BY created_at DESC LIMIT 1
            """,
            {"aid": alert_id, "r": region, "a": account_alias},
        )
        row = result.first()

    if row is None:
        return json.dumps(
            {"error": f"alert {alert_id!r} not found in {region}/{account_alias}"}
        )

    diagnosis = await diagnose(row[0])
    return diagnosis.model_dump_json(indent=2)


@mcp.tool()
async def list_recent_alerts(
    region: str,
    account_alias: str,
    hours_back: int = 24,
    severity: str | None = None,
    limit: int = 50,
) -> str:
    """List recent alerts in a region. Optional severity filter (critical/warning/info)."""
    from ai_cloud_ops.db import get_session

    params: dict[str, Any] = {
        "r": region,
        "a": account_alias,
        "hours_back": hours_back,
        "limit": limit,
    }
    sql = """
        SELECT alert_id, severity, resource_type, resource_id, name, status, created_at
        FROM alerts
        WHERE region = :r AND account_alias = :a
          AND created_at > now() - make_interval(hours => :hours_back)
    """
    if severity:
        sql += " AND severity = :sev"
        params["sev"] = severity
    sql += " ORDER BY created_at DESC LIMIT :limit"

    async with get_session() as session:
        result = await session.execute(sql, params)
        rows = result.fetchall()

    return json.dumps(
        [
            {
                "alert_id": r[0],
                "severity": r[1],
                "resource_type": r[2],
                "resource_id": r[3],
                "name": r[4],
                "status": r[5],
                "created_at": r[6].isoformat(),
            }
            for r in rows
        ],
        indent=2,
    )


@mcp.tool()
async def describe_ecs_instances(region: str, account_alias: str) -> str:
    """List ECS instances in a region (paginated, returns first 50)."""
    from ai_cloud_ops.ingest.fetcher import describe_ecs_instances as _describe

    config = _load_config()
    account = _account(config, account_alias)
    instances = await _describe(account, region, sts=None)  # type: ignore[arg-type]
    return json.dumps(instances[:50], indent=2, default=str)


@mcp.tool()
async def describe_rds_instances(region: str, account_alias: str) -> str:
    """List RDS instances in a region."""
    from ai_cloud_ops.ingest.fetcher import describe_rds_instances as _describe

    config = _load_config()
    account = _account(config, account_alias)
    instances = await _describe(account, region, sts=None)  # type: ignore[arg-type]
    return json.dumps(instances[:50], indent=2, default=str)


@mcp.tool()
async def describe_slb_load_balancers(region: str, account_alias: str) -> str:
    """List SLB load balancers in a region."""
    from ai_cloud_ops.ingest.fetcher import describe_slb_load_balancers as _describe

    config = _load_config()
    account = _account(config, account_alias)
    lbs = await _describe(account, region, sts=None)  # type: ignore[arg-type]
    return json.dumps(lbs[:50], indent=2, default=str)


@mcp.tool()
async def list_accounts() -> str:
    """Show configured accounts (alias + role_arn + regions)."""
    config = _load_config()
    return json.dumps(
        {
            alias: {
                "role_arn": acct.role_arn,
                "regions": acct.regions,
            }
            for alias, acct in config.accounts.items()
        },
        indent=2,
    )


def main() -> None:
    """Entry point for `ai-cloud-ops-mcp` console script.

    Runs MCP server over stdio transport — Claude Desktop / Cline /
    Cursor pipe stdin/stdout to this process.
    """
    from ai_cloud_ops.logging_config import configure_logging

    configure_logging(level=os.environ.get("LOG_LEVEL", "INFO"))
    logger.info("ai-cloud-ops MCP server starting (stdio transport)")
    mcp.run()  # stdio transport is default for FastMCP


if __name__ == "__main__":
    main()