"""Old REST server (built from master in the tests/e2e worktree): the
baseline every comparison is measured against."""


def server_cmd(cfg):
    # --local pins 127.0.0.1:40080 and overrides --port; the pool and
    # buffer sizes let a 5.6M-row query through (docs/bench-plan.md,
    # "Server lifecycle"). --log-file is the application log; stdout and
    # stderr go to the harness's own log next to it.
    return [
        cfg.binary,
        "--local",
        "-c", cfg.cluster_uri,
        "--pool-size", "4",
        "--parallelism-count", "4",
        "--max-in-buffer-size", str(cfg.max_in_buf_size),
        "--log-file", cfg.log_file,
    ]
