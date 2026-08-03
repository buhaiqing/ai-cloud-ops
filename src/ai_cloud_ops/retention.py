"""Data retention enforcement (T18 — data-retention.md § 2).

Background cron task: purge old data per retention policies.
Runs every 24h via AlertWorker integration or standalone.

Tables:
- alerts       → 90 days
- resources    → 30 days (TTL cache aligned)
- dlq          → 14 days after resolved
- worker_heartbeat → 7 days
- logs         → 30 days (handled by logrotate, not here)

This is intentionally a separate module from worker — can be run as a
standalone cron job (`uv run python -m ai_cloud_ops.retention`) or
invoked from AlertWorker.
"""

from __future__ import annotations

import asyncio
import logging
import os
from datetime import datetime, timedelta, timezone

from prometheus_client import Counter

from ai_cloud_ops.db import get_session

logger = logging.getLogger(__name__)

# Override via env (per data-retention.md § 3.1)
RETENTION_ALERTS_DAYS = int(os.environ.get("DATA_RETENTION_ALERTS_DAYS", "90"))
RETENTION_RESOURCES_DAYS = int(os.environ.get("DATA_RETENTION_RESOURCES_DAYS", "30"))
RETENTION_DLQ_RESOLVED_DAYS = int(os.environ.get("DATA_RETENTION_DLQ_DAYS", "14"))
RETENTION_HEARTBEAT_DAYS = int(os.environ.get("DATA_RETENTION_HEARTBEAT_DAYS", "7"))


_rows_purged = Counter(
    "ai_cloud_ops_retention_rows_purged_total",
    "Number of rows purged by retention policy",
    ["table"],
)


async def purge_once() -> dict[str, int]:
    """Run all retention purges once. Returns counts per table."""
    now = datetime.now(timezone.utc)
    results: dict[str, int] = {}

    async with get_session() as session:
        # Alerts: hard delete (no soft delete for raw ops data)
        alerts_cutoff = now - timedelta(days=RETENTION_ALERTS_DAYS)
        r = await session.execute(
            "DELETE FROM alerts WHERE created_at < :cutoff RETURNING id",
            {"cutoff": alerts_cutoff},
        )
        results["alerts"] = len(r.fetchall())
        _rows_purged.labels(table="alerts").inc(results["alerts"])

        # Resources: aligned with TTL cache (30 days)
        resources_cutoff = now - timedelta(days=RETENTION_RESOURCES_DAYS)
        r = await session.execute(
            "DELETE FROM resources WHERE fetched_at < :cutoff RETURNING id",
            {"cutoff": resources_cutoff},
        )
        results["resources"] = len(r.fetchall())
        _rows_purged.labels(table="resources").inc(results["resources"])

        # DLQ: keep resolved entries for 14 days, then delete
        dlq_cutoff = now - timedelta(days=RETENTION_DLQ_RESOLVED_DAYS)
        r = await session.execute(
            "DELETE FROM dlq WHERE resolved_at IS NOT NULL AND resolved_at < :cutoff RETURNING id",
            {"cutoff": dlq_cutoff},
        )
        results["dlq"] = len(r.fetchall())
        _rows_purged.labels(table="dlq").inc(results["dlq"])

        # Worker heartbeat: 7 days
        heartbeat_cutoff = now - timedelta(days=RETENTION_HEARTBEAT_DAYS)
        r = await session.execute(
            "DELETE FROM worker_heartbeat WHERE last_heartbeat_at < :cutoff RETURNING worker_id",
            {"cutoff": heartbeat_cutoff},
        )
        results["worker_heartbeat"] = len(r.fetchall())
        _rows_purged.labels(table="worker_heartbeat").inc(results["worker_heartbeat"])

        await session.commit()

    logger.info("retention.purge", extra={"counts": results})
    return results


async def run_forever(interval_seconds: int = 86400) -> None:
    """Run purge every `interval_seconds` (default 24h). For cron-like use."""
    while True:
        try:
            await purge_once()
        except Exception as exc:  # noqa: BLE001 — log + continue, never crash cron
            logger.error("retention.purge_failed", extra={"error": str(exc)})
        await asyncio.sleep(interval_seconds)


def main() -> None:
    """Entry point: `uv run python -m ai_cloud_ops.retention` for cron."""
    from ai_cloud_ops.logging_config import configure_logging

    configure_logging(level=os.environ.get("LOG_LEVEL", "INFO"))
    asyncio.run(run_forever())


if __name__ == "__main__":
    main()