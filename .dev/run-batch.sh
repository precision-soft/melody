#!/usr/bin/env bash
set -uo pipefail
IFS=$'\n\t'

TOTAL_INTEGER="$#"

if [[ 0 -eq ${TOTAL_INTEGER} ]]; then
    printf '[ batch ][ error ] no commands provided\n' >&2
    exit 1
fi

COMMAND_INDEX_INTEGER=0

for COMMAND_STRING in "$@"; do
    COMMAND_INDEX_INTEGER="$((COMMAND_INDEX_INTEGER + 1))"

    printf '[ batch ][ %d / %d ][ run ] %s\n' \
        "${COMMAND_INDEX_INTEGER}" "${TOTAL_INTEGER}" "${COMMAND_STRING}"

    EXIT_CODE_INTEGER=0
    bash -c "${COMMAND_STRING}" || EXIT_CODE_INTEGER=$?

    if [[ 0 -ne ${EXIT_CODE_INTEGER} ]]; then
        printf '[ batch ][ %d / %d ][ failed ] %s\n' \
            "${COMMAND_INDEX_INTEGER}" "${TOTAL_INTEGER}" "${COMMAND_STRING}" >&2
        exit "${EXIT_CODE_INTEGER}"
    fi

    printf '[ batch ][ %d / %d ][ ok ]\n' "${COMMAND_INDEX_INTEGER}" "${TOTAL_INTEGER}"
done
