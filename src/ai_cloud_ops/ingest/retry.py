"""Retry with exponential backoff + DLQ persistence (T6).

Per design.md decision T6:
- Exponential backoff: 1s → 2s → 4s → 8s (max 3 retries)
- After max retries → persist to DLQ table (manual replay later)
- Wrap any call site: `await with_retry(job_type, payload, fn)`

Ponytail: stdlib `asyncio` + tenacity would be over-engineered — 30 lines.
"""

from __future__ import annotations

import asyncio
import json
import logging
import traceback
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import Any, Awaitable, Callable, TypeVar

from prometheus_client import Counter, Histogram

from ai_cloud_ops.db import get_session

logger = logging.getLogger(__name__)

T = TypeVar("T")

DEFAULT_RETRY_DELAYS = (1.0, 2.0, 4.0)  # seconds; 3 retries then DLQ

_retries_total = Counter(
    "ai_cloud_ops_retries_total",
    "Number of retry attempts",
    ["job_type", "outcome"],  # outcome: success | retry | dlq
)
_retry_latency = Histogram(
    "ai_cloud_ops_retry_latency_seconds",
    "Time spent in retry handler",
    ["job_type"],
)


@dataclass(frozen=True)
class RetryResult:
    """Outcome of a retry-wrapped operation."""

    value: T | None
    attempts: int
    succeeded: bool
    dlq_id: int | None = None


async def with_retry(
    job_type: str,
    payload: dict[str, Any],
    fn: Callable[[], Awaitable[T]],
    delays: tuple[float, ...] = DEFAULT_RETRY_DELAYS,
) -> RetryResult:
    """Call fn() with exponential backoff; on final failure, write to DLQ.

    Args:
        job_type: 'ingest' | 'analyze' | 'webhook' (for metrics + DLQ classification)
        payload: serializable payload to persist in DLQ if all retries fail
        fn: the operation to invoke
        delays: backoff schedule in seconds
    """
    attempts = 0
    last_exc: BaseException | None = None
    with _retry_latency.labels(job_type=job_type).time():
        for delay in (0.0, *delays):
            attempts += 1
            if delay > 0:
                await asyncio.sleep(delay)
            try:
                value = await fn()
                _retries_total.labels(job_type=job_type, outcome="success").inc()
                return RetryResult(value=value, attempts=attempts, succeeded=True)
            except Exception as exc:  # noqa: BLE001 — retry handler catches all
                last_exc = exc
                logger.warning(
                    "retry: %s attempt %d/%d failed: %s",
                    job_type,
                    attempts,
                    len(delays) + 1,
                    exc,
                )
                _retries_total.labels(job_type=job_type, outcome="retry").inc()

        # All retries exhausted → DLQ
        _retries_total.labels(job_type=job_type, outcome="dlq").inc()
        dlq_id = await _write_dlq(job_type=job_type, payload=payload, exc=last_exc)
        return RetryResult(value=None, attempts=attempts, succeeded=False, dlq_id=dlq_id)


async def _write_dlq(
    job_type: str, payload: dict[str, Any], exc: BaseException | None
) -> int:
    """Persist failed job to dlq table. Returns dlq_id."""
    error_message = str(exc) if exc else "unknown"
    error_class = type(exc).__name__ if exc else None
    next_retry = datetime.now(timezone.utc) + timedelta(minutes=15)  # manual replay window

    async with get_session() as session:
        result = await session.execute(
            """
            INSERT INTO dlq
                (job_type, payload, error_message, error_class, retry_count,
                 last_attempt_at, next_retry_at, created_at)
            VALUES
                (:job_type, CAST(:payload AS JSONB), :error_message, :error_class,
                 :retry_count, :last_attempt_at, :next_retry_at, :created_at)
            RETURNING id
            """,
            {
                "job_type": job_type,
                "payload": json.dumps(payload),
                "error_message": error_message,
                "error_class": error_class,
                "retry_count": len(DEFAULT_RETRY_DELAYS) + 1,
                "last_attempt_at": datetime.now(timezone.utc),
                "next_retry_at": next_retry,
                "created_at": datetime.now(timezone.utc),
            },
        )
        dlq_id = result.scalar_one()
        await session.commit()
    logger.error(
        "DLQ write: job_type=%s id=%d error=%s\n%s",
        job_type,
        dlq_id,
        error_message,
        "".join(traceback.format_exception(type(exc), exc, exc.__traceback__))
        if exc
        else "",
    )
    return dlq_id