"""Native protocol: the `quasardb` Python package straight against qdbd.

The reference the gateway is chasing -- the client process holds the C API
handle, so the reduce phase runs on the client and volume 1 lands on the
client's link. Streaming only: `stream_query` (sc-19522) pulls batches;
the one-shot `qdb_query` mode was dropped 2026-08-24 (decision log in
docs/bench-plan.md)."""

import quasardb
import quasardb.pandas as qdbpd

TTFB_DEFINITION = "return of the first batch from stream_query"


def fetch(cfg, query, record_ttfb, telemetry):
    with quasardb.Cluster(cfg.cluster_uri) as conn:
        # Parity with the REST servers' --max-in-buffer-size: every run
        # must accept the same result sizes (the binding default is far
        # below what `full` needs).
        conn.options().set_client_max_in_buf_size(cfg.max_in_buf_size)
        first = True
        for frame in qdbpd.stream_query(conn, query, batch_size=cfg.batch_size):
            if first:
                record_ttfb()
                first = False
            yield frame
