"""Application-wide structured JSON logging."""

from __future__ import annotations

import json
import logging
import logging.config
from datetime import datetime, timezone
from typing import Any

_STANDARD_FIELDS = frozenset(
    logging.LogRecord("", 0, "", 0, "", (), None).__dict__
) | {"asctime", "message"}
_configured = False


class JsonFormatter(logging.Formatter):
    """Serialize a log record as one JSON object."""

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            key: value for key, value in record.__dict__.items() if key not in _STANDARD_FIELDS
        }
        payload.update(
            timestamp=datetime.fromtimestamp(record.created, timezone.utc).isoformat(),
            level=record.levelname,
            logger=record.name,
            message=record.getMessage(),
            module=record.module,
            func=record.funcName,
            line=record.lineno,
            process=record.process,
            thread=record.thread,
        )
        return json.dumps(payload, default=str, ensure_ascii=False)


def configure_logging(level: str = "INFO") -> None:
    """Call once at app startup. Idempotent."""
    global _configured
    logging.config.dictConfig(
        {
            "version": 1,
            "disable_existing_loggers": False,
            "formatters": {"json": {"()": JsonFormatter}},
            "handlers": {
                "stderr": {
                    "class": "logging.StreamHandler",
                    "formatter": "json",
                    "stream": "ext://sys.stderr",
                }
            },
            "root": {"handlers": ["stderr"], "level": level.upper()},
        }
    )
    _configured = True


def get_logger(name: str) -> logging.Logger:
    """Get a logger that's already configured for JSON output."""
    if not _configured:
        configure_logging()
    return logging.getLogger(name)
