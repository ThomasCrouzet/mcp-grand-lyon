"""Single-flight: coalesce concurrent identical async work."""

from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from typing import TypeVar

T = TypeVar("T")


class SingleFlight:
    def __init__(self) -> None:
        self._inflight: dict[str, asyncio.Future[object]] = {}
        self._lock = asyncio.Lock()
        # Références fortes sur les tâches fire-and-forget : sans cela la boucle
        # asyncio ne garde qu'une référence faible et le GC peut collecter la Task
        # pendant qu'elle est suspendue sur `await factory()` (I/O réseau).
        self._tasks: set[asyncio.Task[None]] = set()

    async def do(self, key: str, factory: Callable[[], Awaitable[T]]) -> T:
        async with self._lock:
            existing = self._inflight.get(key)
            if existing is not None:
                fut = existing
            else:
                loop = asyncio.get_running_loop()
                fut = loop.create_future()
                self._inflight[key] = fut

                async def _run() -> None:
                    try:
                        result = await factory()
                        if not fut.done():
                            fut.set_result(result)
                    except Exception as exc:
                        if not fut.done():
                            fut.set_exception(exc)
                    finally:
                        async with self._lock:
                            self._inflight.pop(key, None)

                task = asyncio.create_task(_run())
                self._tasks.add(task)
                task.add_done_callback(self._tasks.discard)

        return await fut  # type: ignore[return-value]
