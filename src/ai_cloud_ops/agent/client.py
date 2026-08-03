"""AI Agent client: turns an alert into a structured Diagnosis via Claude tool-use.

Per design.md decision T2 + T6:
- Anthropic messages.create with the read-only tool whitelist
- Tool-use loop (max N iterations) to pull context before final diagnosis
- Wrapped in with_retry; on DLQ return a low-confidence placeholder
- Persists to `analyses` table; emits Prometheus counters/histograms

Ponytail: no abstraction layer for the SDK — one file, one async function.
"""

from __future__ import annotations

import json
import logging
import os
import time
from typing import Any, Literal

from anthropic import AsyncAnthropic
from prometheus_client import Counter, Histogram
from pydantic import BaseModel, Field, ValidationError

from ai_cloud_ops.agent.prompt import PROMPT_VERSION, SYSTEM_PROMPT, build_user_prompt
from ai_cloud_ops.agent.tools import all_tool_specs_for_llm, is_allowed
from ai_cloud_ops.db import get_session
from ai_cloud_ops.ingest.retry import with_retry

logger = logging.getLogger(__name__)

ANTHROPIC_MODEL = os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-5")

_analyze_total = Counter(
    "ai_cloud_ops_analyze_total",
    "Number of analyze calls by outcome",
    ["outcome"],  # success | failure | dlq
)
_analyze_latency = Histogram(
    "ai_cloud_ops_analyze_latency_seconds",
    "Analyze call latency in seconds",
)


class Diagnosis(BaseModel):
    """Structured diagnosis returned by the AI Agent."""

    root_cause: str
    recommendations: list[dict[str, Any]] = Field(default_factory=list)
    evidence_chains: list[dict[str, Any]] = Field(default_factory=list)
    confidence: Literal["high", "medium", "low"]
    caveats: list[str] = Field(default_factory=list)
    latency_ms: int
    model: str
    prompt_version: str


async def _execute_tool(tool_name: str, tool_input: dict[str, Any]) -> dict[str, Any]:
    """M1 placeholder: real Aliyun SDK calls land in a fetcher module later.

    The whitelist is still enforced here so the LLM can't call anything else.
    """
    if not is_allowed(tool_name):
        return {"status": "tool_not_allowed", "tool": tool_name}
    # ponytail: replaced by fetcher.execute_tool() in M2 — single call site.
    return {"status": "tool_not_implemented_in_M1", "tool": tool_name, "input": tool_input}


def _parse_diagnosis(text: str, latency_ms: int) -> Diagnosis:
    """Parse the LLM's final JSON text into a Diagnosis, with safe defaults."""
    data = json.loads(text)
    return Diagnosis(
        root_cause=data.get("root_cause", "unknown"),
        recommendations=data.get("recommendations", []),
        evidence_chains=data.get("evidence_chains", []),
        confidence=data.get("confidence", "low"),
        caveats=data.get("caveats", []),
        latency_ms=latency_ms,
        model=ANTHROPIC_MODEL,
        prompt_version=PROMPT_VERSION,
    )


def _unavailable(latency_ms: int) -> Diagnosis:
    """Return the DLQ placeholder Diagnosis."""
    return Diagnosis(
        root_cause="diagnosis_unavailable",
        recommendations=[],
        evidence_chains=[],
        confidence="low",
        caveats=["inference failed after retries"],
        latency_ms=latency_ms,
        model=ANTHROPIC_MODEL,
        prompt_version=PROMPT_VERSION,
    )


async def _persist(diagnosis: Diagnosis, alert_id: Any) -> None:
    """Insert a row into the analyses table."""
    async with get_session() as session:
        await session.execute(
            """
            INSERT INTO analyses
                (alert_id, model, prompt_version, root_cause, recommendations,
                 evidence_chains, latency_ms)
            VALUES
                (:alert_id, :model, :prompt_version, :root_cause,
                 CAST(:recommendations AS JSONB),
                 CAST(:evidence_chains AS JSONB),
                 :latency_ms)
            """,
            {
                "alert_id": alert_id,
                "model": diagnosis.model,
                "prompt_version": diagnosis.prompt_version,
                "root_cause": diagnosis.root_cause,
                "recommendations": json.dumps(diagnosis.recommendations),
                "evidence_chains": json.dumps(diagnosis.evidence_chains),
                "latency_ms": diagnosis.latency_ms,
            },
        )
        await session.commit()


async def _call_claude(
    alert: dict[str, Any], max_tool_calls: int
) -> Diagnosis:
    """Run the Claude tool-use loop and return a parsed Diagnosis."""
    client = AsyncAnthropic()
    messages: list[dict[str, Any]] = [
        {"role": "user", "content": build_user_prompt(alert)}
    ]
    started = time.monotonic()
    tool_calls = 0

    while True:
        response = await client.messages.create(
            model=ANTHROPIC_MODEL,
            system=SYSTEM_PROMPT,
            tools=all_tool_specs_for_llm(),
            max_tokens=2048,
            messages=messages,
        )

        # Append assistant turn so subsequent tool_results are well-formed.
        messages.append({"role": "assistant", "content": response.content})

        tool_uses = [b for b in response.content if b.type == "tool_use"]
        if not tool_uses:
            text_blocks = [b.text for b in response.content if b.type == "text"]
            final_text = "\n".join(text_blocks)
            latency_ms = int((time.monotonic() - started) * 1000)
            return _parse_diagnosis(final_text, latency_ms)

        # Hit the cap — stop and surface a low-confidence placeholder.
        if tool_calls >= max_tool_calls:
            latency_ms = int((time.monotonic() - started) * 1000)
            return Diagnosis(
                root_cause="diagnosis_truncated",
                recommendations=[],
                evidence_chains=[],
                confidence="low",
                caveats=[f"tool_call_cap_reached:{max_tool_calls}"],
                latency_ms=latency_ms,
                model=ANTHROPIC_MODEL,
                prompt_version=PROMPT_VERSION,
            )

        # Execute the requested tools and feed results back in one user turn.
        tool_results: list[dict[str, Any]] = []
        for block in tool_uses:
            result = await _execute_tool(block.name, block.input)
            tool_results.append(
                {
                    "type": "tool_result",
                    "tool_use_id": block.id,
                    "content": json.dumps(result),
                }
            )
            tool_calls += 1
        messages.append({"role": "user", "content": tool_results})


async def diagnose(alert: dict[str, Any], *, max_tool_calls: int = 5) -> Diagnosis:
    """Diagnose an alert by calling Claude with the read-only tool whitelist.

    Wraps the LLM call in with_retry. On final failure (DLQ), returns a
    placeholder Diagnosis with confidence='low' and the
    'inference failed after retries' caveat.
    """
    alert_id = alert.get("alert_id")

    async def _run() -> Diagnosis:
        with _analyze_latency.time():
            return await _call_claude(alert, max_tool_calls)

    result = await with_retry(
        job_type="analyze",
        payload={"alert_id": alert_id},
        fn=_run,
    )

    if result.succeeded and result.value is not None:
        _analyze_total.labels(outcome="success").inc()
        try:
            await _persist(result.value, alert_id)
        except Exception as exc:  # noqa: BLE001 — persistence is best-effort
            logger.warning("analyses persist failed: %s", exc)
        return result.value

    _analyze_total.labels(outcome="dlq").inc()
    return _unavailable(latency_ms=0)


__all__ = ["ANTHROPIC_MODEL", "Diagnosis", "diagnose"]
