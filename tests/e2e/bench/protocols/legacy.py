"""Legacy protocol: what a customer's Python script does against /api.

Anonymous login, POST /api/query, plain json.loads -- that parse cost is
the honest price a real client pays on this path and belongs in the
measurement. Runs unchanged against the old and the new server; identical
fingerprints across the two is the drop-in compatibility claim.

stdlib http.client on purpose: exact first-body-byte TTFB and explicit
Accept-Encoding control (the server gzips on any header containing gzip)."""

import gzip
import http.client
import json
from urllib.parse import urlsplit

import pandas as pd

TTFB_DEFINITION = "first response body byte"

# Sentinels the wire uses for typed undefined values; a customer parser
# must map them (and the counter makes a silent drop visible in `report`).
WART_VOID = "(void)"
WART_UNDEFINED = "(undefined)"


def request(base_url, method, path, body, headers):
    """One HTTP exchange; returns the response object and its connection
    (kept open so the caller controls when the body is read)."""
    parts = urlsplit(base_url)
    conn = http.client.HTTPConnection(parts.hostname, parts.port)
    conn.request(method, path, body=body, headers=headers)
    return conn, conn.getresponse()


def login(base_url):
    """Anonymous login; the insecure cluster hands out a token for empty
    credentials."""
    conn, response = request(
        base_url, "POST", "/api/login",
        json.dumps({"username": "", "secret_key": ""}),
        {"Content-Type": "application/json"},
    )
    payload = json.loads(response.read())
    conn.close()
    token = payload.get("token", "")
    if not token:
        raise RuntimeError(f"login failed against {base_url}: {payload}")
    return token


def normalize_column(column, telemetry):
    """Wire column -> pandas Series: sentinels to nulls (counted),
    ISO timestamps parsed, count/int64 kept integral."""
    name, kind, data = column["name"], column["type"], column["data"]
    warts = sum(1 for v in data if v in (WART_VOID, WART_UNDEFINED))
    if warts:
        telemetry["wart_count"] = telemetry.get("wart_count", 0) + warts
        data = [None if v in (WART_VOID, WART_UNDEFINED) else v for v in data]
    if kind == "timestamp":
        series = pd.to_datetime(pd.Series(data), utc=True, format="ISO8601")
        return name, series.dt.tz_localize(None)
    if kind in ("int64", "count"):
        return name, pd.Series(data, dtype="float64" if None in data else "int64")
    if kind == "double":
        return name, pd.Series(data, dtype="float64")
    return name, pd.Series(data, dtype=object)


def frame_from_payload(payload, telemetry):
    tables = payload["tables"]
    if len(tables) != 1:
        raise RuntimeError(f"expected one result table, got {len(tables)}")
    telemetry.setdefault("wart_count", 0)
    return pd.DataFrame(dict(
        normalize_column(c, telemetry) for c in tables[0]["columns"]
    ))


def fetch(cfg, query, record_ttfb, telemetry):
    token = login(cfg.base_url)
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {token}",
    }
    if cfg.gzip:
        headers["Accept-Encoding"] = "gzip"
    conn, response = request(
        cfg.base_url, "POST", "/api/query",
        json.dumps({"query": query}), headers,
    )
    first = response.read(1)
    record_ttfb()
    raw = first + response.read()
    conn.close()
    telemetry["gzip"] = response.getheader("Content-Encoding") == "gzip"
    telemetry["response_bytes"] = len(raw)
    body = gzip.decompress(raw) if telemetry["gzip"] else raw
    telemetry["body_bytes_decoded"] = len(body)
    if response.status != 200:
        raise RuntimeError(f"query failed ({response.status}): {body[:512]!r}")
    yield frame_from_payload(json.loads(body), telemetry)
