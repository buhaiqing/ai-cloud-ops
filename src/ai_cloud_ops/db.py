"""Database session factory + base setup.

Ponytail: SQLAlchemy async session, no ORM models for raw SQL use cases.
"""

from __future__ import annotations

import os
from contextlib import asynccontextmanager
from typing import AsyncIterator

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

_engine = None
_sessionmaker: async_sessionmaker[AsyncSession] | None = None


def _get_url() -> str:
    user = os.environ.get("POSTGRES_USER", "aiops")
    password = os.environ.get("POSTGRES_PASSWORD", "")
    db = os.environ.get("POSTGRES_DB", "aiops")
    host = os.environ.get("POSTGRES_HOST", "localhost")
    return f"postgresql+asyncpg://{user}:{password}@{host}/{db}"


def init_engine() -> None:
    """Lazy-init the engine. Call once at startup."""
    global _engine, _sessionmaker
    if _engine is not None:
        return
    _engine = create_async_engine(_get_url(), pool_pre_ping=True, pool_size=10)
    _sessionmaker = async_sessionmaker(_engine, expire_on_commit=False)


@asynccontextmanager
async def get_session() -> AsyncIterator[AsyncSession]:
    """Yield an async session. Caller commits or rolls back."""
    init_engine()
    assert _sessionmaker is not None
    async with _sessionmaker() as session:
        try:
            yield session
        except Exception:
            await session.rollback()
            raise