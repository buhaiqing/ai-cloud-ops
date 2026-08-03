"""CLI entry point — minimal scaffold for M1 Day 1-2.

Full implementation per design.md Milestone 1:
- `ai-cloud-ops analyze` — single-account single-region AI diagnostic
- `ai-cloud-ops serve` — start FastAPI worker (M2)
"""

from __future__ import annotations

import typer
from rich.console import Console

app = typer.Typer(
    name="ai-cloud-ops",
    help="AI-Native Alibaba Cloud Multi-Account Ops Console",
    no_args_is_help=True,
)
console = Console()


@app.command()
def analyze(
    account: str = typer.Option(..., "--account", "-a", help="Aliyun account alias"),
    region: str = typer.Option(..., "--region", "-r", help="Aliyun region ID (e.g. cn-hangzhou)"),
    since: str = typer.Option("1h", "--since", help="Look back window (e.g. 1h, 24h)"),
) -> None:
    """Run AI diagnostic on recent alerts for one account/region.

    M1 MVP scope: 1 account, 1 region, CLI output of AI analysis.
    """
    console.print(
        f"[yellow]M1 scaffold — analyze not yet implemented[/yellow]\n"
        f"Will analyze alerts for {account} in {region} (since {since}).",
    )
    raise typer.Exit(code=1)


@app.command()
def serve() -> None:
    """Start the FastAPI worker (webhook receiver + AI Agent)."""
    console.print("[yellow]M2 feature — serve not yet implemented[/yellow]")
    raise typer.Exit(code=1)


def main() -> None:
    app()


if __name__ == "__main__":
    main()