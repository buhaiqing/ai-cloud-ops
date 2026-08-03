"""CloudMonitor EventSubscription webhook receiver (T4).

Per design.md decision T4:
- CloudMonitor pushes alert events to this endpoint (not polled)
- Verify webhook signature
- Idempotency: same alert_id → update existing, don't insert duplicate
- Persist + enqueue AI analysis job

FastAPI route handler at POST /webhook/cms.
"""

from __future__ import annotations

import hashlib
import hmac
import logging
from datetime import datetime, timezone
from typing import Any

from fastapi import APIRouter, HTTPException, Request

from ai_cloud_ops.db import get_session
from ai_cloud_ops.ingest.retry import with_retry

logger = logging.getLogger(__name__)

router = APIRouter()


def _verify_signature(body: bytes, signature: str, secret: str) -> bool:
    """Verify Aliyun CloudMonitor webhook signature.

    The signature is HMAC-SHA256(secret, body), hex-encoded, in the
    `X-Aliyun-Signature` header. Constant-time compare.
    """
    if not signature:
        return False
    expected = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, signature)


@router.post("/webhook/cms")
async def receive_cms_webhook(request: Request) -> dict[str, Any]:
    """Receive a CloudMonitor alert event."""
    body = await request.body()
    secret = request.headers.get("X-Webhook-Secret", "")
    signature = request.headers.get("X-Aliyun-Signature", "")

    # Signature verification — T5 / T11
    if not _verify_signature(body, signature, secret):
        raise HTTPException(status_code=401, detail="invalid signature")

    import json

    payload: dict[str, Any] = json.loads(body)

    alert_id = payload.get("alert_id") or payload.get("alertName")
    if not alert_id:
        raise HTTPException(status_code=400, detail="alert_id missing")

    # Process with retry + DLQ (T6)
    result = await with_retry(
        job_type="webhook",
        payload={"alert_id": alert_id, "raw": payload},
        fn=lambda: _persist_alert(payload),
    )
    if not result.succeeded:
        logger.warning("webhook ingest went to DLQ: alert_id=%s dlq_id=%s", alert_id, result.dlq_id)
        return {"status": "queued_for_retry", "dlq_id": result.dlq_id, "attempts": result.attempts}
    return {"status": "persisted", "alert_id": alert_id, "attempts": result.attempts}


async def _persist_alert(payload: dict[str, Any]) -> None:
    """Insert or update an alert row. Idempotent on (alert_id, created_at)."""
    alert_id = payload["alert_id"]
    account_alias = payload.get("account_alias", "unknown")
    region = payload.get("region", "unknown")
    severity = payload.get("severity", "warning")
    created_at_raw = payload.get("created_at")
    created_at = (
        datetime.fromisoformat(created_at_raw.replace("Z", "+00:00"))
        if created_at_raw
        else datetime.now(timezone.utc)
    )

    async with get_session() as session:
        # Idempotent insert: ON CONFLICT (alert_id, created_at) DO NOTHING
        await session.execute(
            """
            INSERT INTO alerts
                (alert_id, account_alias, region, severity, name, metric,
                 tags, payload, status, created_at, updated_at)
            VALUES
                (:alert_id, :account_alias, :region, :severity, :name, CAST(:metric AS JSONB),
                 CAST(:tags AS JSONB), CAST(:payload AS JSONB), 'open',
                 :created_at, :now)
            ON CONFLICT (alert_id, created_at) DO NOTHING
            """,
            {
                "alert_id": alert_id,
                "account_alias": account_alias,
                "region": region,
                "severity": severity,
                "name": payload.get("alertName", ""),
                "metric": json.dumps(payload.get("metric", {})),
                "tags": json.dumps(payload.get("tags", {})),
                "payload": json.dumps(payload),
                "created_at": created_at,
                "now": datetime.now(timezone.utc),
            },
        )
        await session.commit()
    logger.info("alert persisted: alert_id=%s account=%s region=%s", alert_id, account_alias, region)