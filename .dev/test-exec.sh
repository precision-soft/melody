#!/usr/bin/env bash

# go test -exec wrapper: kills a test binary above MELODY_TEST_MEMORY_LIMIT resident; RSS, because GOMEMLIMIT never
# stops a process and an address-space ulimit breaks -race

set -uo pipefail

BINARY_PATH_STRING="${1:-}"

if [[ "${BINARY_PATH_STRING}" != *.test ]]; then
    exec "$@"
fi

LIMIT_STRING="${MELODY_TEST_MEMORY_LIMIT:-6G}"

limit_kibibytes() {
    local VALUE_STRING="${1^^}"
    local NUMBER_STRING="${VALUE_STRING%[KMGT]}"
    local SUFFIX_STRING="${VALUE_STRING#"${NUMBER_STRING}"}"

    if [[ ! "${NUMBER_STRING}" =~ ^[0-9]+$ ]]; then
        echo "MELODY_TEST_MEMORY_LIMIT must be a whole number with an optional K, M, G or T suffix, got '${1}'" >&2
        exit 2
    fi

    case "${SUFFIX_STRING}" in
        K) echo "${NUMBER_STRING}" ;;
        M) echo "$((NUMBER_STRING * 1024))" ;;
        G) echo "$((NUMBER_STRING * 1024 * 1024))" ;;
        T) echo "$((NUMBER_STRING * 1024 * 1024 * 1024))" ;;
        *) echo "$((NUMBER_STRING / 1024))" ;;
    esac
}

LIMIT_KIBIBYTES=$(limit_kibibytes "${LIMIT_STRING}") || exit 2

if [[ "0" == "${LIMIT_KIBIBYTES}" ]]; then
    exec "$@"
fi

"$@" &
TEST_PID=$!

# go test signals the process it started on its own timeout and on an interrupt, which is this wrapper
trap 'kill -s QUIT "${TEST_PID}" 2>/dev/null' QUIT
trap 'kill -s TERM "${TEST_PID}" 2>/dev/null' TERM
trap 'kill -s INT "${TEST_PID}" 2>/dev/null' INT

while kill -0 "${TEST_PID}" 2>/dev/null; do
    RSS_KIBIBYTES=$(awk '/^VmRSS:/ { print $2 }' "/proc/${TEST_PID}/status" 2>/dev/null || true)

    if [[ -n "${RSS_KIBIBYTES}" ]] && (( RSS_KIBIBYTES > LIMIT_KIBIBYTES )); then
        kill -s KILL "${TEST_PID}" 2>/dev/null
        wait "${TEST_PID}" 2>/dev/null
        echo "memory limit exceeded: ${PWD} $(basename "${BINARY_PATH_STRING}") rss=$((RSS_KIBIBYTES / 1024))M limit=${LIMIT_STRING} (MELODY_TEST_MEMORY_LIMIT)" >&2
        exit 137
    fi

    # the sleep runs aside so the loop wakes the moment the test ends, not at the end of the interval
    sleep 0.5 &
    wait -n "${TEST_PID}" "$!" 2>/dev/null
done

wait "${TEST_PID}"
