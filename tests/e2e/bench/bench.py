#!/usr/bin/env python3
"""Assessment bench: one (protocol, server) pair per invocation.

Measures wall clock until a Python client holds a fully materialized
pandas DataFrame, plus the supporting metrics of docs/bench-plan.md.
Temporary tool: deletable with `rm -rf tests/e2e/bench` once the rewrite
beats the old server. The Makefile is the only configuration source;
every path and port arrives as an explicit flag.

Subcommands: run, report, selftest (and the internal _child, spawned by
run for each measurement so RSS and CPU are attributable to one process).
"""

import argparse
import base64
import json
import math
import os
import shlex
import subprocess
import sys
import time
from collections import deque

import numpy as np
import pandas as pd

# --------------------------------------------------------------- constants

# The registry of valid runs is this table, nothing else. A non-empty value
# names the milestone that enables the run.
REGISTRY = {
    ("native", "qdbd"): "",
    ("legacy", "old-rest"): "",
    ("legacy", "new-rest"): "available with M1",
    ("flightsql", "new-rest"): "available with M3",
}

# The query set is data: adding a query is one line. Reduce-family rules
# (docs/bench-plan.md, "Queries"): agg_topk is the agg_wide text plus the
# ORDER BY ... LIMIT clause and nothing else, so their qdbd->reducer
# volumes are directly comparable; every ORDER BY carries a full tiebreaker
# (id is unique per group); COUNT(id), never COUNT(*) (the wire expands
# COUNT(*) into one count per column).
_AGG_WIDE = (
    'SELECT id, COUNT(id), SUM(amount), MIN(rate), MAX(rate) '
    'FROM "reproduce" GROUP BY id'
)
QUERIES = {
    "count": 'SELECT COUNT(id) FROM "reproduce"',
    "head": 'SELECT * FROM "reproduce" LIMIT 65536',
    "full": 'SELECT * FROM "reproduce"',
    "limit10": 'SELECT * FROM "reproduce" LIMIT 10',
    "agg_coarse": (
        'SELECT $timestamp, accountId, COUNT(id), SUM(amount) '
        'FROM "reproduce" GROUP BY 1h, accountId'
    ),
    "agg_wide": _AGG_WIDE,
    "agg_topk": _AGG_WIDE + " ORDER BY SUM(amount) DESC, id ASC LIMIT 10",
}

DATASET_TABLE = "reproduce"
DATASET_ROWS = 5_613_032

# `ps` forks per sample; 50 ms keeps the sampler cheap while still catching
# RSS peaks of multi-second measurements.
SAMPLE_INTERVAL_SECONDS = 0.05
# qdbd refreshes $qdb.statistics.* periodically (500 ms in the shared
# test-setup config); read counters only after at least 2x that.
COUNTER_SETTLE_SECONDS = 1.0
# Client-side C API input buffer, equal to the old server's
# --max-in-buffer-size so every run accepts the same result sizes.
MAX_IN_BUF_SIZE = 8_589_934_592
STREAM_BATCH_SIZE = 65_536
# qdbd <-> C API compression: every client keeps the binding default so the
# volume-1 counters are comparable across runs (no getter is exposed; the
# default is `balanced` in qdb-api-go and qdb-api-python alike).
CAPI_COMPRESSION = "balanced (binding default)"

FINGERPRINT_EDGE_ROWS = 5
REL_TOLERANCE = 1e-9


def die(message, code=1):
    print(f"[FAIL] {message}", file=sys.stderr)
    sys.exit(code)


def log(message):
    print(f"[INFO] {message}", file=sys.stderr)


# ------------------------------------------------------------- fingerprint
#
# A streaming accumulator: one code path fingerprints a one-shot DataFrame
# and a sequence of batches identically (batch boundaries must not affect
# the result -- `selftest` pins that). Equality across runs is checked over
# these persisted fingerprints, never over live DataFrames.


def normalize_frame(frame):
    """Canonical form shared by every protocol: columns sorted by name,
    timestamps tz-naive UTC nanoseconds, integers int64 (float64 when
    null), floats float64. Protocol modules resolve wire-specific shapes
    (sentinel strings, ISO timestamps) before this point."""
    if isinstance(frame.index, pd.DatetimeIndex):
        frame = frame.reset_index()
    else:
        frame = frame.reset_index(drop=True)
    out = {}
    for name in sorted(frame.columns):
        col = frame[name]
        if pd.api.types.is_datetime64_any_dtype(col):
            if getattr(col.dtype, "tz", None) is not None:
                col = col.dt.tz_convert("UTC").dt.tz_localize(None)
            col = col.astype("datetime64[ns]")
        elif pd.api.types.is_bool_dtype(col):
            col = col.astype("int64")
        elif pd.api.types.is_integer_dtype(col):
            col = col.astype("float64") if col.isna().any() else col.astype("int64")
        elif pd.api.types.is_float_dtype(col):
            col = col.astype("float64")
        else:
            col = col.astype(object)
        out[name] = col
    return pd.DataFrame(out)


def column_kind(col):
    if pd.api.types.is_datetime64_any_dtype(col):
        return "ts"
    if pd.api.types.is_numeric_dtype(col):
        return "num"
    return "str"


def canonical_cell(value):
    if value is None or (isinstance(value, float) and math.isnan(value)):
        return None
    if value is pd.NaT:
        return None
    if isinstance(value, pd.Timestamp):
        return value.isoformat()
    if isinstance(value, np.integer):
        return int(value)
    if isinstance(value, np.floating):
        return float(value)
    if isinstance(value, (bytes, bytearray)):
        return base64.b64encode(value).decode("ascii")
    if isinstance(value, (int, float, str)):
        return value
    return str(value)


def canonical_rows(frame):
    return [
        [canonical_cell(v) for v in row]
        for row in frame.itertuples(index=False, name=None)
    ]


def column_sum(values):
    """Exact for int64 (Python-int arithmetic, immune to wrap differences
    between batch splits), fsum partial for float64."""
    if pd.api.types.is_integer_dtype(values):
        return int(values.to_numpy().astype(object).sum())
    return math.fsum(values.to_numpy())


def order_free_hash(col):
    """Order-independent content hash: sum of per-row hashes mod 2^64, so
    batch boundaries and row order cannot affect it."""
    hashes = pd.util.hash_pandas_object(col, index=False).to_numpy()
    return int(hashes.astype(object).sum()) % (1 << 64)


def fingerprint_begin():
    return {
        "rows": 0,
        "columns": None,
        "cols": None,
        "head": [],
        "tail": deque(maxlen=FINGERPRINT_EDGE_ROWS),
    }


def fingerprint_update(state, frame):
    frame = normalize_frame(frame)
    names = list(frame.columns)
    if state["columns"] is None:
        state["columns"] = names
        state["cols"] = {
            n: {"kind": column_kind(frame[n]), "nulls": 0, "sums": [],
                "min": None, "max": None, "hash": 0}
            for n in names
        }
    elif names != state["columns"]:
        raise ValueError(f"column set changed mid-stream: {names} != {state['columns']}")
    state["rows"] += len(frame)
    for name in names:
        col = frame[name]
        acc = state["cols"][name]
        acc["nulls"] += int(col.isna().sum())
        if acc["kind"] == "num":
            values = col.dropna()
            if len(values):
                acc["sums"].append(column_sum(values))
                low, high = values.min(), values.max()
                acc["min"] = low if acc["min"] is None else min(acc["min"], low)
                acc["max"] = high if acc["max"] is None else max(acc["max"], high)
        else:
            acc["hash"] = (acc["hash"] + order_free_hash(col)) % (1 << 64)
    missing = FINGERPRINT_EDGE_ROWS - len(state["head"])
    if missing > 0:
        state["head"].extend(canonical_rows(frame.head(missing)))
    state["tail"].extend(canonical_rows(frame.tail(FINGERPRINT_EDGE_ROWS)))


def fingerprint_finish(state):
    cols = {}
    for name in state["columns"] or []:
        acc = state["cols"][name]
        entry = {"kind": acc["kind"], "nulls": acc["nulls"]}
        if acc["kind"] == "num":
            entry["sum"] = math.fsum(acc["sums"]) if any(
                isinstance(s, float) for s in acc["sums"]
            ) else sum(acc["sums"])
            entry["min"] = canonical_cell(acc["min"])
            entry["max"] = canonical_cell(acc["max"])
        else:
            entry["hash"] = acc["hash"]
        cols[name] = entry
    return {
        "rows": state["rows"],
        "columns": state["columns"] or [],
        "cols": cols,
        "head": state["head"],
        "tail": list(state["tail"]),
    }


def numbers_close(a, b, rel_tol):
    return math.isclose(a, b, rel_tol=rel_tol, abs_tol=1e-12)


def cells_equal(a, b, rel_tol):
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return numbers_close(a, b, rel_tol)
    return a == b


def fingerprint_diff(a, b, rel_tol=REL_TOLERANCE):
    """Empty list == equivalent (identical after normalization, floats
    within tolerance). Messages are one line per difference."""
    msgs = []
    if a["rows"] != b["rows"]:
        msgs.append(f"rows: {a['rows']} != {b['rows']}")
    if a["columns"] != b["columns"]:
        msgs.append(f"columns: {a['columns']} != {b['columns']}")
        return msgs
    for name in a["columns"]:
        ca, cb = a["cols"][name], b["cols"][name]
        if ca["kind"] != cb["kind"]:
            msgs.append(f"col {name}: kind {ca['kind']} != {cb['kind']}")
            continue
        if ca["nulls"] != cb["nulls"]:
            msgs.append(f"col {name}: nulls {ca['nulls']} != {cb['nulls']}")
        if ca["kind"] == "num":
            for stat in ("sum", "min", "max"):
                va, vb = ca[stat], cb[stat]
                if va is None and vb is None:
                    continue
                if va is None or vb is None or not numbers_close(va, vb, rel_tol):
                    msgs.append(f"col {name}: {stat} {va} != {vb}")
        elif ca["hash"] != cb["hash"]:
            msgs.append(f"col {name}: content hash differs")
    for edge in ("head", "tail"):
        ra, rb = a[edge], b[edge]
        if len(ra) != len(rb):
            msgs.append(f"{edge}: {len(ra)} rows != {len(rb)} rows")
            continue
        for i, (rowa, rowb) in enumerate(zip(ra, rb)):
            bad = [
                j for j, (x, y) in enumerate(zip(rowa, rowb))
                if not cells_equal(x, y, rel_tol)
            ]
            if bad:
                j = bad[0]
                msgs.append(f"{edge} row {i} col {a['columns'][j]}: "
                            f"{rowa[j]!r} != {rowb[j]!r}")
    return msgs


# ---------------------------------------------------------------- selftest
#
# The fingerprint invariants are the only piece of the bench with logic
# worth pinning in isolation; everything else is tested end to end by the
# cross-protocol comparison (native@qdbd == legacy@old-rest in `report`).


def selftest_frame():
    n = 100
    frame = pd.DataFrame({
        "s": [None if k % 30 == 7 else f"s-{k},'\"<&>" for k in range(n)],
        "i": np.arange(n, dtype=np.int64) * 1_000_003,
        "d": np.arange(n, dtype=np.float64) * 0.5,
        "t": pd.date_range("2020-01-01", periods=n, freq="1h"),
    })
    frame.loc[13, "t"] = pd.NaT
    return frame


def fingerprint_of(frames):
    state = fingerprint_begin()
    for frame in frames:
        fingerprint_update(state, frame)
    return fingerprint_finish(state)


def expect(condition, label):
    if not condition:
        die(f"selftest: {label}")
    print(f"[OK] selftest: {label}", file=sys.stderr)


def selftest_main(_args):
    frame = selftest_frame()
    whole = fingerprint_of([frame])
    split = fingerprint_of([frame.iloc[i:i + 33] for i in range(0, len(frame), 33)])
    expect(fingerprint_diff(whole, split) == [], "batch boundaries are invisible")

    permuted = fingerprint_of([frame[["t", "d", "s", "i"]]])
    expect(fingerprint_diff(whole, permuted) == [], "column order is invisible")

    shuffled = fingerprint_of([frame.sample(frac=1, random_state=7)])
    edge_only = [m for m in fingerprint_diff(whole, shuffled)
                 if not m.startswith(("head", "tail"))]
    expect(fingerprint_diff(whole, shuffled) != [] and edge_only == [],
           "row order shows up only in the edge rows")

    nudged = frame.copy()
    nudged.loc[50, "d"] *= 1 + 1e-12
    expect(fingerprint_diff(whole, fingerprint_of([nudged])) == [],
           "1e-12 relative drift is within tolerance")
    bumped = frame.copy()
    bumped.loc[50, "d"] *= 1 + 1e-6
    expect(fingerprint_diff(whole, fingerprint_of([bumped])) != [],
           "1e-6 relative drift is out of tolerance")

    roundtrip = json.loads(json.dumps(whole))
    expect(fingerprint_diff(whole, roundtrip) == [],
           "fingerprints survive the JSON round trip")


# ---------------------------------------------------------------------- CLI


def parse_args(argv):
    parser = argparse.ArgumentParser(
        prog="bench.py",
        description="Assessment bench: one (protocol, server) pair per invocation.",
    )
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("selftest", help="pin the fingerprint invariants")
    return parser.parse_args(argv)


HANDLERS = {
    "selftest": selftest_main,
}


def main(argv=None):
    args = parse_args(argv)
    HANDLERS[args.command](args)


if __name__ == "__main__":
    main()
