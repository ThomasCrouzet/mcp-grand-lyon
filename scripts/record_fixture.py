#!/usr/bin/env python3
"""Record a redacted HTTP fixture (explicit opt-in only)."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from urllib.parse import urlparse

ALLOWLIST = frozenset(
    {
        "data.grandlyon.com",
        "download.data.grandlyon.com",
        "api.transitous.org",
    }
)


def main() -> int:
    parser = argparse.ArgumentParser(description="Record redacted fixture")
    parser.add_argument("--url", required=True)
    parser.add_argument("--out", required=True, help="Relative path under tests/fixtures/recorded/")
    parser.add_argument("--confirm", action="store_true", help="Required to write")
    args = parser.parse_args()
    if not args.confirm:
        print("Refusing to write without --confirm", file=sys.stderr)
        return 2
    host = urlparse(args.url).hostname or ""
    if host not in ALLOWLIST:
        print(f"Host not allowlisted: {host}", file=sys.stderr)
        return 2
    print(
        "WARNING: inspect the output for secrets before committing.",
        file=sys.stderr,
    )
    import httpx

    response = httpx.get(args.url, timeout=30.0)
    # Strip sensitive headers from any metadata we store
    meta = {
        "url": args.url,
        "status_code": response.status_code,
        "headers": {
            k: "[REDACTED]" if k.lower() in {"authorization", "set-cookie", "cookie"} else v
            for k, v in response.headers.items()
        },
    }
    out = Path("tests/fixtures/recorded") / args.out
    out.parent.mkdir(parents=True, exist_ok=True)
    try:
        body: object = response.json()
    except Exception:
        body = response.text[:50_000]
    out.write_text(
        json.dumps({"meta": meta, "body": body}, ensure_ascii=False, indent=2)[:2_000_000],
        encoding="utf-8",
    )
    print(f"Wrote {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
