"""Aliyun resource metadata fetcher (M1).

Per design.md T10: 5-15 min TTL on read-only resource metadata.
Per tool whitelist (T2): one public function per whitelisted tool.

Ponytail: lazy-import the SDK per call — never at module load. The whole
file is a thin dispatcher; no abstraction layer.
"""
from __future__ import annotations

import logging

from ai_cloud_ops.cache import TTLCache
from ai_cloud_ops.config import AccountConfig, endpoint_for
from ai_cloud_ops.credentials import StsTokenCache

logger = logging.getLogger(__name__)

# T10: 10 min default — instance metadata rarely changes
DEFAULT_TTL_SECONDS = 600.0
_cache: TTLCache = TTLCache(ttl_seconds=DEFAULT_TTL_SECONDS)


def _key(tool: str, account: AccountConfig, region: str, resource_id: str = "*") -> str:
    # role_arn is globally unique per RAM role → safe cache-key component.
    # AccountConfig has no `alias` field; role_arn stands in.
    return f"{tool}:{account.role_arn}:{region}:{resource_id}"


async def _ecs_client(account: AccountConfig, region: str, sts: StsTokenCache):
    from alibabacloud_ecs20140526.client import Client as EcsClient

    creds = await sts.get(account.role_arn, account.role_arn)
    return EcsClient(
        access_key_id=creds.access_key_id,
        access_key_secret=creds.access_key_secret,
        security_token=creds.security_token,
        endpoint=endpoint_for("ecs", region, account),
    )


async def _rds_client(account: AccountConfig, region: str, sts: StsTokenCache):
    from alibabacloud_rds20140815.client import Client as RdsClient

    creds = await sts.get(account.role_arn, account.role_arn)
    return RdsClient(
        access_key_id=creds.access_key_id,
        access_key_secret=creds.access_key_secret,
        security_token=creds.security_token,
        endpoint=endpoint_for("rds", region, account),
    )


# ---------------------------------------------------------------------------
# describe_ecs_instances — IMPLEMENTED (proves the pattern end-to-end)
# ---------------------------------------------------------------------------
async def describe_ecs_instances(
    account: AccountConfig, region: str, sts: StsTokenCache
) -> list[dict]:
    """List ECS instances in a region. Cached for DEFAULT_TTL_SECONDS."""
    from alibabacloud_ecs20140526 import models as ecs_models

    key = _key("describe_ecs_instances", account, region)

    async def fetch() -> list[dict]:
        client = await _ecs_client(account, region, sts)
        req = ecs_models.DescribeInstancesRequest(region_id=region)
        resp = await client.describe_instances_async(req) if hasattr(
            client, "describe_instances_async"
        ) else await client.describe_instances(req)
        body = resp.body.to_map() if resp.body else {}
        raw = body.get("Instances", {}).get("Instance", []) or []
        out: list[dict] = []
        for inst in raw:
            tag_block = inst.get("Tags", {}).get("Tag", []) if inst.get("Tags") else []
            tags = [{"tag_key": t.get("TagKey"), "tag_value": t.get("TagValue")} for t in tag_block]
            vpc = inst.get("VpcAttributes") or {}
            inner = inst.get("InnerIpAddress")
            inner_ip = (
                (inner.get("IpAddress", [None]) or [None])[0]
                if isinstance(inner, dict)
                else None
            )
            out.append(
                {
                    "instance_id": inst.get("InstanceId"),
                    "host_name": inst.get("HostName"),
                    "status": inst.get("Status"),
                    "instance_type": inst.get("InstanceType"),
                    "region_id": inst.get("RegionId"),
                    "vpc_id": vpc.get("VpcId"),
                    "inner_ip": inner_ip,
                    "tags": tags,
                }
            )
        return out

    return await _cache.get_or_fetch(key, fetch)


# ---------------------------------------------------------------------------
# Remaining M1 tools — STUBBED (TODO M2)
# ---------------------------------------------------------------------------
async def describe_ecs_instance_status(
    account: AccountConfig, region: str, instance_id: str, sts: StsTokenCache
) -> dict:
    """Get one ECS instance's status. STUB — TODO M2: real DescribeInstanceStatus."""
    key = _key("describe_ecs_instance_status", account, region, instance_id)

    async def fetch() -> dict:
        # ponytail: stub returns plausible shape; real impl in M2
        return {"instance_id": instance_id, "status": "Running", "region": region}

    return await _cache.get_or_fetch(key, fetch)


async def describe_ecs_monitor_data(
    account: AccountConfig,
    region: str,
    instance_id: str,
    metric: str,
    sts: StsTokenCache,
    hours_back: int = 1,
) -> list[dict]:
    """Pull monitor datapoints for one ECS instance. STUB — TODO M2."""
    key = _key("describe_ecs_monitor_data", account, region, f"{instance_id}:{metric}:{hours_back}")

    async def fetch() -> list[dict]:
        # ponytail: stub returns empty series; real impl in M2
        return []

    return await _cache.get_or_fetch(key, fetch)


async def describe_rds_instances(
    account: AccountConfig, region: str, sts: StsTokenCache
) -> list[dict]:
    """List RDS instances in a region. STUB — TODO M2: real DescribeDBInstances."""
    key = _key("describe_rds_instances", account, region)

    async def fetch() -> list[dict]:
        # ponytail: stub returns empty list; real impl in M2
        return []

    return await _cache.get_or_fetch(key, fetch)


def invalidate(tool: str, account: AccountConfig, region: str, resource_id: str = "*") -> None:
    """Drop a single cached entry (e.g., after a webhook signals a change)."""
    _cache.invalidate(_key(tool, account, region, resource_id))


def invalidate_all() -> None:
    """Drop every cached entry (e.g., after STS AssumeRole rotation)."""
    _cache.invalidate_all()
