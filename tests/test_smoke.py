"""Smoke test — verify package import + CLI scaffold loads."""

from __future__ import annotations


def test_import() -> None:
    """Package imports cleanly with version metadata."""
    import ai_cloud_ops

    assert ai_cloud_ops.__version__ == "0.1.0"


def test_cli_help() -> None:
    """CLI --help renders without error."""
    from typer.testing import CliRunner

    from ai_cloud_ops.cli import app

    runner = CliRunner()
    result = runner.invoke(app, ["--help"])
    assert result.exit_code == 0
    assert "AI-Native Alibaba Cloud" in result.output