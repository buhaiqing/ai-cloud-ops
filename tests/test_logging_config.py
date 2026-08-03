"""Tests for application-wide structured JSON logging."""

from __future__ import annotations

import json

import pytest

from ai_cloud_ops.logging_config import configure_logging, get_logger


def test_json_log_contains_required_fields(capsys: pytest.CaptureFixture[str]) -> None:
    configure_logging()
    get_logger("test.required").info("service started")

    payload = json.loads(capsys.readouterr().err)
    assert {
        "timestamp",
        "level",
        "logger",
        "message",
        "module",
        "func",
        "line",
        "process",
        "thread",
    } <= payload.keys()
    assert payload["level"] == "INFO"
    assert payload["logger"] == "test.required"
    assert payload["message"] == "service started"
    assert payload["timestamp"].endswith("+00:00")


def test_extra_fields_are_merged_at_top_level(capsys: object) -> None:
    configure_logging()
    get_logger("test.extra").info("alert", extra={"alert_id": "x", "account": "prod"})

    payload = json.loads(capsys.readouterr().err)
    assert payload["alert_id"] == "x"
    assert payload["account"] == "prod"


def test_configure_logging_does_not_duplicate_handlers(capsys: object) -> None:
    configure_logging()
    configure_logging()
    get_logger("test.idempotent").warning("once")

    lines = capsys.readouterr().err.splitlines()
    assert len(lines) == 1
    assert json.loads(lines[0])["message"] == "once"
