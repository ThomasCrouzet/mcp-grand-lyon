"""Retry policy for idempotent HTTP calls."""

from __future__ import annotations

from collections.abc import Callable
from typing import TypeVar

from tenacity import (
    retry,
    retry_if_exception,
    stop_after_attempt,
    wait_exponential_jitter,
)

T = TypeVar("T")

RETRYABLE_STATUS = frozenset({408, 429, 500, 502, 503, 504})


class RetryableHttpError(Exception):
    def __init__(self, status_code: int, message: str = "") -> None:
        super().__init__(message or f"HTTP {status_code}")
        self.status_code = status_code


def is_retryable(exc: BaseException) -> bool:
    if isinstance(exc, RetryableHttpError):
        return exc.status_code in RETRYABLE_STATUS
    # Connection / timeout style
    name = type(exc).__name__
    return name in {
        "ConnectError",
        "ConnectTimeout",
        "ReadTimeout",
        "WriteTimeout",
        "PoolTimeout",
        "NetworkError",
        "RemoteProtocolError",
    }


def with_retry(fn: Callable[[], T], *, attempts: int = 3) -> T:
    decorated = retry(
        reraise=True,
        stop=stop_after_attempt(attempts),
        wait=wait_exponential_jitter(initial=0.2, max=4.0),
        retry=retry_if_exception(is_retryable),
    )(fn)
    result: T = decorated()
    return result
