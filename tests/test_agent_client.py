"""Unit tests for the AI Agent client (Claude tool-use loop + DLQ path).

Mocks AsyncAnthropic + the DB session — no real API calls, no real DB.
"""

from __future__ import annotations

import json
from contextlib import asynccontextmanager
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from ai_cloud_ops.agent import client
from ai_cloud_ops.agent.client import Diagnosis, diagnose


# --------------------------------------------------------------------------- #
# Mock helpers
# --------------------------------------------------------------------------- #


class _Block:
    """Stand-in for anthropic.types.Message.content[i] — duck-typed by .type."""

    def __init__(self, type_: str, **kwargs: Any) -> None:
        self.type = type_
        for k, v in kwargs.items():
            setattr(self, k, v)


def _text_block(text: str) -> _Block:
    return _Block("text", text=text)


def _tool_use_block(name: str, tool_id: str, input_: dict[str, Any]) -> _Block:
    return _Block("tool_use", name=name, id=tool_id, input=input_)


def _valid_diagnosis_json() -> str:
    return json.dumps(
        {
            "root_cause": "RDS CPU saturated due to missing index on orders.created_at",
            "recommendations": [
                {
                    "action": "add index",
                    "command": "aliyun rds CreateIndex ...",
                    "expected_outcome": "query time drops below 100ms",
                }
            ],
            "evidence_chains": [
                {
                    "claim": "CPU > 95% for 10m",
                    "supporting_tool": "describe_cms_metric_list",
                    "supporting_data": "avg=97.2",
                }
            ],
            "confidence": "high",
            "caveats": ["verify during business hours"],
        }
    )


def _make_response(content: list[_Block]) -> MagicMock:
    resp = MagicMock()
    resp.content = content
    return resp


def _make_client(*responses: MagicMock) -> AsyncMock:
    """Return an AsyncAnthropic whose messages.create yields responses in order."""
    anthropic_mock = MagicMock()
    anthropic_mock.messages = MagicMock()
    anthropic_mock.messages.create = AsyncMock(side_effect=list(responses))
    return anthropic_mock


@asynccontextmanager
async def _fake_session_noop():
    """Session that accepts execute/commit silently (for success-path persist)."""

    class _S:
        async def execute(self, *a: Any, **kw: Any) -> Any:
            class _R:
                def scalar_one(self) -> int:
                    return 1

            return _R()

        async def commit(self) -> None:
            pass

    yield _S()


# --------------------------------------------------------------------------- #
# Tests
# --------------------------------------------------------------------------- #


@pytest.mark.asyncio
async def test_diagnose_text_only_returns_parsed_diagnosis() -> None:
    """Single text response — should parse to Diagnosis and persist."""
    response = _make_response([_text_block(_valid_diagnosis_json())])
    anthropic_mock = _make_client(response)

    with (
        patch.object(client, "AsyncAnthropic", return_value=anthropic_mock),
        patch.object(client, "_persist", new=AsyncMock()) as persist_mock,
    ):
        result = await diagnose({"alert_id": "abc-1"})

    assert isinstance(result, Diagnosis)
    assert result.confidence == "high"
    assert "RDS CPU" in result.root_cause
    assert result.model
    assert result.prompt_version == "v0.1.0-m1"
    assert result.latency_ms >= 0
    persist_mock.assert_awaited_once()


@pytest.mark.asyncio
async def test_diagnose_tool_use_loop_then_final_text() -> None:
    """tool_use → tool_result → final text response should yield a Diagnosis."""
    tool_resp = _make_response(
        [_tool_use_block("describe_ecs_instances", "tu-1", {"region": "cn-hangzhou"})]
    )
    text_resp = _make_response([_text_block(_valid_diagnosis_json())])
    anthropic_mock = _make_client(tool_resp, text_resp)

    with (
        patch.object(client, "AsyncAnthropic", return_value=anthropic_mock),
        patch.object(client, "_persist", new=AsyncMock()),
    ):
        result = await diagnose({"alert_id": "abc-2"}, max_tool_calls=3)

    assert result.confidence == "high"
    # tools invoked (1) + final text (1) = 2 messages.create calls
    assert anthropic_mock.messages.create.await_count == 2


@pytest.mark.asyncio
async def test_diagnose_caps_tool_calls_at_max() -> None:
    """If the model keeps requesting tools, we cap at max_tool_calls and
    return a low-confidence diagnosis_truncated placeholder."""
    tool_responses = [
        _make_response([_tool_use_block("describe_ecs_instances", f"tu-{i}",
                                        {"region": "cn-hangzhou"})])
        for i in range(5)
    ]
    anthropic_mock = _make_client(*tool_responses)

    with (
        patch.object(client, "AsyncAnthropic", return_value=anthropic_mock),
        patch.object(client, "_persist", new=AsyncMock()),
    ):
        result = await diagnose({"alert_id": "abc-3"}, max_tool_calls=2)

    assert result.confidence == "low"
    assert result.root_cause == "diagnosis_truncated"
    assert any("tool_call_cap_reached" in c for c in result.caveats)
    # First messages.create returns tool_use, second call is the one that
    # observes the cap; we should never exceed max_tool_calls + 1 round-trips.
    assert anthropic_mock.messages.create.await_count <= 3


@pytest.mark.asyncio
async def test_diagnose_dlq_on_persistent_failure() -> None:
    """All retries exhausted → diagnosis_unavailable placeholder."""
    from ai_cloud_ops.ingest import retry as retry_mod

    # Speed up the test
    retry_mod.DEFAULT_RETRY_DELAYS = (0.01, 0.01, 0.01)

    anthropic_mock = MagicMock()
    anthropic_mock.messages = MagicMock()
    anthropic_mock.messages.create = AsyncMock(side_effect=RuntimeError("api down"))

    # Stub get_session where retry.py actually uses it (not ai_cloud_ops.db —
    # retry.py imports the symbol, so patching the module attr alone is no-op).
    with patch.object(retry_mod, "get_session", _fake_session_noop):
        with patch.object(client, "AsyncAnthropic", return_value=anthropic_mock):
            result = await diagnose({"alert_id": "abc-4"})

    assert result.confidence == "low"
    assert result.root_cause == "diagnosis_unavailable"
    assert "inference failed after retries" in result.caveats
    # with_retry schedules (0.0, *delays) → 4 attempts.
    assert anthropic_mock.messages.create.await_count == 4
