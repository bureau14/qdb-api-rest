#!/bin/bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/config.sh" $@ # important: pass $@ as config.sh also parses our runtime args
source "$SCRIPT_DIR/utils.sh"
source "$SCRIPT_DIR/cleanup.sh"

kill_instances
full_cleanup

echo "qdbsh seeding"
qdbsh_seed_db

echo "qdb rest:"
ARGS_REST=""
qdb_rest_start "${ARGS_REST}" ${CONSOLE_LOG_REST} ${CONSOLE_ERR_LOG_REST}

sleep_time=5
timeout=60
end_time=$(($(date +%s) + $timeout))
SUCCESS=0
while [ $(date +%s) -le $end_time ]; do
    insecure_check=$(check_address $URI_REST_INSECURE)
    
    if [[ $insecure_check != ""  ]]; then
        echo "qdb_rest was started properly."
        SUCCESS=1
        break
    fi

    sleep $sleep_time
done

if [[ "${SUCCESS}" == "0" ]]
then
    echo "Could not start all instances, aborting..."
    exit 1
fi