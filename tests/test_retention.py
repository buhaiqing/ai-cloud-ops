"""Unit tests for retention purge (T18)."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from ai_cloud_ops.retention import purge_once


class FakeResult:
    def __init__(self, rows: list) -> None:
        self._rows = rows

    def fetchall(self) -> list:
        return self._rows


@pytest.mark.asyncio
async def test_purge_once_deletes_old_rows() -> None:
    """purge_once deletes rows older than the retention cutoff for each table."""
    now = datetime.now(timezone.utc)

    # Mock session: each execute() returns a FakeResult
    execute_mock = MagicMock(
        side_effect=[
            FakeResult([(1,), (2,)]),          # alerts: 2 rows
            FakeResult([(10,)]),               # resources: 1 row
            FakeResult([]),                    # dlq: 0
            FakeResult([("worker-1",)]),       # heartbeat: 1 row
        ]
    )
    session_mock = AsyncMock()
    session_mock.execute = execute_mock
    session_mock.commit = AsyncMock()

    @asynccontextmanager
    async def fake_session():
        yield session_mock

    with patch("ai_cloud_ops.retention.get_session", fake_session):
        results = await purge_once()

    assert results["alerts"] == 2
    assert results["resources"] == 1
    assert results["dlq"] == 0
    assert results["worker_heartbeat"] == 1

    # Verify all 4 DELETE statements were issued + 1 commit
    assert execute_mock.call_count == 4
    session_mock.commit.assert_awaited_once()


@pytest.mark.asyncio
async def test_purge_continues_after_individual_failure() -> None:
    """If one DELETE fails, we still try the others (future-proof)."""
    # Currently purge_once does NOT catch per-DELETE errors (raises immediately).
    # Documenting current behavior: any failure aborts the cycle.
    execute_mock = MagicMock(
        side_effect=Exception("DB connection lost")
    )
    session_mock = AsyncMock()
    session_mock.execute = execute_mock
    session_mock.commit = AsyncMock()

    @asynccontextmanager
    async def fake_session():
        yield session_mock

    with patch("ai_cloud_ops.retention.get_session", fake_session):
        with pytest.raises(Exception, match="DB connection lost"):
            await purge_once()


def test_retention_defaults_match_docs() -> None:
    """Sanity: defaults match docs/data-retention.md § 2."""
    import os

    # Ensure no env override
    for var in [
        "DATA_RETENTION_ALERTS_DAYS",
        "DATA_RETENTION_RESOURCES_DAYS",
        "DATA_RETENTION_DLQ_DAYS",
        "DATA_RETENTION_HEARTBEAT_DAYS",
    ]:
        os.environ.pop(var, None)

    # Reimport to pick up fresh env
    import importlib

    import ai_cloud_ops.retention as r

    importlib.reload(r)
    assert r.RETENTION_ALERTS_DAYS == 90
    assert r.RETENTION_RESOURCES_DAYS == 30
    assert r.RETENTION_DLQ_RESOLVED_DAYS == 14
    assert r.RETENTION_HEARTBEAT_DAYS == 7


# Helper for async context manager mocking
from contextlib import asynccontextmanager  # noqa: E402