"""Domain-level errors (no infrastructure types)."""

from __future__ import annotations


class DomainError(Exception):
    """Base domain error."""

    def __init__(self, message: str, *, code: str = "DOMAIN_ERROR") -> None:
        super().__init__(message)
        self.message = message
        self.code = code


class InvalidRequestError(DomainError):
    def __init__(self, message: str) -> None:
        super().__init__(message, code="INVALID_REQUEST")


class PlaceNotFoundError(DomainError):
    def __init__(self, message: str = "Place not found") -> None:
        super().__init__(message, code="PLACE_NOT_FOUND")


class AmbiguousPlaceError(DomainError):
    def __init__(self, message: str = "Multiple places match") -> None:
        super().__init__(message, code="AMBIGUOUS_PLACE")


class SourceUnavailableError(DomainError):
    def __init__(self, message: str, *, source: str | None = None) -> None:
        super().__init__(message, code="SOURCE_UNAVAILABLE")
        self.source = source


class SourceUnresolvedError(DomainError):
    def __init__(self, message: str, *, source: str | None = None) -> None:
        super().__init__(message, code="SOURCE_UNRESOLVED")
        self.source = source
