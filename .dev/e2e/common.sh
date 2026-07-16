#!/usr/bin/env bash

# Shared plumbing for the e2e scripts (.dev/e2e/run.sh and .dev/e2e/stack.sh): the backend env defaults,
# the dev-container exec wrappers and the check pass/fail accounting. Source it; never execute it.
#
# How to add a new check (stack.sh style):
#   1. section_start "MY CHECK" "${TAG_VALIDATE}" "e2e"
#   2. run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . my:command"
#      then read RUN_IN_DEV_OUTPUT_STRING / RUN_IN_DEV_STATUS_INTEGER (never call it inside "$(...)")
#   3. assert with check_pass "..." / check_fail "..." — every check needs a reachable fail branch;
#      a check that cannot fail is a bug
#   4. section_end "MY CHECK" "success" "${TAG_VALIDATE}" "e2e" and keep finish_checks last in the script

if [[ "1" = "${MELODY_E2E_COMMON_SOURCED:-0}" ]]; then
    return 0
fi

MELODY_E2E_COMMON_SOURCED="1"
readonly MELODY_E2E_COMMON_SOURCED

E2E_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "${E2E_DIRECTORY_STRING}/../.." && pwd)"

. "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/utility.sh"

E2E_SERVICE_NAME_STRING="dev"

# paths as seen from inside the dev container
E2E_HARNESS_DIRECTORY_STRING="/app/.dev/e2e"
EXAMPLE_DIRECTORY_STRING="/app/v3/.example"
EXAMPLE_ENV_LOCAL_PATH_STRING="${EXAMPLE_DIRECTORY_STRING}/.env.local"

# backend env defaults — the single source of truth for every e2e script. Each value is overridable from
# the environment, and the defaults address the compose services by their service name on the compose
# network, as seen from inside the dev container. The clear-to-skip contract (clear one to skip the
# sections it gates) applies to run.sh's in-process harness; stack.sh exercises the full compose stack
# and requires every backend up, so clearing a variable there does not skip its sections.
REDIS_ADDRESS="${REDIS_ADDRESS-redis:6379}"
POSTGRES_DSN="${POSTGRES_DSN-postgres://melody:melody@postgres:5432/melody_test?sslmode=disable}"
AMQP_DSN="${AMQP_DSN-amqp://guest:guest@rabbitmq:5672/}"
SMTP_ADDRESS="${SMTP_ADDRESS-mailpit:1025}"
MAILPIT_API_URL="${MAILPIT_API_URL-http://mailpit:8025}"
EXAMPLE_BASE_URL="${EXAMPLE_BASE_URL-http://127.0.0.1:8080}"
EXAMPLE_LOAD_BALANCER_URL="${EXAMPLE_LOAD_BALANCER_URL-http://load-balancer:80}"

CHECK_FAILURE_COUNT_INTEGER=0

RUN_IN_DEV_OUTPUT_STRING=""
RUN_IN_DEV_STATUS_INTEGER=0

e2e_require_dev_service() {
    require_docker
    require_docker_daemon

    if ! docker_compose_service_exists "${E2E_SERVICE_NAME_STRING}"; then
        fail "missing docker compose service: ${E2E_SERVICE_NAME_STRING}"
    fi

    ensure_service_running "${E2E_SERVICE_NAME_STRING}"
}

# builds the in-container command line: go on the PATH, GOWORK=off, the backend env exported, then cd into
# the working directory. The cd is a statement of its own: joining it to the command with && would make a
# trailing & background the whole chain, leaving the foreground shell outside the working directory.
e2e_dev_command() {
    local DIRECTORY_STRING="${1:?}"
    local COMMAND_STRING="${2:?}"

    printf '%s' \
        "export PATH=/usr/local/go/bin:\$PATH" \
        " GOWORK=off" \
        " REDIS_ADDRESS='${REDIS_ADDRESS}'" \
        " POSTGRES_DSN='${POSTGRES_DSN}'" \
        " AMQP_DSN='${AMQP_DSN}'" \
        " SMTP_ADDRESS='${SMTP_ADDRESS}'" \
        " MAILPIT_API_URL='${MAILPIT_API_URL}'" \
        " EXAMPLE_BASE_URL='${EXAMPLE_BASE_URL}'" \
        " EXAMPLE_LOAD_BALANCER_URL='${EXAMPLE_LOAD_BALANCER_URL}'" \
        "; cd ${DIRECTORY_STRING} || exit 1; ${COMMAND_STRING}"
}

# runs a command in the dev container with the shared env, echoing the docker command it runs and streaming
# the output to the terminal; the exit status is the command's own, so use it for top-level steps
run_in_dev() {
    local DIRECTORY_STRING="${1:?}"
    local COMMAND_STRING="${2:?}"

    run_in_service_shell "${E2E_SERVICE_NAME_STRING}" "$(e2e_dev_command "${DIRECTORY_STRING}" "${COMMAND_STRING}")"
}

# runs a command in the dev container with the shared env and captures stdout into RUN_IN_DEV_OUTPUT_STRING
# and the exit status into RUN_IN_DEV_STATUS_INTEGER, without tripping set -euo pipefail and without the
# docker command echo run_in_service_shell would mix into the captured output. Never call it inside a
# command substitution: the subshell would discard both variables.
run_in_dev_capture() {
    local DIRECTORY_STRING="${1:?}"
    local COMMAND_STRING="${2:?}"

    RUN_IN_DEV_OUTPUT_STRING=""
    RUN_IN_DEV_STATUS_INTEGER=0

    if RUN_IN_DEV_OUTPUT_STRING="$(
        docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" bash -c \
            "$(e2e_dev_command "${DIRECTORY_STRING}" "${COMMAND_STRING}")" </dev/null
    )"; then
        RUN_IN_DEV_STATUS_INTEGER=0
    else
        RUN_IN_DEV_STATUS_INTEGER=$?
    fi
}

check_pass() {
    printf 'PASS  %s\n' "${1}"
}

check_fail() {
    printf 'FAIL  %s\n' "${1}" >&2
    CHECK_FAILURE_COUNT_INTEGER="$((CHECK_FAILURE_COUNT_INTEGER + 1))"
}

# prints the summary and exits non-zero when any check failed; the label names the check family ("stack")
finish_checks() {
    local LABEL_STRING="${1:?}"

    if [[ 0 -lt ${CHECK_FAILURE_COUNT_INTEGER} ]]; then
        fail "${CHECK_FAILURE_COUNT_INTEGER} ${LABEL_STRING} check(s) failed"
    fi

    printf '\nALL %s CHECKS PASSED\n' "$(utility_to_upper "${LABEL_STRING}")"
}
