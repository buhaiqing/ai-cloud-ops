"""AI 诊断 LLM-as-judge 评分（T9 + docs/ai-quality.md）。

Per docs/ai-quality.md scoring rubric:
- 5-dim score card (1-5 each): 根因准确性, 修复建议可执行性, 证据链完整性, 幻觉率, 响应时间
- Total = 25, pass threshold = 18 (72%)
- CI gate: 平均 ≥ 18 → pass, 17-18 → warn, < 17 → fail

This script:
1. Loads baseline_samples.json
2. For each sample, runs the AI Agent (mocked in unit tests, real in CI)
3. Sends the AI output to Claude-as-judge for 5-dim scoring
4. Asserts the pass threshold

Run: `pytest -m eval` (CI workflow calls this with ANTHROPIC_API_KEY set).
"""

from __future__ import annotations

import asyncio
import json
import time
from pathlib import Path
from typing import Any

import pytest


PASS_THRESHOLD = 18  # of 25
JUDGE_MODEL = "claude-sonnet-4-5"

JUDGE_SYSTEM_PROMPT = """You are an expert SRE evaluator. You will receive an \
Alibaba Cloud alert and an AI-generated diagnosis. Score the diagnosis on \
5 dimensions (1-5 each):

1. 根因准确性 (root cause accuracy): does it correctly identify the cause?
2. 修复建议可执行性 (recommendation executability): are actions concrete + actionable?
3. 证据链完整性 (evidence chain completeness): does every claim trace to a tool call?
4. 幻觉率 (hallucination rate): 5=no invented IDs/timestamps; 1=many hallucinations
5. 响应时间 (response time): 5=<5s; 1=>30s

Output JSON only:
{"root_cause": 1-5, "recommendation": 1-5, "evidence": 1-5, "hallucination": 1-5, "latency": 1-5, "reasoning": "..."}
"""


def load_baseline_samples() -> list[dict[str, Any]]:
    """Load the 10 baseline samples from JSON."""
    path = Path(__file__).parent / "baseline_samples.json"
    return json.loads(path.read_text())["samples"]


@pytest.fixture(scope="module")
def baseline_samples() -> list[dict[str, Any]]:
    return load_baseline_samples()


@pytest.mark.eval
def test_baseline_samples_exist() -> None:
    """Sanity check: 10 samples loaded."""
    samples = load_baseline_samples()
    assert len(samples) == 10, f"expected 10 baseline samples, got {len(samples)}"


@pytest.mark.eval
@pytest.mark.asyncio
async def test_each_sample_meets_threshold(baseline_samples: list[dict[str, Any]]) -> None:
    """For each sample, run AI Agent + LLM-judge. Assert mean score ≥ 18.

    This test is the CI gate. It requires ANTHROPIC_API_KEY.
    """
    # M1: this test imports the agent and runs end-to-end. Skipped in unit tests.
    pytest.skip("M1: requires AI Agent implementation (T2) before this can run end-to-end")


def _stub_score_for_unit_tests() -> dict[str, int]:
    """A passing stub score for unit-testing the judge logic itself."""
    return {
        "root_cause": 4,
        "recommendation": 4,
        "evidence": 3,
        "hallucination": 5,
        "latency": 4,
        "reasoning": "stub for unit tests",
    }


def aggregate_scores(scores: list[dict[str, int]]) -> dict[str, float]:
    """Compute mean across all 5 dimensions for a list of judge outputs."""
    if not scores:
        return {k: 0.0 for k in ("root_cause", "recommendation", "evidence", "hallucination", "latency")}
    return {
        k: sum(s[k] for s in scores) / len(scores)
        for k in ("root_cause", "recommendation", "evidence", "hallucination", "latency")
    }


def total_score(s: dict[str, int]) -> int:
    """Sum the 5-dim score card (max 25)."""
    return sum(s[k] for k in ("root_cause", "recommendation", "evidence", "hallucination", "latency"))


def test_judge_logic_passes_stub() -> None:
    """Sanity: the stub score passes the threshold."""
    assert total_score(_stub_score_for_unit_tests()) >= PASS_THRESHOLD


def test_aggregate_scores_empty() -> None:
    """Empty input → zeros, no division error."""
    agg = aggregate_scores([])
    assert agg["root_cause"] == 0.0
    assert agg["recommendation"] == 0.0


@pytest.mark.eval
def test_ci_gate_evaluation(baseline_samples: list[dict[str, Any]]) -> None:
    """The actual CI gate. Will run when ANTHROPIC_API_KEY is set + AI Agent ready.

    Logic placeholder: aggregate scores and fail CI if mean total < PASS_THRESHOLD.
    """
    pytest.skip("M1: gate implementation deferred to AI Agent integration (T2)")

    # Future:
    # scores = []
    # for sample in baseline_samples:
    #     diagnosis = await ai_agent.diagnose(sample["alert_payload"])
    #     judge_output = await judge_model.score(sample, diagnosis)
    #     scores.append(judge_output)
    # mean_total = sum(total_score(s) for s in scores) / len(scores)
    # assert mean_total >= PASS_THRESHOLD, f"mean={mean_total:.1f} < {PASS_THRESHOLD}"