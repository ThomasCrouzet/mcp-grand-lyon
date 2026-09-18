# Bounded HTTP transfers

Status: Accepted.

## Decision

The shared client permits only configured HTTPS hosts on port 443.
It rejects URL credentials and fragments before network access.
It applies the same policy to every redirect and permits at most five redirects.
It closes redirect bodies without buffering them.
HTTPX removes authentication when the redirect origin changes.
The client does not reapply that authentication or use environment proxies.

A 60-second deadline covers queue waits, retries, redirects, and body consumption.
Normal API responses have an 8 MiB body limit.
The client checks declared lengths and actual bytes.
It requests identity encoding and rejects compressed HTTP bodies before decompression.
This prevents a decoder from allocating excessive data before the byte counter can reject it.

GTFS downloads use the importer's 300 MiB archive limit.
The downloader writes bounded chunks into a private neighboring temporary file.
It requires a complete HTTP 200 response and a nonempty ZIP directory.
It synchronizes the file before atomic replacement of the previous archive.
Failures and cancellation remove the partial file and preserve the previous archive.
A 304 response does not write a file.

## Limits

ZIP-directory validation does not establish CSV correctness or complete CRC integrity.
The importer remains responsible for archive members and data validation.
The importer still materializes CSV dictionaries; this decision does not establish a strict importer memory budget.

## Validation

Run the synthetic transport and publication tests:

```sh
uv run --locked pytest tests/integration/test_http_bounds.py
```
