"""Account & region configuration (T3).

Per design.md decision T3: per-account region list + static endpoint dictionary.
Loaded from a single YAML file at startup. Validated eagerly — fail loud, fail fast.

Schema:
    accounts:
      prod:
        role_arn: acs:ram::123:role/ai-cloud-ops
        regions: [cn-hangzhou, cn-beijing]
        endpoint_overrides: {}  # optional per-region overrides
      staging:
        role_arn: acs:ram::456:role/ai-cloud-ops
        regions: [cn-shanghai]
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, Field, field_validator


class AccountConfig(BaseModel):
    """Per-account configuration."""

    role_arn: str = Field(..., min_length=1)
    regions: list[str] = Field(..., min_length=1)
    endpoint_overrides: dict[str, str] = Field(default_factory=dict)

    @field_validator("regions")
    @classmethod
    def validate_regions(cls, v: list[str]) -> list[str]:
        # Aliyun region IDs are lowercase with hyphens, e.g. cn-hangzhou
        for r in v:
            if not r or not r.replace("-", "").isalnum():
                raise ValueError(f"invalid region ID: {r!r}")
        return v


class Config(BaseModel):
    """Top-level config."""

    accounts: dict[str, AccountConfig] = Field(..., min_length=1)


# Static endpoint dictionary for known regions. Override via endpoint_overrides
# in account config when Aliyun adds new regions or you need private endpoints.
DEFAULT_ENDPOINTS: dict[str, str] = {
    # CloudMonitor
    "cn-hangzhou": "cms.cn-hangzhou.aliyuncs.com",
    "cn-beijing": "cms.cn-beijing.aliyuncs.com",
    "cn-shanghai": "cms.cn-shanghai.aliyuncs.com",
    "cn-shenzhen": "cms.cn-shenzhen.aliyuncs.com",
    "cn-qingdao": "cms.cn-qingdao.aliyuncs.com",
    "cn-hongkong": "cms.cn-hongkong.aliyuncs.com",
    "ap-southeast-1": "cms.ap-southeast-1.aliyuncs.com",  # Singapore
    "ap-southeast-5": "cms.ap-southeast-5.aliyuncs.com",  # Malaysia
    "us-west-1": "cms.us-west-1.aliyuncs.com",
    # STS is region-agnostic — any STS endpoint works; default to Hangzhou
    "_sts_default": "sts.cn-hangzhou.aliyuncs.com",
}


def load_config(path: Path) -> Config:
    """Load and validate config from YAML file. Fail loudly on errors."""
    raw: dict[str, Any] = yaml.safe_load(path.read_text())
    return Config.model_validate(raw)


def endpoint_for(service: str, region: str, account: AccountConfig) -> str:
    """Resolve endpoint for a (service, region, account) combination.

    Per-account override takes precedence; falls back to the static dictionary.
    """
    if service == "sts":
        return DEFAULT_ENDPOINTS["_sts_default"]
    override_key = f"{service}.{region}"
    if override_key in account.endpoint_overrides:
        return account.endpoint_overrides[override_key]
    default_key = f"{region}"
    if default_key in DEFAULT_ENDPOINTS:
        return DEFAULT_ENDPOINTS[default_key]
    raise ValueError(
        f"no endpoint known for {service} in {region}; "
        f"add it to account.endpoint_overrides or update DEFAULT_ENDPOINTS"
    )