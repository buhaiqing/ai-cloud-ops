"""In-process TTL cache for resource metadata (T10).

Per design.md decision T10: 5-15 min TTL + active invalidation.
Used by the AI Agent to cache DescribeInstances / DescribeDBInstances results
so repeated alerts about the same resource don't burn API quota.

Ponytail: dict + timestamp — no cache library.
"""

from __future__ import annotations

import asyncio
import time
from typing import Awaitable, Callable, TypeVar

from prometheus_client import Counter

V = TypeVar("V")

_hits = Counter("ai_cloud_ops_resource_cache_hits_total", "Resource cache hits", ["key"])
_misses = Counter("ai_cloud_ops_resource_cache_misses_total", "Resource cache misses", ["key"])


class TTLCache:
    """Async TTL cache with invalidation.

    Single-process, single-flight: concurrent gets for the same key share one
    in-flight fetch (the second waits on the first's coroutine).
    """

    def __init__(self, ttl_seconds: float = 600.0) -> None:
        self._ttl = ttl_seconds
        self._store: dict[str, tuple[float, V]] = {}
        self._in_flight: dict[str, asyncio.Future[V]] = {}
        self._lock = asyncio.Lock()

    async def get_or_fetch(
        self,
        key: str,
        fetcher: Callable[[], Awaitable[V]],
    ) -> V:
        """Return cached value if fresh, else fetch via fetcher() and cache it."""
        # Fast path: hit (no lock needed for read)
        entry = self._store.get(key)
        if entry is not None and (time.monotonic() - entry[0]) < self._ttl:
            _hits.labels(key=key).inc()
            return entry[1]

        # Slow path: dedupe concurrent fetches
        async with self._lock:
            existing = self._in_flight.get(key)
            if existing is not None:
                return await existing

            future: asyncio.Future[V] = asyncio.get_event_loop().create_future()
            self._in_flight[key] = future
            _misses.labels(key=key).inc()

        try:
            value = await fetcher()
        except Exception:
            async with self._lock:
                self._in_flight.pop(key, None)
                if not future.done():
                    future.set_exception(  # type: ignore[arg-type]
                        Exception("fetch failed"),
                    )
            raise

        async with self._lock:
            self._store[key] = (time.monotonic(), value)
            self._in_flight.pop(key, None)
            if not future.done():
                future.set_result(value)
        return value

    def invalidate(self, key: str) -> None:
        """Drop a specific key."""
        self._store.pop(key, None)

    def invalidate_all(self) -> None:
        """Drop everything (e.g., after major config change)."""
        self._store.clear()

    def size(self) -> int:
        return len(self._store)