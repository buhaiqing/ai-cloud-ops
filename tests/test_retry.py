"""Unit tests for retry + DLQ handler (T6)."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from ai_cloud_ops.ingest.retry import DEFAULT_RETRY_DELAYS, RetryResult, with_retry


class _CountingFn:
    """A function that fails the first N times, then succeeds."""

    def __init__(self, succeed_after: int, return_value: Any = "ok") -> None:
        self.succeed_after = succeed_after
        self.return_value = return_value
        self.attempts = 0

    async def __call__(self) -> Any:
        self.attempts += 1
        if self.attempts <= self.succeed_after:
            raise RuntimeError(f"simulated failure attempt {self.attempts}")
        return self.return_value


@pytest.mark.asyncio
async def test_first_attempt_success_no_retry() -> None:
    fn = _CountingFn(succeed_after=0, return_value="done")
    result = await with_retry("test", {"foo": "bar"}, fn)
    assert result.succeeded is True
    assert result.attempts == 1
    assert fn.attempts == 1


@pytest.mark.asyncio
async def test_succeeds_after_two_retries() -> None:
    """Fails twice, succeeds third time. No DLQ."""
    fn = _CountingFn(succeed_after=2)
    # Use very short delays to keep test fast
    result = await with_retry("test", {}, fn, delays=(0.01, 0.01, 0.01))
    assert result.succeeded is True
    assert result.attempts == 3
    assert fn.attempts == 3


@pytest.mark.asyncio
async def test_persistent_failure_goes_to_dlq() -> None:
    """All retries exhausted → DLQ. We don't actually write to DB (no fixture),
    so we just verify the attempt count and succeeded=False."""
    fn = _CountingFn(succeed_after=999)

    # Patch the DB session to be a no-op (avoid real DB connection)
    from ai_cloud_ops import db
    from contextlib import asynccontextmanager

    @asynccontextmanager
    async def fake_session():
        class FakeResult:
            def scalar_one(self) -> int:
                return 999

        class FakeSession:
            async def execute(self, *args, **kwargs) -> FakeResult:
                return FakeResult()

            async def commit(self) -> None:
                pass

        yield FakeSession()

    db.get_session = fake_session  # type: ignore[assignment]

    result = await with_retry("test", {"payload_key": "value"}, fn, delays=(0.01, 0.01, 0.01))
    assert result.succeeded is False
    assert result.attempts == 4  # initial + 3 retries
    assert result.dlq_id == 999


def test_default_retry_delays_follow_exponential_backoff() -> None:
    """Sanity: 1s → 2s → 4s is exponential with ratio 2."""
    assert DEFAULT_RETRY_DELAYS == (1.0, 2.0, 4.0)