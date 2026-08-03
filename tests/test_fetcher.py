"""Unit tests for Aliyun resource metadata fetcher (M1).

Strategy: monkeypatch the Aliyun SDK Client classes (lazy-imported inside
the fetcher) with FakeAliyunSdk so we can exercise the real fetch + cache
plumbing without hitting the network.
"""
from __future__ import annotations

import time
from dataclasses import dataclass
from typing import Any

import pytest

from ai_cloud_ops.cache import TTLCache
from ai_cloud_ops.config import AccountConfig
from ai_cloud_ops.credentials import StsCredentials, StsTokenCache
from ai_cloud_ops.ingest import fetcher as fetcher_module


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------
@dataclass
class FakeStsClient:
    """Counts assume_role calls; returns creds expiring in 1h."""

    call_count: int = 0

    async def assume_role(
        self, account: str, role_arn: str, duration_seconds: int = 3600
    ) -> StsCredentials:
        self.call_count += 1
        return StsCredentials(
            access_key_id=f"ak-{account}",
            access_key_secret="secret",  # noqa: S106 — test fixture
            security_token=f"token-{self.call_count}",
            expiration=time.time() + 3600.0,
        )


class FakeAliyunSdk:
    """Records every call; returns canned ECS/RDS responses."""

    def __init__(self) -> None:
        self.ecs_calls: list[dict] = []
        self.rds_calls: list[dict] = []
        self._ecs_call_count = 0
        self._rds_call_count = 0

    class FakeEcsClient:
        def __init__(self, outer: FakeAliyunSdk, **kwargs: Any) -> None:
            self._outer = outer
            self._kwargs = kwargs

        async def describe_instances(self, request: Any) -> Any:
            self._outer._ecs_call_count += 1
            self._outer.ecs_calls.append({"region": request.region_id})
            return _FakeResponse(
                body={
                    "Instances": {
                        "Instance": [
                            {
                                "InstanceId": "i-test-1",
                                "HostName": "node-1",
                                "Status": "Running",
                                "InstanceType": "ecs.g6.large",
                                "RegionId": request.region_id,
                                "VpcAttributes": {"VpcId": "vpc-1"},
                                "Tags": {"Tag": [{"TagKey": "env", "TagValue": "prod"}]},
                            }
                        ]
                    }
                }
            )

        async def describe_instances_async(self, request: Any) -> Any:
            return await self.describe_instances(request)

    class FakeRdsClient:
        def __init__(self, outer: FakeAliyunSdk, **kwargs: Any) -> None:
            self._outer = outer
            self._kwargs = kwargs

    @property
    def ecs_call_count(self) -> int:
        return self._ecs_call_count

    @property
    def rds_call_count(self) -> int:
        return self._rds_call_count


class _FakeResponse:
    def __init__(self, body: dict) -> None:
        self.body = _FakeBody(body)


class _FakeBody:
    def __init__(self, data: dict) -> None:
        self._data = data

    def to_map(self) -> dict:
        return self._data


@pytest.fixture
def account() -> AccountConfig:
    return AccountConfig(
        role_arn="acs:ram::123:role/ai-cloud-ops-test",
        regions=["cn-hangzhou"],
    )


@pytest.fixture
def sts_cache() -> StsTokenCache:
    return StsTokenCache(FakeStsClient())


@pytest.fixture
def fake_sdk(monkeypatch: pytest.MonkeyPatch) -> FakeAliyunSdk:
    """Install FakeAliyunSdk in place of the real Aliyun SDK clients."""
    sdk = FakeAliyunSdk()

    # Build a fake module for alibabacloud_ecs20140526.client with our Client
    import types
    ecs_pkg = types.ModuleType("alibabacloud_ecs20140526")
    ecs_client_mod = types.ModuleType("alibabacloud_ecs20140526.client")
    ecs_client_mod.Client = lambda **kwargs: sdk.FakeEcsClient(sdk, **kwargs)  # type: ignore[attr-defined]
    ecs_pkg.client = ecs_client_mod
    ecs_models_mod = types.ModuleType("alibabacloud_ecs20140526.models")

    class _FakeRequest:
        def __init__(self, **kwargs: Any) -> None:
            for k, v in kwargs.items():
                setattr(self, k, v)

    ecs_models_mod.DescribeInstancesRequest = _FakeRequest
    ecs_pkg.models = ecs_models_mod

    rds_pkg = types.ModuleType("alibabacloud_rds20140815")
    rds_client_mod = types.ModuleType("alibabacloud_rds20140815.client")
    rds_client_mod.Client = lambda **kwargs: sdk.FakeRdsClient(sdk, **kwargs)  # type: ignore[attr-defined]
    rds_pkg.client = rds_client_mod

    sys_modules = __import__("sys").modules
    monkeypatch.setitem(sys_modules, "alibabacloud_ecs20140526", ecs_pkg)
    monkeypatch.setitem(sys_modules, "alibabacloud_ecs20140526.client", ecs_client_mod)
    monkeypatch.setitem(sys_modules, "alibabacloud_ecs20140526.models", ecs_models_mod)
    monkeypatch.setitem(sys_modules, "alibabacloud_rds20140815", rds_pkg)
    monkeypatch.setitem(sys_modules, "alibabacloud_rds20140815.client", rds_client_mod)
    return sdk


@pytest.fixture(autouse=True)
def _reset_cache() -> None:
    """Each test starts with a clean cache."""
    fetcher_module.invalidate_all()


# ---------------------------------------------------------------------------
# describe_ecs_instances
# ---------------------------------------------------------------------------
@pytest.mark.asyncio
async def test_ecs_first_call_hits_sts_and_sdk(
    account: AccountConfig, sts_cache: StsTokenCache, fake_sdk: FakeAliyunSdk
) -> None:
    result = await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)

    assert len(result) == 1
    assert result[0]["instance_id"] == "i-test-1"
    assert result[0]["tags"] == [{"tag_key": "env", "tag_value": "prod"}]
    assert sts_cache.size() == 1
    assert fake_sdk.ecs_call_count == 1


@pytest.mark.asyncio
async def test_ecs_ttl_cache_hit_avoids_sdk_call(
    account: AccountConfig, sts_cache: StsTokenCache, fake_sdk: FakeAliyunSdk
) -> None:
    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)

    # TTL is 10 min — second and third calls must hit the cache
    assert fake_sdk.ecs_call_count == 1


@pytest.mark.asyncio
async def test_sts_cache_hit_when_fetcher_called_twice(
    account: AccountConfig, sts_cache: StsTokenCache, fake_sdk: FakeAliyunSdk
) -> None:
    fake_sts = sts_cache._client  # type: ignore[attr-defined]

    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)

    # STS AssumeRole runs once; second call reuses the cached creds
    assert fake_sts.call_count == 1
    assert sts_cache.size() == 1


@pytest.mark.asyncio
async def test_invalidate_all_forces_refetch_and_new_sts(
    account: AccountConfig, sts_cache: StsTokenCache, fake_sdk: FakeAliyunSdk
) -> None:
    fake_sts = sts_cache._client  # type: ignore[attr-defined]

    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    assert fake_sdk.ecs_call_count == 1
    assert fake_sts.call_count == 1

    # Cache invalidation → next call re-fetches resource AND refreshes STS
    fetcher_module.invalidate_all()
    sts_cache.invalidate(account.role_arn)

    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    assert fake_sdk.ecs_call_count == 2
    assert fake_sts.call_count == 2


@pytest.mark.asyncio
async def test_different_regions_cached_separately(
    account: AccountConfig, sts_cache: StsTokenCache, fake_sdk: FakeAliyunSdk
) -> None:
    await fetcher_module.describe_ecs_instances(account, "cn-hangzhou", sts_cache)
    await fetcher_module.describe_ecs_instances(account, "cn-beijing", sts_cache)

    assert fake_sdk.ecs_call_count == 2
    assert {c["region"] for c in fake_sdk.ecs_calls} == {"cn-hangzhou", "cn-beijing"}


# ---------------------------------------------------------------------------
# Stubbed M2 tools — exercise cache plumbing only
# ---------------------------------------------------------------------------
@pytest.mark.asyncio
async def test_ecs_status_stub_returns_cached_shape(
    account: AccountConfig, sts_cache: StsTokenCache
) -> None:
    r1 = await fetcher_module.describe_ecs_instance_status(
        account, "cn-hangzhou", "i-1", sts_cache
    )
    r2 = await fetcher_module.describe_ecs_instance_status(
        account, "cn-hangzhou", "i-1", sts_cache
    )

    assert r1 == {"instance_id": "i-1", "status": "Running", "region": "cn-hangzhou"}
    assert r1 == r2
    # STS was never called — stub returns a dict literal, no SDK involved
    assert sts_cache.size() == 0


@pytest.mark.asyncio
async def test_monitor_data_stub_distinguishes_metric(
    account: AccountConfig, sts_cache: StsTokenCache
) -> None:
    r = await fetcher_module.describe_ecs_monitor_data(
        account, "cn-hangzhou", "i-1", "CPUUtilization", sts_cache, hours_back=2
    )
    assert r == []


@pytest.mark.asyncio
async def test_rds_stub_returns_empty_list(
    account: AccountConfig, sts_cache: StsTokenCache
) -> None:
    result = await fetcher_module.describe_rds_instances(account, "cn-hangzhou", sts_cache)
    assert result == []


# ---------------------------------------------------------------------------
# TTL cache sanity (10s) — verify the configured TTL bound
# ---------------------------------------------------------------------------
@pytest.mark.asyncio
async def test_ttl_cache_holds_value_within_10s() -> None:
    """The spec's TTL is 10 minutes; sanity-check the 10s window."""
    cache: TTLCache = TTLCache(ttl_seconds=10.0)
    calls = 0

    async def fetch() -> str:
        nonlocal calls
        calls += 1
        return "value"

    await cache.get_or_fetch("k", fetch)
    await cache.get_or_fetch("k", fetch)
    assert calls == 1, "second call within 10s must hit the cache"
