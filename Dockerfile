# syntax=docker/dockerfile:1.7

FROM python:3.11-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PROJECT_ENVIRONMENT=python

# Install uv
COPY --from=ghcr.io/astral-sh/uv:0.5.11 /uv /uvx /usr/local/bin/

WORKDIR /app

# Install dependencies first (better layer caching)
COPY pyproject.toml uv.lock* ./
RUN uv sync --no-install-project --no-dev

# Copy source
COPY src ./src
COPY alembic.ini* ./
COPY db ./db
RUN uv sync --no-dev

# Create non-root user
RUN useradd --create-home --shell /bin/bash appuser
USER appuser

EXPOSE 8080 8081

# Default: start API server
CMD ["uv", "run", "uvicorn", "ai_cloud_ops.api:app", "--host", "0.0.0.0", "--port", "8080"]