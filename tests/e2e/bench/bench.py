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
import hashlib
import importlib
import json
import math
import os
import pathlib
import platform
import resource
import socket
import subprocess
import sys
import time
from collections import deque
from datetime import datetime, timezone
from types import SimpleNamespace

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
        if acc["nulls"] == state["rows"]:
            # An all-null column has no observable type on the wire (the
            # legacy JSON types it "none", the native client picks a dtype);
            # fingerprint it type-free so protocols cannot disagree.
            cols[name] = {"kind": "null", "nulls": acc["nulls"]}
            continue
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
        elif ca["kind"] != "null" and ca["hash"] != cb["hash"]:
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


# ---------------------------------------------------------- process sampling
#
# `ps` is the portable source of RSS and CPU time on macOS (there is no
# /proc); one fork samples every pid of interest at once.


def rss_bytes(pids):
    """Current RSS per pid; pids that exited are simply absent."""
    listed = ",".join(str(p) for p in pids)
    out = subprocess.run(
        ["ps", "-o", "pid=,rss=", "-p", listed],
        capture_output=True, text=True,
    ).stdout
    result = {}
    for line in out.splitlines():
        pid, rss_kib = line.split()
        result[int(pid)] = int(rss_kib) * 1024
    return result


def cpu_seconds(pid):
    """user+system CPU of one process; `ps -o cputime=` prints
    [dd-]hh:mm:ss.cc with leading fields omitted when zero."""
    out = subprocess.run(
        ["ps", "-o", "cputime=", "-p", str(pid)],
        capture_output=True, text=True,
    ).stdout.strip()
    if not out:
        return None
    days, _, clock = out.rpartition("-")
    seconds = 0.0
    for field in clock.split(":"):
        seconds = seconds * 60 + float(field)
    return (int(days) * 86400 if days else 0) + seconds


# ------------------------------------------------------------- qdbd counters
#
# Volume 1 of the gateway thesis: what qdbd pushes to whichever process
# holds the C API handle (the Python client for native runs, the REST
# server otherwise). The counters are node-global; attribution works
# because exactly one (protocol, server) pair runs at a time.


def direct_node(conn, cluster_uri):
    """Direct connection to the single qdbd node behind the cluster URI."""
    return conn.node(cluster_uri.removeprefix("qdb://"))


def request_counters(dconn):
    """Cumulative (out_bytes, total_count) of the qdbd node. Read key by
    key on the direct connection: the read itself then costs a couple of
    requests, small enough for the baseline subtraction to be exact-ish
    even for tiny queries."""
    return (
        dconn.integer("$qdb.statistics.requests.out_bytes").get(),
        dconn.integer("$qdb.statistics.requests.total_count").get(),
    )


def counter_baseline(dconn):
    """Cost of one counter read cycle plus a settle interval's connection
    noise. Every measurement's delta contains exactly one such cycle (the
    read that closes it pays for the read that opened it), so this is
    subtracted once per measurement."""
    before = request_counters(dconn)
    time.sleep(COUNTER_SETTLE_SECONDS)
    after = request_counters(dconn)
    return (after[0] - before[0], after[1] - before[1])


# ---------------------------------------------------------- server lifecycle
#
# bench.py owns the REST server of its run: it needs the pid for RSS
# sampling, and a fresh server per measurement resets the RSS baseline.
# qdbd is a shared service and is never started or stopped here.


def port_open(port):
    with socket.socket() as sock:
        sock.settimeout(0.5)
        return sock.connect_ex(("127.0.0.1", port)) == 0


def wait_for_port(port, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if port_open(port):
            return True
        time.sleep(0.2)
    return False


def start_server(cmd, port, log_path, pid_path):
    if port_open(port):
        die(f"port {port} is already in use; stop the stray server first")
    env = dict(os.environ, TZ="UTC")  # legacy JSON renders server-local time
    with open(log_path, "ab") as log_file:
        proc = subprocess.Popen(cmd, stdout=log_file, stderr=log_file, env=env)
    pathlib.Path(pid_path).write_text(f"{proc.pid}\n")
    if not wait_for_port(port):
        stop_server(proc, pid_path)
        die(f"server did not answer on port {port}; log: {log_path}")
    return proc


def stop_server(proc, pid_path):
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    pathlib.Path(pid_path).unlink(missing_ok=True)


# -------------------------------------------------------------------- child
#
# Each measurement runs in a fresh process so RSS and CPU belong to exactly
# one fetch. The clock and the CPU meter cover only the fetch-iterator
# pulls: fingerprint accumulation between pulls is bench overhead, outside
# both -- symmetric with one-shot protocols, where it happens after the
# final pull.


def child_main(args):
    cfg = json.loads(args.config)
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    protocol = importlib.import_module(f"protocols.{cfg['protocol']}")

    started = None
    ttfb = {"seconds": None}

    def record_ttfb():
        ttfb["seconds"] = time.perf_counter() - started

    state = fingerprint_begin()
    telemetry = {}
    frames = protocol.fetch(SimpleNamespace(**cfg), cfg["query"], record_ttfb, telemetry)
    rows = batches = 0
    wall = cpu = 0.0
    while True:
        usage_before = resource.getrusage(resource.RUSAGE_SELF)
        pull_started = time.perf_counter()
        if started is None:
            started = pull_started
        try:
            frame = next(frames)
        except StopIteration:
            frame = None
        wall += time.perf_counter() - pull_started
        usage_after = resource.getrusage(resource.RUSAGE_SELF)
        cpu += (usage_after.ru_utime - usage_before.ru_utime) \
             + (usage_after.ru_stime - usage_before.ru_stime)
        if frame is None:
            break
        rows += len(frame)
        batches += 1
        fingerprint_update(state, frame)
        del frame

    definition = protocol.TTFB_DEFINITION
    if isinstance(definition, dict):
        definition = definition[cfg["mode"]]
    print(json.dumps({
        "mode": cfg["mode"],
        "rows": rows,
        "batches": batches,
        "wall_to_dataframe_seconds": wall,
        "ttfb_seconds": ttfb["seconds"] if ttfb["seconds"] is not None else wall,
        "ttfb_definition": definition,
        "client_cpu_seconds": cpu,
        "telemetry": telemetry,
        "fingerprint": fingerprint_finish(state),
    }), flush=True)


# ------------------------------------------------------------------- parent


def parse_run(args):
    protocol, _, server = args.run.partition("@")
    if (protocol, server) not in REGISTRY:
        valid = ", ".join(f"{p}@{s}" for p, s in REGISTRY)
        die(f"unknown run '{args.run}'; valid runs: {valid}")
    gate = REGISTRY[(protocol, server)]
    if gate:
        die(f"{args.run}: {gate}", code=2)
    return protocol, server


def connect_qdbd(args):
    import quasardb

    try:
        conn = quasardb.Cluster(args.cluster)
    except Exception as error:
        die(f"qdbd is not answering on {args.cluster} ({error}); "
            "run: bash scripts/tests/setup/start-services.sh")
    rows = conn.query(f'SELECT COUNT(id) FROM "{DATASET_TABLE}"')
    have = next(iter(rows[0].values())) if rows else 0
    if have != DATASET_ROWS:
        die(f"table {DATASET_TABLE} has {have} rows, expected {DATASET_ROWS}; "
            "run: make -C tests/e2e load")
    return conn


def file_sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def git_head(repo_dir):
    return subprocess.run(
        ["git", "-C", repo_dir, "rev-parse", "HEAD"],
        capture_output=True, text=True, check=True,
    ).stdout.strip()


def capi_library(qdb_dir):
    for name in ("libqdb_api.dylib", "libqdb_api.so"):
        path = os.path.join(qdb_dir, "lib", name)
        if os.path.exists(path):
            return path
    die(f"no libqdb_api under {qdb_dir}/lib")


def environment_block(args):
    """Everything needed to judge whether two result files are comparable."""
    import quasardb

    qdbd_version = subprocess.run(
        [os.path.join(args.qdb_dir, "bin", "qdbd"), "--version"],
        capture_output=True, text=True,
    ).stdout.strip()
    return {
        "timestamp": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "machine": platform.platform(),
        "cpu_count": os.cpu_count(),
        "python": platform.python_version(),
        "quasardb": quasardb.version(),
        "pandas": pd.__version__,
        "capi_sha256": file_sha256(capi_library(args.qdb_dir)),
        "capi_compression": CAPI_COMPRESSION,
        "qdbd_version": qdbd_version,
        "git": {
            "qdb-api-rest": git_head(args.repo_root),
            "old-master": git_head(args.old_master_dir),
            "qdb-api-python": git_head(args.qdb_api_python_dir),
        },
    }


def server_spec(args, server):
    """(cmd, port, log_path, pid_path) for the REST server of the run;
    None when qdbd itself answers."""
    if server == "qdbd":
        return None
    bench_dir = os.path.dirname(os.path.abspath(__file__))
    module = importlib.import_module(f"servers.{server.replace('-', '_')}")
    binary = args.old_rest_bin if server == "old-rest" else args.new_rest_bin
    port = args.old_rest_port if server == "old-rest" else args.new_rest_port
    if not binary or not os.access(binary, os.X_OK):
        die(f"no executable server binary '{binary}'; run: make old-server")
    cmd = module.server_cmd(SimpleNamespace(
        binary=binary,
        cluster_uri=args.cluster,
        port=port,
        max_in_buf_size=MAX_IN_BUF_SIZE,
        log_file=os.path.join(bench_dir, f"{server}-app.log"),
    ))
    return cmd, port, os.path.join(bench_dir, f"{server}.log"), \
        os.path.join(bench_dir, f"{server}.pid")


def child_config(args, protocol, server, query_id, mode):
    port = args.old_rest_port if server == "old-rest" else args.new_rest_port
    return {
        "protocol": protocol,
        "mode": mode,
        "query": QUERIES[query_id],
        "cluster_uri": args.cluster,
        "base_url": f"http://127.0.0.1:{port}",
        "gzip": not args.no_gzip,
        "batch_size": STREAM_BATCH_SIZE,
        "max_in_buf_size": MAX_IN_BUF_SIZE,
    }


def run_measurement(args, dconn, baseline, spec, cfg):
    """One (query, mode, repetition): fresh REST server, fresh child,
    RSS sampled from outside, counter deltas around the child."""
    server_proc = None
    if spec:
        cmd, port, log_path, pid_path = spec
        server_proc = start_server(cmd, port, log_path, pid_path)
    try:
        time.sleep(COUNTER_SETTLE_SECONDS)
        counters_before = request_counters(dconn)
        server_cpu_before = cpu_seconds(server_proc.pid) if server_proc else None

        child = subprocess.Popen(
            [sys.executable, os.path.abspath(__file__), "_child", json.dumps(cfg)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        pids = [child.pid] + ([server_proc.pid] if server_proc else [])
        peaks = {pid: 0 for pid in pids}
        while child.poll() is None:
            for pid, rss in rss_bytes(pids).items():
                peaks[pid] = max(peaks[pid], rss)
            time.sleep(SAMPLE_INTERVAL_SECONDS)
        out, err = child.communicate()
        if child.returncode != 0:
            die(f"measurement child failed ({child.returncode}):\n{err}")

        time.sleep(COUNTER_SETTLE_SECONDS)
        counters_after = request_counters(dconn)
        server_cpu_after = cpu_seconds(server_proc.pid) if server_proc else None
    finally:
        if server_proc:
            stop_server(server_proc, spec[3])

    record = json.loads(out)
    record["qdbd_out_bytes"] = max(0, counters_after[0] - counters_before[0] - baseline[0])
    record["qdbd_request_count"] = max(0, counters_after[1] - counters_before[1] - baseline[1])
    record["client_peak_rss_bytes"] = peaks[child.pid]
    if server_proc:
        record["server_peak_rss_bytes"] = peaks[server_proc.pid]
        record["server_cpu_seconds"] = server_cpu_after - server_cpu_before
    # native's client is the reducer: what qdbd sends IS what the client
    # receives; every other protocol reports its own wire bytes.
    record["client_bytes"] = (
        record["qdbd_out_bytes"]
        if cfg["protocol"] == "native"
        else record["telemetry"].get("response_bytes")
    )
    return record


def mean_of(records, key):
    values = [r[key] for r in records if r.get(key) is not None]
    return sum(values) / len(values) if values else None


MEAN_KEYS = (
    "wall_to_dataframe_seconds", "ttfb_seconds", "client_cpu_seconds",
    "client_peak_rss_bytes", "server_peak_rss_bytes", "server_cpu_seconds",
    "qdbd_out_bytes", "qdbd_request_count", "client_bytes",
)


def summarize_query(reps):
    """Per-mode means, plus the single fingerprint all repetitions must
    share (a mismatch is nondeterminism and kills the run)."""
    reference = reps[0]["fingerprint"]
    for rep in reps[1:]:
        diff = fingerprint_diff(reference, rep["fingerprint"])
        if diff:
            die("fingerprint differs between repetitions/modes:\n  " + "\n  ".join(diff))
    for rep in reps:
        del rep["fingerprint"]
    modes = sorted({rep["mode"] or "" for rep in reps})
    means = {
        mode or "": {
            key: mean_of([r for r in reps if (r["mode"] or "") == mode], key)
            for key in MEAN_KEYS
        }
        for mode in modes
    }
    return {"fingerprint": reference, "reps": reps, "means": means}


def run_main(args):
    protocol, server = parse_run(args)
    query_ids = args.queries.split(",") if args.queries else list(QUERIES)
    unknown = [q for q in query_ids if q not in QUERIES]
    if unknown:
        die(f"unknown queries {unknown}; known: {', '.join(QUERIES)}")

    conn = connect_qdbd(args)
    dconn = direct_node(conn, args.cluster)
    baseline = counter_baseline(dconn)
    spec = server_spec(args, server)
    modes = ("query", "stream") if protocol == "native" else (None,)

    queries = {}
    for query_id in query_ids:
        reps = []
        for mode in modes:
            for rep in range(args.reps):
                label = f" mode={mode}" if mode else ""
                log(f"{args.run} {query_id}{label} rep {rep + 1}/{args.reps}")
                cfg = child_config(args, protocol, server, query_id, mode)
                record = run_measurement(args, dconn, baseline, spec, cfg)
                record["rep"] = rep
                reps.append(record)
        queries[query_id] = {"sql": QUERIES[query_id], **summarize_query(reps)}

    os.makedirs(args.results_dir, exist_ok=True)
    path = os.path.join(args.results_dir, f"{args.run}.json")
    with open(path, "w") as handle:
        json.dump({
            "run": args.run,
            "protocol": protocol,
            "server": server,
            "environment": environment_block(args),
            "counter_baseline": {"out_bytes": baseline[0], "total_count": baseline[1]},
            "queries": queries,
        }, handle, indent=1)
        handle.write("\n")
    log(f"wrote {path}")


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

    as_floats = frame.assign(n=np.full(len(frame), np.nan))
    as_objects = frame.assign(n=pd.Series([None] * len(frame), dtype=object))
    expect(fingerprint_diff(fingerprint_of([as_floats]),
                            fingerprint_of([as_objects])) == [],
           "all-null columns fingerprint type-free")

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

    run = sub.add_parser("run", help="measure one (protocol, server) pair")
    run.add_argument("--run", required=True, help="<protocol>@<server>, e.g. legacy@old-rest")
    run.add_argument("--cluster", required=True)
    run.add_argument("--qdb-dir", required=True)
    run.add_argument("--old-rest-bin", required=True)
    run.add_argument("--old-rest-port", type=int, required=True)
    run.add_argument("--new-rest-bin", default="")
    run.add_argument("--new-rest-port", type=int, required=True)
    run.add_argument("--flight-port", type=int, required=True)
    run.add_argument("--repo-root", required=True)
    run.add_argument("--old-master-dir", required=True)
    run.add_argument("--qdb-api-python-dir", required=True)
    run.add_argument("--reps", type=int, required=True)
    run.add_argument("--queries", default="", help="comma list; empty = all")
    run.add_argument("--results-dir", required=True)
    run.add_argument("--no-gzip", action="store_true",
                     help="drop Accept-Encoding on legacy requests")

    child = sub.add_parser("_child")  # internal: one measurement, one process
    child.add_argument("config")

    sub.add_parser("selftest", help="pin the fingerprint invariants")
    return parser.parse_args(argv)


HANDLERS = {
    "run": run_main,
    "_child": child_main,
    "selftest": selftest_main,
}


def main(argv=None):
    args = parse_args(argv)
    HANDLERS[args.command](args)


if __name__ == "__main__":
    main()
