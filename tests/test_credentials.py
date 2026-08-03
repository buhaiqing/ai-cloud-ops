"""Unit tests for STS Token cache (T1)."""

from __future__ import annotations

import asyncio
import time
from typing import Awaitable

import pytest

from ai_cloud_ops.credentials import (
    DEFAULT_TTL_SECONDS,
    REFRESH_MARGIN_SECONDS,
    StsAssumeRoleError,
    StsCredentials,
    StsTokenCache,
)


class FakeStsClient:
    """Counts calls; returns StsCredentials expiring in `seconds`."""

    def __init__(self, expire_after: float = 3600.0) -> None:
        self.call_count = 0
        self.expire_after = expire_after

    async def assume_role(
        self, account: str, role_arn: str, duration_seconds: int = 3600
    ) -> StsCredentials:
        self.call_count += 1
        return StsCredentials(
            access_key_id=f"ak-{account}-{self.call_count}",
            access_key_secret="secret",
            security_token="token",
            expiration=time.time() + self.expire_after,
        )


@pytest.mark.asyncio
async def test_first_call_misses_cache() -> None:
    client = FakeStsClient()
    cache = StsTokenCache(client)
    creds = await cache.get("prod", "acs:ram::123:role/x")
    assert creds.access_key_id == "ak-prod-1"
    assert client.call_count == 1
    assert cache.size() == 1


@pytest.mark.asyncio
async def test_second_call_hits_cache() -> None:
    client = FakeStsClient()
    cache = StsTokenCache(client)
    await cache.get("prod", "acs:ram::123:role/x")
    await cache.get("prod", "acs:ram::123:role/x")
    assert client.call_count == 1  # No second AssumeRole


@pytest.mark.asyncio
async def test_invalidate_triggers_refresh() -> None:
    client = FakeStsClient()
    cache = StsTokenCache(client)
    await cache.get("prod", "arn")
    cache.invalidate("prod")
    await cache.get("prod", "arn")
    assert client.call_count == 2


@pytest.mark.asyncio
async def test_different_accounts_cached_separately() -> None:
    client = FakeStsClient()
    cache = StsTokenCache(client)
    await cache.get("prod", "arn-prod")
    await cache.get("staging", "arn-staging")
    assert client.call_count == 2
    assert cache.size() == 2


@pytest.mark.asyncio
async def test_expiry_triggers_refresh() -> None:
    """If credentials are within refresh margin, cache should re-fetch."""
    # Token expires in 60 seconds — within the 300s refresh margin
    client = FakeStsClient(expire_after=60.0)
    cache = StsTokenCache(client, refresh_margin_seconds=REFRESH_MARGIN_SECONDS)
    await cache.get("prod", "arn")
    # Second call should see the token is near expiry and refresh
    await cache.get("prod", "arn")
    assert client.call_count == 2


@pytest.mark.asyncio
async def test_assume_role_error_propagates() -> None:
    class FailingClient:
        async def assume_role(self, account: str, role_arn: str, duration_seconds: int) -> StsCredentials:
            raise StsAssumeRoleError(f"assume role failed for {account}")

    cache = StsTokenCache(FailingClient())  # type: ignore[arg-type]
    with pytest.raises(StsAssumeRoleError):
        await cache.get("prod", "arn")


def test_default_ttl_and_margin() -> None:
    """Sanity: defaults are set for STS 1-hour token."""
    assert DEFAULT_TTL_SECONDS == 2700  # 45 min, refresh 15 min before expiry
    assert REFRESH_MARGIN_SECONDS == 300