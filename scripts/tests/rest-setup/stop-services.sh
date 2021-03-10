#!/bin/bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/config.sh"
source "$SCRIPT_DIR/utils.sh"
source "$SCRIPT_DIR/cleanup.sh"

kill_instances

instances_still_running=$(count_instances)
if [[ $((${instances_still_running})) != 0 ]] ; then
    echo "${instances_still_running} ${QDB_REST_FILENAME} instance(s) were not killed properly"
    exit 1
fi
