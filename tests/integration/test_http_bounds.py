"""Exercise origin checks and streaming limits without provider access."""

from __future__ import annotations

import asyncio
import gzip
import io
import zipfile
from pathlib import Path

import httpx
import pytest

from grand_lyon_mcp.infrastructure.http import (
    HostNotAllowedError,
    HttpClient,
    ResponseEncodingError,
    ResponseTooLargeError,
)
from grand_lyon_mcp.providers.gtfs.downloader import download_gtfs


class FixtureStream(httpx.AsyncByteStream):
    def __init__(self, chunks: list[bytes], *, stall: bool = False, fail: bool = False):
        self.chunks = chunks
        self.stall = stall
        self.fail = fail
        self.closed = False
        self.reads = 0

    async def __aiter__(self):
        for chunk in self.chunks:
            self.reads += 1
            yield chunk
        if self.fail:
            raise httpx.ReadError("Synthetic interruption")
        if self.stall:
            await asyncio.Event().wait()

    async def aclose(self):
        self.closed = True


def archive_bytes() -> bytes:
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w") as archive:
        archive.writestr("stops.txt", "stop_id,stop_name\nfixture,Fixture\n")
    return output.getvalue()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "url",
    [
        "http://data.grandlyon.com/path",
        "https://data.grandlyon.com:8443/path",
        "https://data.grandlyon.com:0/path",
        "https://data.grandlyon.com:invalid/path",
        "https://user:secret@data.grandlyon.com/path",
        "https://data.grandlyon.com/path#fragment",
        "https://data.grandlyon.com.evil.test/path",
    ],
)
async def test_origin_rejection_precedes_transport(url: str) -> None:
    calls = 0

    def handler(request):
        nonlocal calls
        calls += 1
        return httpx.Response(200, content=b"{}")

    client = HttpClient(transport=httpx.MockTransport(handler))
    try:
        with pytest.raises(HostNotAllowedError):
            await client.get(url)
        assert calls == 0
        client.assert_allowed("https://data.grandlyon.com:443/path")
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_empty_allowlist_rejects_default_hosts() -> None:
    client = HttpClient(allowlist=frozenset())
    try:
        with pytest.raises(HostNotAllowedError):
            client.assert_allowed("https://data.grandlyon.com/path")
    finally:
        await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "target", ["http://data.grandlyon.com/path", "https://data.grandlyon.com:8443/path"]
)
async def test_unsafe_redirect_does_not_read_body_or_send_target(target: str) -> None:
    stream = FixtureStream([b"oversized redirect content"], stall=True)
    calls = 0

    def handler(request):
        nonlocal calls
        calls += 1
        return httpx.Response(302, headers={"location": target}, stream=stream)

    client = HttpClient(transport=httpx.MockTransport(handler), total_timeout=0.2)
    try:
        with pytest.raises(HostNotAllowedError):
            await client.get("https://data.grandlyon.com/start")
        assert calls == 1
        assert stream.closed
        assert stream.reads == 0
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_allowed_cross_origin_redirect_removes_basic_credentials() -> None:
    requests = []

    def handler(request):
        requests.append(request)
        if request.url.host == "data.grandlyon.com":
            return httpx.Response(
                302, headers={"location": "https://download.data.grandlyon.com/end"}
            )
        return httpx.Response(200, content=b"{}")

    client = HttpClient(transport=httpx.MockTransport(handler))
    try:
        response = await client.get(
            "https://data.grandlyon.com/start", auth=httpx.BasicAuth("fixture", "private-fixture")
        )
        assert response.json() == {}
        assert "authorization" in requests[0].headers
        assert "authorization" not in requests[1].headers
    finally:
        await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "headers,chunks",
    [
        ({"content-length": "100"}, [b"small"]),
        ({"content-length": "1"}, [b"12345", b"67890", b"x"]),
        ({}, [b"12345", b"67890", b"x"]),
    ],
)
async def test_json_response_has_declared_and_actual_byte_caps(headers, chunks) -> None:
    stream = FixtureStream(chunks)
    client = HttpClient(
        max_response_bytes=10,
        transport=httpx.MockTransport(
            lambda request: httpx.Response(200, headers=headers, stream=stream)
        ),
    )
    try:
        with pytest.raises(ResponseTooLargeError):
            await client.get("https://data.grandlyon.com/fixture")
        assert stream.closed
    finally:
        await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize("encoding", ["gzip", "deflate", "br"])
async def test_compressed_bodies_fail_before_decompression(encoding: str) -> None:
    stream = FixtureStream([gzip.compress(b"x" * 100_000)])

    def handler(request):
        assert request.headers["accept-encoding"] == "identity"
        return httpx.Response(200, headers={"content-encoding": encoding}, stream=stream)

    client = HttpClient(max_response_bytes=10, transport=httpx.MockTransport(handler))
    try:
        with pytest.raises(ResponseEncodingError):
            await client.get("https://data.grandlyon.com/fixture")
        assert stream.reads == 0
        assert stream.closed
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_stalled_body_obeys_total_deadline() -> None:
    stream = FixtureStream([], stall=True)
    client = HttpClient(
        total_timeout=0.02,
        transport=httpx.MockTransport(lambda request: httpx.Response(200, stream=stream)),
    )
    try:
        with pytest.raises(TimeoutError):
            await asyncio.wait_for(client.get("https://data.grandlyon.com/fixture"), 1)
        assert stream.closed
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_gtfs_complete_download_publishes_private_archive(tmp_path: Path) -> None:
    data = archive_bytes()
    dest = tmp_path / "feed.zip"
    dest.write_bytes(b"previous")
    stream = FixtureStream([data[:20], data[20:]])
    client = HttpClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(200, headers={"etag": '"fixture"'}, stream=stream)
        )
    )
    try:
        result = await download_gtfs(client, dest, max_bytes=len(data))
        assert result == (dest, '"fixture"', True)
        assert dest.read_bytes() == data
        assert dest.stat().st_mode & 0o777 == 0o600
        assert stream.closed
        assert list(tmp_path.iterdir()) == [dest]
    finally:
        await client.aclose()


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "case", ["oversized", "declared", "corrupt", "interrupted", "stalled", "partial"]
)
async def test_failed_gtfs_download_preserves_previous_archive(tmp_path: Path, case: str) -> None:
    data = archive_bytes()
    stream = FixtureStream([data], fail=case == "interrupted", stall=case == "stalled")
    headers = {"content-length": str(len(data) + 1)} if case == "declared" else {}
    if case == "corrupt":
        stream = FixtureStream([b"not an archive"])
    status = 206 if case == "partial" else 200
    client = HttpClient(
        total_timeout=0.03,
        transport=httpx.MockTransport(
            lambda request: httpx.Response(status, headers=headers, stream=stream)
        ),
    )
    dest = tmp_path / "feed.zip"
    dest.write_bytes(b"previous")
    try:
        with pytest.raises(
            (ResponseTooLargeError, ValueError, zipfile.BadZipFile, httpx.ReadError, TimeoutError)
        ):
            await download_gtfs(client, dest, max_bytes=len(data) - (case == "oversized"))
        assert dest.read_bytes() == b"previous"
        assert list(tmp_path.iterdir()) == [dest]
        assert stream.closed
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_gtfs_cancellation_removes_partial_file(tmp_path: Path) -> None:
    stream = FixtureStream([b"partial"], stall=True)
    client = HttpClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(200, stream=stream))
    )
    dest = tmp_path / "feed.zip"
    task = asyncio.create_task(download_gtfs(client, dest))
    try:
        for _ in range(100):
            if stream.reads:
                break
            await asyncio.sleep(0.001)
        assert stream.reads
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert not list(tmp_path.iterdir())
        assert stream.closed
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_gtfs_not_modified_does_not_write(tmp_path: Path) -> None:
    dest = tmp_path / "feed.zip"
    dest.write_bytes(b"previous")
    client = HttpClient(transport=httpx.MockTransport(lambda request: httpx.Response(304)))
    try:
        assert await download_gtfs(client, dest, etag='"fixture"') == (None, '"fixture"', False)
        assert dest.read_bytes() == b"previous"
        assert list(tmp_path.iterdir()) == [dest]
    finally:
        await client.aclose()
