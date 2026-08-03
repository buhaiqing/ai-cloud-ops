"""Unit tests for config loading + endpoint resolution (T3)."""

from __future__ import annotations

from pathlib import Path

import pytest

from ai_cloud_ops.config import Config, endpoint_for, load_config


def test_minimal_config_loads(tmp_path: Path) -> None:
    cfg_path = tmp_path / "accounts.yaml"
    cfg_path.write_text(
        """
accounts:
  prod:
    role_arn: acs:ram::123:role/ai-cloud-ops
    regions: [cn-hangzhou, cn-beijing]
"""
    )
    cfg = load_config(cfg_path)
    assert "prod" in cfg.accounts
    assert cfg.accounts["prod"].role_arn == "acs:ram::123:role/ai-cloud-ops"
    assert cfg.accounts["prod"].regions == ["cn-hangzhou", "cn-beijing"]


def test_empty_accounts_rejected() -> None:
    with pytest.raises(Exception):  # ValidationError
        Config.model_validate({"accounts": {}})


def test_invalid_region_rejected() -> None:
    with pytest.raises(Exception):
        Config.model_validate(
            {
                "accounts": {
                    "prod": {
                        "role_arn": "acs:ram::1:role/x",
                        "regions": ["cn hangzhou"],  # space — invalid
                    }
                }
            }
        )


def test_endpoint_for_known_region() -> None:
    from ai_cloud_ops.config import AccountConfig

    account = AccountConfig(role_arn="arn", regions=["cn-hangzhou"])
    ep = endpoint_for("cms", "cn-hangzhou", account)
    assert ep == "cms.cn-hangzhou.aliyuncs.com"


def test_endpoint_for_unknown_region_raises() -> None:
    from ai_cloud_ops.config import AccountConfig

    account = AccountConfig(role_arn="arn", regions=["mars-1"])  # fictitious region
    with pytest.raises(ValueError, match="no endpoint known"):
        endpoint_for("cms", "mars-1", account)


def test_endpoint_override_takes_precedence() -> None:
    from ai_cloud_ops.config import AccountConfig

    account = AccountConfig(
        role_arn="arn",
        regions=["cn-hangzhou"],
        endpoint_overrides={"cms.cn-hangzhou": "cms.internal.example.com"},
    )
    assert endpoint_for("cms", "cn-hangzhou", account) == "cms.internal.example.com"


def test_sts_endpoint_is_region_agnostic() -> None:
    from ai_cloud_ops.config import AccountConfig

    account = AccountConfig(role_arn="arn", regions=["cn-hangzhou", "cn-beijing"])
    # STS endpoint should be the same regardless of region
    assert endpoint_for("sts", "cn-hangzhou", account) == endpoint_for("sts", "cn-beijing", account)