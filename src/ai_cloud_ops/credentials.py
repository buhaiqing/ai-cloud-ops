"""STS Token management with in-process LRU cache (T1).

Per design.md decision T1:
- Process-local LRU cache (TTL=2700s, refresh 5 min before expiry)
- Used by all Aliyun API calls to avoid AssumeRole on every request
- Cache hit/miss metrics exposed via prometheus_client

This module does NOT manage the initial AccessKey (that's in .env, T5).
It manages: how often we exchange that initial AccessKey for an STS Token.
"""

from __future__ import annotations

import asyncio
import time
from dataclasses import dataclass
from typing import Protocol

from prometheus_client import Counter, Gauge

# Cache TTL: 45 minutes (refresh 15 min before STS's 1-hour expiry by default)
DEFAULT_TTL_SECONDS = 2700

# Refresh this many seconds before expiry, to avoid races with STS-side expiry
REFRESH_MARGIN_SECONDS = 300

# Metrics
_cache_hits = Counter(
    "ai_cloud_ops_sts_cache_hits_total",
    "Number of STS token cache hits",
    ["account"],
)
_cache_misses = Counter(
    "ai_cloud_ops_sts_cache_misses_total",
    "Number of STS token cache misses",
    ["account"],
)
_cache_size = Gauge(
    "ai_cloud_ops_sts_cache_size",
    "Number of STS tokens currently cached",
)


class StsAssumeRoleError(Exception):
    """Failed to assume role via STS."""


@dataclass(frozen=True)
class StsCredentials:
    """Temporary credentials for an Alibaba Cloud RAM role."""

    access_key_id: str
    access_key_secret: str
    security_token: str
    expiration: float  # Unix epoch seconds


class StsAssumeRoleClient(Protocol):
    """Protocol for the STS client. Real implementation wraps Aliyun SDK."""

    async def assume_role(
        self,
        account: str,
        role_arn: str,
        duration_seconds: int = 3600,
    ) -> StsCredentials: ...


class StsTokenCache:
    """In-process LRU-style cache for STS tokens, keyed by account.

    Ponytail: single-purpose dict with TTL — no cache library.
    """

    def __init__(
        self,
        assume_role_client: StsAssumeRoleClient,
        ttl_seconds: int = DEFAULT_TTL_SECONDS,
        refresh_margin_seconds: int = REFRESH_MARGIN_SECONDS,
    ) -> None:
        self._client = assume_role_client
        self._ttl = ttl_seconds
        self._refresh_margin = refresh_margin_seconds
        self._cache: dict[str, StsCredentials] = {}
        self._lock = asyncio.Lock()

    async def get(self, account: str, role_arn: str) -> StsCredentials:
        """Get STS credentials for an account, refreshing if expired."""
        async with self._lock:
            cached = self._cache.get(account)
            now = time.monotonic()
            if cached is not None and cached.expiration > now + self._refresh_margin:
                _cache_hits.labels(account=account).inc()
                return cached
            _cache_misses.labels(account=account).inc()
            creds = await self._client.assume_role(
                account=account,
                role_arn=role_arn,
                duration_seconds=self._ttl + self._refresh_margin,
            )
            # Translate wall-clock expiration to monotonic time
            wall_now = time.time()
            monotonic_expiration = now + (creds.expiration - wall_now)
            stored = StsCredentials(
                access_key_id=creds.access_key_id,
                access_key_secret=creds.access_key_secret,
                security_token=creds.security_token,
                expiration=monotonic_expiration,
            )
            self._cache[account] = stored
            _cache_size.set(len(self._cache))
            return stored

    def invalidate(self, account: str) -> None:
        """Drop cached creds for an account (e.g., after AssumeRole 403)."""
        if account in self._cache:
            del self._cache[account]
            _cache_size.set(len(self._cache))

    def size(self) -> int:
        """Current number of cached accounts (for tests / metrics)."""
        return len(self._cache)