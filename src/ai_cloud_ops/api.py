"""FastAPI application entry point.

Routes:
- GET  /healthz        — liveness probe (T7)
- GET  /readyz         — readiness probe
- GET  /metrics        — Prometheus metrics (T6)
- POST /webhook/cms    — CloudMonitor alert webhook (T4)
"""

from __future__ import annotations

import logging
import os

from fastapi import FastAPI
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from ai_cloud_ops.ingest.webhook import router as webhook_router

logger = logging.getLogger(__name__)


def create_app() -> FastAPI:
    app = FastAPI(
        title="ai-cloud-ops",
        version="0.1.0",
        description="AI-Native Alibaba Cloud Multi-Account Ops Console",
    )
    app.include_router(webhook_router)

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        # Liveness only — should not depend on DB/Redis
        return {"status": "ok"}

    @app.get("/readyz")
    def readyz() -> dict[str, str]:
        # Readiness checks DB + Redis are reachable
        # M2 TODO: actually probe; for now return ok if process is up
        return {"status": "ok"}

    @app.get("/metrics")
    def metrics() -> tuple[bytes, dict[str, str]]:
        return generate_latest(), {"Content-Type": CONTENT_TYPE_LATEST}

    @app.on_event("startup")
    async def _startup_log() -> None:
        logger.info(
            "ai-cloud-ops starting: port=%s env=%s",
            os.environ.get("WEBHOOK_PORT", "8080"),
            os.environ.get("ENV", "dev"),
        )

    return app


app = create_app()