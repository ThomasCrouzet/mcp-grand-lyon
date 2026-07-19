"""Simple concurrency limiter per provider."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager


class ConcurrencyLimiter:
    def __init__(self, max_parallel: int = 6) -> None:
        self._sem = asyncio.Semaphore(max(1, max_parallel))

    @asynccontextmanager
    async def acquire(self) -> AsyncIterator[None]:
        async with self._sem:
            yield
