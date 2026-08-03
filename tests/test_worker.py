"""Tests for the alert worker lifecycle and processing loop."""

from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from ai_cloud_ops.config import Config
from ai_cloud_ops.worker import AlertWorker


class _Result:
    def __init__(self, rows: list[dict]) -> None:
        self.rows = rows

    def mappings(self) -> _Result:
        return self

    def all(self) -> list[dict]:
        return self.rows


def _config() -> Config:
    return Config.model_validate(
        {"accounts": {"test": {"role_arn": "role", "regions": ["cn-hangzhou"]}}}
    )


def _session_context(rows: list[dict]) -> tuple[MagicMock, object]:
    session = MagicMock()
    session.execute = AsyncMock(side_effect=[_Result(rows), MagicMock(), MagicMock(), MagicMock()])
    session.commit = AsyncMock()

    @asynccontextmanager
    async def context():
        yield session

    return session, context


@pytest.mark.asyncio
async def test_process_one_cycle_diagnoses_each_alert() -> None:
    alerts = [
        {"id": 1, "created_at": datetime.now(timezone.utc), "alert_id": "a-1"},
        {"id": 2, "created_at": datetime.now(timezone.utc), "alert_id": "a-2"},
    ]
    session, context = _session_context(alerts)
    diagnose_mock = AsyncMock()

    with patch("ai_cloud_ops.worker.get_session", return_value=context()), patch(
        "ai_cloud_ops.worker.diagnose", diagnose_mock
    ):
        processed = await AlertWorker(_config()).process_one_cycle()

    assert processed == 2
    assert diagnose_mock.await_count == 2
    assert [call.args[0] for call in diagnose_mock.await_args_list] == alerts
    session.commit.assert_awaited_once()


@pytest.mark.asyncio
async def test_start_is_idempotent() -> None:
    worker = AlertWorker(_config(), poll_interval_seconds=60)
    cycle_started = asyncio.Event()
    release_cycle = asyncio.Event()

    async def cycle() -> int:
        cycle_started.set()
        await release_cycle.wait()
        return 0

    with patch.object(worker, "process_one_cycle", side_effect=cycle):
        await worker.start()
        await cycle_started.wait()
        first_task = worker._task
        await worker.start()
        assert worker._task is first_task
        release_cycle.set()
        await worker.stop()


@pytest.mark.asyncio
async def test_stop_waits_for_current_cycle() -> None:
    worker = AlertWorker(_config(), poll_interval_seconds=60)
    cycle_started = asyncio.Event()
    release_cycle = asyncio.Event()

    async def cycle() -> int:
        cycle_started.set()
        await release_cycle.wait()
        return 0

    with patch.object(worker, "process_one_cycle", side_effect=cycle):
        await worker.start()
        await cycle_started.wait()
        stop_task = asyncio.create_task(worker.stop())
        await asyncio.sleep(0)
        assert not stop_task.done()
        release_cycle.set()
        await stop_task
        assert worker._task is None
        assert worker._running is False
