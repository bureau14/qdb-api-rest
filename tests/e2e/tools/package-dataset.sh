#!/usr/bin/env bash
# One-off: convert the sc-19522 qdbd data directory (db.tar.zst) into the
# distributable dataset archive described in docs/e2e-plan.md, "Dataset":
#
#   reproduce.csv            data, no header (qdb_export convention)
#   reproduce.import.json    qdb_import config, with shard_size
#   metadata.json            row count, sha256 of the csv, generation date
#
# Starts a throwaway qdbd on the extracted data dir (port 2846, never the
# shared service), exports, packages, prints the datasets.json entry and the
# S3 upload command. Upload is a manual operator step (needs AWS credentials).
#
# Usage: package-dataset.sh <db.tar.zst> <output-dir>

source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/common.sh"

TABLE="reproduce"
SRC_ARCHIVE="${1:?usage: package-dataset.sh <db.tar.zst> <output-dir>}"
OUT_DIR="${2:?usage: package-dataset.sh <db.tar.zst> <output-dir>}"
SCRATCH_PORT=2846
SCRATCH_URI="qdb://127.0.0.1:${SCRATCH_PORT}"
S3_BASE="s3://qdb-cicd-builddeps-20260226074339625300000001/datasets/qdb-api-rest"

require_command jq
require_command tar
require_command zstd
[[ -f "$SRC_ARCHIVE" ]] || die "source archive not found: $SRC_ARCHIVE"
[[ -x "$QDBD" ]] || die "qdbd not found at $QDBD (extract the qdb distribution into $QDB_DIR)"

mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/qdb-dataset.XXXXXX")"
PID_FILE="$WORK/qdbd.pid"
trap 'stop_server "$PID_FILE"; rm -rf "$WORK"' EXIT

log_info "Extracting $SRC_ARCHIVE"
mkdir -p "$WORK/src"
tar --use-compress-program=unzstd -xf "$SRC_ARCHIVE" -C "$WORK/src"
DATA_DIR="$(dirname "$(find "$WORK/src" -type d -name wal | head -1)")"
[[ -d "$DATA_DIR/db" ]] || die "could not locate a qdbd data dir (db/ + wal/) inside the archive"

log_info "Starting throwaway qdbd on $SCRATCH_URI (data dir $DATA_DIR)"
start_server "$PID_FILE" "$WORK/qdbd.log" "$SCRATCH_PORT" \
    "$QDBD" -a "127.0.0.1:${SCRATCH_PORT}" -r "$DATA_DIR" -l "$WORK/qdbd-log" --id 0-0-0-9 \
    --local-limiter-max-bytes-soft-percentage 60 --local-limiter-max-bytes-hard-percentage 80

shard_ms=$(table_shard_ms "$TABLE" "$SCRATCH_URI")
rows=$(count_qdb_rows "$TABLE" "$SCRATCH_URI")
log_info "$TABLE: $rows rows, shard size ${shard_ms}ms"

export_table_csv "$TABLE" "$WORK/$TABLE.csv" "$SCRATCH_URI" "$WORK/$TABLE.import.json"
csv_rows=$(wc -l < "$WORK/$TABLE.csv" | tr -d ' ')
[[ "$csv_rows" == "$rows" ]] || die "exported $csv_rows rows but COUNT(*) is $rows"

# qdb_import needs shard_size to create the table on a fresh cluster.
jq --arg s "${shard_ms}ms" '. + {shard_size: $s}' "$WORK/$TABLE.import.json" > "$WORK/import.tmp" \
    && mv "$WORK/import.tmp" "$WORK/$TABLE.import.json"

csv_sha=$(sha256_file "$WORK/$TABLE.csv")
date_tag=$(date -u +%Y-%m-%d)
jq -n --arg table "$TABLE" --argjson rows "$rows" --arg sha "$csv_sha" --arg shard "${shard_ms}ms" \
      --arg gen "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg qdbd "$("$QDBD" --version 2>&1 | grep -m1 version)" \
      '{table: $table, rows: $rows, csv_sha256: $sha, shard_size: $shard, generated_at: $gen,
        qdbd_version: $qdbd, source: "sc-19522 db.tar.zst"}' > "$WORK/metadata.json"

archive="$TABLE-$date_tag-$rows.tar.gz"
tar -czf "$OUT_DIR/$archive" -C "$WORK" "$TABLE.csv" "$TABLE.import.json" metadata.json
archive_sha=$(sha256_file "$OUT_DIR/$archive")

log_info "Wrote $OUT_DIR/$archive"
cat <<MSG

datasets.json entry:
  { "name": "$TABLE", "date": "$date_tag", "rows": $rows, "sha256": "$archive_sha" }

upload (manual, needs AWS credentials):
  aws s3 cp "$OUT_DIR/$archive" "$S3_BASE/$archive"

until uploaded, point the harness at the local copy:
  make -C tests/e2e load DATASETS_LOCAL_DIR=$OUT_DIR
MSG
