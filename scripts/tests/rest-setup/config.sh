#!/bin/bash

set -xe

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

# Static config

URI_REST_SECURE="127.0.0.1:40443"
URI_REST_INSECURE="127.0.0.1:40080"
CONFIG_REST="${SCRIPT_DIR}/default.rest.cfg"
CONSOLE_LOG_REST="rest_log.out.txt"
CONSOLE_ERR_LOG_REST="rest_log.err.txt"
