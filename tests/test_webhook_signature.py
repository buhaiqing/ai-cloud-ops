"""Unit tests for CloudMonitor webhook signature verification (T4)."""

from __future__ import annotations

import hashlib
import hmac

from ai_cloud_ops.ingest.webhook import _verify_signature


def test_valid_signature_passes() -> None:
    secret = "shhh-secret"
    body = b'{"alert_id": "abc", "region": "cn-hangzhou"}'
    sig = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    assert _verify_signature(body, sig, secret) is True


def test_invalid_signature_rejected() -> None:
    body = b'{"alert_id": "abc"}'
    assert _verify_signature(body, "not-the-right-signature", "secret") is False


def test_tampered_body_rejected() -> None:
    secret = "shhh-secret"
    body = b'{"alert_id": "abc"}'
    sig = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    tampered = b'{"alert_id": "xyz"}'  # body changed
    assert _verify_signature(tampered, sig, secret) is False


def test_empty_signature_rejected() -> None:
    assert _verify_signature(b"any", "", "any-secret") is False


def test_signature_is_constant_time() -> None:
    """Constant-time comparison prevents timing attacks. Smoke test."""
    secret = "x"
    body = b"body"
    sig = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    # Just verify the function returns bool for both valid and tampered
    assert _verify_signature(body, sig, secret) is True
    assert _verify_signature(body, sig[:-1] + ("0" if sig[-1] != "0" else "1"), secret) is False