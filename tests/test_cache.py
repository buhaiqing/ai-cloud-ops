"""Unit tests for resource metadata TTL cache (T10)."""

from __future__ import annotations

import asyncio

import pytest

from ai_cloud_ops.cache import TTLCache


@pytest.mark.asyncio
async def test_cache_hit() -> None:
    cache = TTLCache[str](ttl_seconds=60.0)
    calls = 0

    async def fetch() -> str:
        nonlocal calls
        calls += 1
        return "value-1"

    # First call: miss
    v1 = await cache.get_or_fetch("k1", fetch)
    assert v1 == "value-1"
    assert calls == 1

    # Second call: hit (no extra fetch)
    v2 = await cache.get_or_fetch("k1", fetch)
    assert v2 == "value-1"
    assert calls == 1


@pytest.mark.asyncio
async def test_cache_invalidate() -> None:
    cache = TTLCache[str](ttl_seconds=60.0)
    calls = 0

    async def fetch() -> str:
        nonlocal calls
        calls += 1
        return f"v{calls}"

    await cache.get_or_fetch("k1", fetch)
    cache.invalidate("k1")
    v = await cache.get_or_fetch("k1", fetch)
    assert v == "v2"
    assert calls == 2


@pytest.mark.asyncio
async def test_cache_invalidate_all() -> None:
    cache = TTLCache[str](ttl_seconds=60.0)

    async def fetch() -> str:
        return "v"

    await cache.get_or_fetch("k1", fetch)
    await cache.get_or_fetch("k2", fetch)
    assert cache.size() == 2
    cache.invalidate_all()
    assert cache.size() == 0


@pytest.mark.asyncio
async def test_concurrent_fetches_dedupe() -> None:
    """Two concurrent get_or_fetch for the same key share one fetcher call."""
    cache = TTLCache[str](ttl_seconds=60.0)
    calls = 0
    started = asyncio.Event()

    async def fetch() -> str:
        nonlocal calls
        calls += 1
        await asyncio.sleep(0.05)  # Simulate network
        return "shared-value"

    # Fire two concurrent fetches for the same key
    results = await asyncio.gather(cache.get_or_fetch("k1", fetch), cache.get_or_fetch("k1", fetch))
    assert results == ["shared-value", "shared-value"]
    assert calls == 1, f"expected 1 fetch, got {calls}"


@pytest.mark.asyncio
async def test_fetch_failure_propagates() -> None:
    cache = TTLCache[str](ttl_seconds=60.0)

    async def fetch() -> str:
        raise ValueError("upstream API down")

    with pytest.raises(ValueError):
        await cache.get_or_fetch("k1", fetch)

    # Should be able to retry — cache should not have stored the failure
    async def fetch_ok() -> str:
        return "now-ok"

    v = await cache.get_or_fetch("k1", fetch_ok)
    assert v == "now-ok"