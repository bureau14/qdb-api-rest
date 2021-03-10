#!/bin/bash

set -xe

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/config.sh"

function full_cleanup {
    echo "Removing ${CONSOLE_LOG_REST} ..."
    rm -Rf ${CONSOLE_LOG_REST} || true
    echo "Removing ${CONSOLE_ERR_LOG_REST} ..."
    rm -Rf ${CONSOLE_ERR_LOG_REST} || true
}
