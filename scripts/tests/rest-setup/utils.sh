#!/bin/bash

set -eux

SCRIPT_DIR="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null && pwd)"
source "$SCRIPT_DIR/binaries.sh"

function count_instances {
    local instances_count=$(($(ps aux | grep qdb_rest | grep -v "grep" | wc -l)))
    echo ${instances_count}
}

function qdb_rest_start {
    local args=$1; shift
    local output=$1; shift
    local err_output=$1; shift

    echo "Starting ${QDB_REST} with args: ${args}"
    echo "Redirecting output to ${output}"
    echo "Redirecting error output to ${err_output}"

    $QDB_REST ${args} 1>${output} 2>${err_output} &
}

function qdbsh_seed_db {
    echo "Seeding ${QDB_SHELL} with data"

    $QDB_SHELL -c 'CREATE TABLE foo_01 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO foo_01 ($timestamp, value) VALUES (2020-01-01, 1.0), (2020-01-02, 2.0), (2020-01-03, 3.0)'
    $QDB_SHELL -c 'attach_tag foo_01 tag_01'

    $QDB_SHELL -c 'CREATE TABLE foo_02 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO foo_02 ($timestamp, value) VALUES (2020-01-01, 4.0), (2020-01-02, 5.0), (2020-01-03, 6.0)'
    $QDB_SHELL -c 'attach_tag foo_02 tag_02'

    $QDB_SHELL -c 'CREATE TABLE foo_03 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO foo_03 ($timestamp, value) VALUES (2020-01-01, 7.0), (2020-01-02, 8.0), (2020-01-03, 9.0)'
    $QDB_SHELL -c 'attach_tag foo_03 tag_03'

    $QDB_SHELL -c 'CREATE TABLE bar_01 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO bar_01 ($timestamp, value) VALUES (2020-01-01, 10.0), (2020-01-02, 11.0), (2020-01-03, 12.0)'
    $QDB_SHELL -c 'attach_tag bar_01 tag_01'

    $QDB_SHELL -c 'CREATE TABLE bar_02 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO bar_02 ($timestamp, value) VALUES (2020-01-01, 13.0), (2020-01-02, 14.0), (2020-01-03, 15.0)'
    $QDB_SHELL -c 'attach_tag bar_02 tag_02'

    $QDB_SHELL -c 'CREATE TABLE bar_03 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO bar_03 ($timestamp, value) VALUES (2020-01-01, 16.0), (2020-01-02, 17.0), (2020-01-03, 18.0)'
    $QDB_SHELL -c 'attach_tag bar_03 tag_03'

    $QDB_SHELL -c 'CREATE TABLE baz_01 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO baz_01 ($timestamp, value) VALUES (2020-01-01, 19.0), (2020-01-02, 20.0), (2020-01-03, 21.0)'
    $QDB_SHELL -c 'attach_tag baz_01 tag_01'

    $QDB_SHELL -c 'CREATE TABLE baz_02 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO baz_02 ($timestamp, value) VALUES (2020-01-01, 22.0), (2020-01-02, 23.0), (2020-01-03, 24.0)'
    $QDB_SHELL -c 'attach_tag baz_02 tag_02'

    $QDB_SHELL -c 'CREATE TABLE baz_03 (value DOUBLE)'
    $QDB_SHELL -c 'INSERT INTO baz_03 ($timestamp, value) VALUES (2020-01-01, 25.0), (2020-01-02, 26.0), (2020-01-03, 27.0)'
    $QDB_SHELL -c 'attach_tag baz_03 tag_03'

    $QDB_SHELL -c 'attach_tag tag_01 $qdb.tagroot'
    $QDB_SHELL -c 'attach_tag tag_02 $qdb.tagroot'
    $QDB_SHELL -c 'attach_tag tag_03 $qdb.tagroot'
}

function check_address {
    local address=$1;shift
    case "$(uname)" in
        MINGW*)
            local pid=$(NETSTAT.EXE -an -o | grep $address | grep "LISTENING" | tr -s [:space:] | cut -d' ' -f6)
            if [[ $pid != "" ]]; then
                ps aux | grep $pid | grep qdb_rest
            fi
        ;;
        FreeBSD*)
            sockstat -l | grep $address | tr -s [:space:] | cut -d' ' -f2
        ;;
        *)
            lsof -i -n -P | grep $address | grep LISTEN | cut -d' ' -f1
        ;;
    esac
    echo ""
}

function kill_instances {
    echo "Killing ${QDB_REST_FILENAME} instances..."
    case "$(uname)" in
        MINGW*)
            # we need double slashes for the flag to be recognized
            # a simple slash would cause this error: Invalid argument/option - 'C:/Program Files/Git/IM'.
            #
            # See http://www.mingw.org/wiki/Posix_path_conversion
            Taskkill //IM ${QDB_REST_FILENAME} //F || true
        ;;
        *)
            pkill -SIGKILL -f ${QDB_REST_FILENAME} || true
        ;;
    esac
    sleep 5
    if [[ $(($(count_instances))) != 0 ]]; then
        echo "Could not kill all instances, aborting..."
        exit 1
    fi
}
