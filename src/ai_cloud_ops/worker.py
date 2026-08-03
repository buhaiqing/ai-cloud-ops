"""Long-running alert analysis worker."""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import time
from datetime import UTC, datetime
from functools import partial
from pathlib import Path

from sqlalchemy import text

from ai_cloud_ops.agent.client import diagnose
from ai_cloud_ops.config import Config, load_config
from ai_cloud_ops.db import get_session
from ai_cloud_ops.ingest.retry import with_retry

logger = logging.getLogger(__name__)

_ALERT_QUERY = text(
    """
    SELECT a.*
    FROM alerts AS a
    LEFT JOIN analyses AS analysis ON analysis.alert_id = a.id
    WHERE analysis.id IS NULL
    ORDER BY a.created_at DESC
    LIMIT 50
    """
)
_UPDATE_STATUS = text(
    """
    UPDATE alerts
    SET status = 'analyzed', updated_at = :updated_at
    WHERE id = :id AND created_at = :created_at
    """
)
_UPSERT_HEARTBEAT = text(
    """
    INSERT INTO worker_heartbeat (worker_id, last_heartbeat_at)
    VALUES (:worker_id, :heartbeat_at)
    ON CONFLICT (worker_id) DO UPDATE
    SET last_heartbeat_at = EXCLUDED.last_heartbeat_at
    """
)


class AlertWorker:
    def __init__(self, config: Config, poll_interval_seconds: int = 30):
        self.config = config
        self.poll_interval = poll_interval_seconds
        self._running = False
        self._task: asyncio.Task[None] | None = None
        self._stop_event = asyncio.Event()

    async def start(self) -> None:
        """Start the background polling loop. Idempotent."""
        if self._task is not None and not self._task.done():
            return
        self._running = True
        self._stop_event.clear()
        self._task = asyncio.create_task(self.run_forever(), name="alert-worker")

    async def stop(self) -> None:
        """Gracefully stop. Waits for current cycle to finish."""
        self._running = False
        self._stop_event.set()
        task = self._task
        if task is not None and task is not asyncio.current_task():
            await task

    async def run_forever(self) -> None:
        """Run until stop() is called."""
        current_task = asyncio.current_task()
        started_directly = self._task is None
        if started_directly:
            self._task = current_task
            self._running = True
            self._stop_event.clear()

        try:
            while self._running:
                try:
                    await self.process_one_cycle()
                except Exception as exc:  # noqa: BLE001 - keep the worker alive
                    logger.exception("worker.cycle.failed", extra={"error": str(exc)})
                if self._running:
                    try:
                        await asyncio.wait_for(
                            self._stop_event.wait(), timeout=self.poll_interval
                        )
                    except TimeoutError:
                        pass
        finally:
            self._running = False
            if self._task is current_task:
                self._task = None

    async def process_one_cycle(self) -> int:
        """Analyze up to 50 alerts without analyses and return the processed count."""
        started_at = time.monotonic()
        processed = 0
        errors = 0

        async with get_session() as session:
            result = await session.execute(_ALERT_QUERY)
            alerts = [dict(row) for row in result.mappings().all()]

            for alert in alerts:
                try:
                    retry_result = await with_retry(
                        "analyze",
                        {"alert_id": alert["id"]},
                        partial(diagnose, alert),
                    )
                    if not retry_result.succeeded:
                        errors += 1
                        continue
                    now = datetime.now(UTC)
                    await session.execute(
                        _UPDATE_STATUS,
                        {"id": alert["id"], "created_at": alert["created_at"], "updated_at": now},
                    )
                    processed += 1
                except Exception as exc:  # noqa: BLE001 - one alert must not block the batch
                    errors += 1
                    logger.exception(
                        "worker.alert.failed",
                        extra={"alert_id": alert.get("id"), "error": str(exc)},
                    )

            await session.execute(
                _UPSERT_HEARTBEAT,
                {"worker_id": "alert-worker", "heartbeat_at": datetime.now(UTC)},
            )
            await session.commit()

        logger.info(
            "worker.cycle.complete",
            extra={
                "processed": processed,
                "errors": errors,
                "duration_seconds": round(time.monotonic() - started_at, 3),
            },
        )
        return processed


async def _main() -> None:
    config_path = Path(os.environ.get("ACCOUNTS_CONFIG_PATH", "./accounts.yaml"))
    worker = AlertWorker(load_config(config_path))
    loop = asyncio.get_running_loop()

    for sig in (signal.SIGTERM, signal.SIGINT):
        try:
            loop.add_signal_handler(sig, lambda: asyncio.create_task(worker.stop()))
        except NotImplementedError:  # pragma: no cover - Windows event loops
            pass

    await worker.run_forever()


def main() -> None:
    """Load configuration and run the worker until interrupted."""
    asyncio.run(_main())


if __name__ == "__main__":
    main()
