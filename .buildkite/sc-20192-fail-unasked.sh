#!/usr/bin/env bash
# sc-20192 test branch only, never for master: proof steps 10 and 11.
# The freebsd-haswell build step fails when the build of this chain was
# not asked for freebsd-haswell-release (SNAPSHOT_NEEDS, set by the
# trigger step), and goes on when it was. Other build steps go on.
set -euo pipefail
slug="$1"
if [ "${slug}" != "freebsd-haswell" ]; then
  exit 0
fi
case "${SNAPSHOT_NEEDS:-}" in
  *'"freebsd-haswell-release"'*)
    echo "sc-20192: freebsd-haswell-release was asked for (SNAPSHOT_NEEDS=${SNAPSHOT_NEEDS}); the build goes on"
    ;;
  *)
    echo "sc-20192: freebsd-haswell-release was not asked for (SNAPSHOT_NEEDS=${SNAPSHOT_NEEDS:-unset}); this build step fails on purpose" >&2
    exit 1
    ;;
esac
