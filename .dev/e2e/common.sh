#!/usr/bin/env bash

# Shared plumbing for the e2e scripts (.dev/e2e/run.sh and .dev/e2e/stack.sh): the backend env defaults,
# the dev-container exec wrappers and the check pass/fail accounting. Source it; never execute it.
#
# How to add a new check (stack.sh style):
#   1. check_section_start "MY CHECK" "${TAG_VALIDATE}" "e2e"
#   2. run_in_dev_capture "$(e2e_example_directory 3)" "go run . my:command"   # name the major the check drives
#      then read RUN_IN_DEV_OUTPUT_STRING / RUN_IN_DEV_STATUS_INTEGER (never call it inside "$(...)")
#   3. assert with check_pass "..." / check_fail "..." — every check needs a reachable fail branch;
#      a check that cannot fail is a bug
#   4. check_section_end "MY CHECK" "${TAG_VALIDATE}" "e2e" — it reports the status the section's own checks
#      earned, so never call section_end with a hardcoded "success" from a check script
#   5. keep finish_checks last in the script and raise its expected check count by the number of checks added

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

# the .example application of every major, addressed by tree position rather than by one hardcoded directory:
# e2e_example_directory <major> resolves it, so a check states which major it drives instead of inheriting one.
EXAMPLE_DIRECTORY_V1_STRING="/app/.example"
EXAMPLE_DIRECTORY_V2_STRING="/app/v2/.example"
EXAMPLE_DIRECTORY_V3_STRING="/app/v3/.example"

# the majors the example-driving coverage exercises. Unset means all three, because the project's convention is
# that the default run is the full run; a shorter list narrows it ("1 3"), an empty value opts out entirely. The
# run.sh harness reads the same variable, so both scripts agree on which majors a run covers.
MELODY_E2E_MAJORS="${MELODY_E2E_MAJORS-1 2 3}"

e2e_example_directory() {
    local MAJOR_STRING="${1:?}"

    case "${MAJOR_STRING}" in
        1 | v1) printf '%s' "${EXAMPLE_DIRECTORY_V1_STRING}" ;;
        2 | v2) printf '%s' "${EXAMPLE_DIRECTORY_V2_STRING}" ;;
        3 | v3) printf '%s' "${EXAMPLE_DIRECTORY_V3_STRING}" ;;
        *) fail "unknown melody major: ${MAJOR_STRING} (expected 1, 2 or 3)" ;;
    esac
}

# stack.sh drives the v3 example throughout: every one of its checks either exercises a module that exists only in
# v3 (wiring, openapi, outbox, encrypt) or reaches its property through a command, a parameter or an app:info line
# that only the v3 example application declares. It names v3 explicitly here so the choice is visible; the
# coverage that DOES generalize across the majors lives in the run.sh harness, one section per major.
EXAMPLE_DIRECTORY_STRING="$(e2e_example_directory 3)"
EXAMPLE_ENV_LOCAL_PATH_STRING="${EXAMPLE_DIRECTORY_STRING}/.env.local"

# backend env defaults — the single source of truth for every e2e script. Each value is overridable from
# the environment, and the defaults address the compose services by their service name on the compose
# network, as seen from inside the dev container. The clear-to-skip contract (clear one to skip the
# sections it gates) applies to run.sh's in-process harness; stack.sh exercises the full compose stack
# and requires every backend up, so clearing a variable there does not skip its sections.
REDIS_ADDRESS="${REDIS_ADDRESS-redis:6379}"
POSTGRES_DSN="${POSTGRES_DSN-postgres://melody:melody@postgres:5432/melody_test?sslmode=disable}"
AMQP_DSN="${AMQP_DSN-amqp://guest:guest@rabbitmq:5672/}"
MINIO_ENDPOINT="${MINIO_ENDPOINT-localstack:4566}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY-test}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY-test}"
SMTP_ADDRESS="${SMTP_ADDRESS-mailpit:1025}"
MAILPIT_API_URL="${MAILPIT_API_URL-http://mailpit:8025}"
EXAMPLE_BASE_URL="${EXAMPLE_BASE_URL-http://127.0.0.1:8080}"
EXAMPLE_LOAD_BALANCER_URL="${EXAMPLE_LOAD_BALANCER_URL-http://load-balancer:80}"
# the harness's OWN connection to the database the example writes through: the mysql and two-factor sections
# re-read a column out of band instead of trusting the value the application reported, and delete the row they
# created. Clearing it keeps both sections running with their out-of-band halves announced as skipped.
# The credentials are the example's own (v3/.example/.env) and the address is the compose service name on the
# compose network, as seen from inside the dev container.
MYSQL_DSN="${MYSQL_DSN-melody:melody@tcp(mysql:3306)/melody_example}"
# the dev prometheus that scrapes the example's /metrics (.dev/docker/prometheus/prometheus.yml). The port is the
# IN-NETWORK 9090, not the 9091 the host mapping publishes: every e2e variable addresses the compose services from
# inside the dev container. Clearing it skips the scrape half of the metrics section.
PROMETHEUS_URL="${PROMETHEUS_URL-http://prometheus:9090}"

CHECK_FAILURE_COUNT_INTEGER=0
CHECK_EXECUTED_COUNT_INTEGER=0

# the failure count the section currently open began with; check_section_end compares against it to report the
# status that section's own checks earned. One variable is enough because the check sections are flat — a check
# script opens one section, asserts, closes it and opens the next.
CHECK_SECTION_FAILURE_BASELINE_INTEGER=0

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
        " MINIO_ENDPOINT='${MINIO_ENDPOINT}'" \
        " MINIO_ACCESS_KEY='${MINIO_ACCESS_KEY}'" \
        " MINIO_SECRET_KEY='${MINIO_SECRET_KEY}'" \
        " SMTP_ADDRESS='${SMTP_ADDRESS}'" \
        " MAILPIT_API_URL='${MAILPIT_API_URL}'" \
        " EXAMPLE_BASE_URL='${EXAMPLE_BASE_URL}'" \
        " EXAMPLE_LOAD_BALANCER_URL='${EXAMPLE_LOAD_BALANCER_URL}'" \
        " MYSQL_DSN='${MYSQL_DSN}'" \
        " PROMETHEUS_URL='${PROMETHEUS_URL}'" \
        " MELODY_E2E_MAJORS='${MELODY_E2E_MAJORS}'" \
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
    CHECK_EXECUTED_COUNT_INTEGER="$((CHECK_EXECUTED_COUNT_INTEGER + 1))"
}

check_fail() {
    printf 'FAIL  %s\n' "${1}" >&2
    CHECK_FAILURE_COUNT_INTEGER="$((CHECK_FAILURE_COUNT_INTEGER + 1))"
    CHECK_EXECUTED_COUNT_INTEGER="$((CHECK_EXECUTED_COUNT_INTEGER + 1))"
}

# opens a check section and snapshots the failure counter, so its end can report what the section actually did
check_section_start() {
    CHECK_SECTION_FAILURE_BASELINE_INTEGER="${CHECK_FAILURE_COUNT_INTEGER}"

    section_start "$@"
}

# closes a check section with the status its own checks earned: "failure" when the failure counter moved while the
# section ran, "success" otherwise. A check script must never pass the status itself — a hardcoded "success" is
# how a section that ran a check_fail still printed a green banner
check_section_end() {
    local TITLE_STRING="${1:?}"
    shift

    local STATUS_STRING="success"
    if [[ ${CHECK_FAILURE_COUNT_INTEGER} -gt ${CHECK_SECTION_FAILURE_BASELINE_INTEGER} ]]; then
        STATUS_STRING="failure"
    fi

    section_end "${TITLE_STRING}" "${STATUS_STRING}" "$@"
}

# prints the summary and exits non-zero when any check failed; the label names the check family ("stack"). The
# expected count is what makes a DELETED check block red: no individual assertion can notice its own absence, so
# the run states up front how many checks a complete pass executes and finishes by comparing it with the number
# that actually ran
finish_checks() {
    local LABEL_STRING="${1:?}"
    local EXPECTED_COUNT_INTEGER="${2:-}"

    if [[ "" != "${EXPECTED_COUNT_INTEGER}" && ${EXPECTED_COUNT_INTEGER} -ne ${CHECK_EXECUTED_COUNT_INTEGER} ]]; then
        check_fail "the run executed ${CHECK_EXECUTED_COUNT_INTEGER} ${LABEL_STRING} check(s), expected ${EXPECTED_COUNT_INTEGER} — a check block was skipped, removed or added (update the expected count in the same change)"
    fi

    if [[ 0 -lt ${CHECK_FAILURE_COUNT_INTEGER} ]]; then
        fail "${CHECK_FAILURE_COUNT_INTEGER} ${LABEL_STRING} check(s) failed"
    fi

    printf '\nALL %s CHECKS PASSED (%s of %s)\n' \
        "$(utility_to_upper "${LABEL_STRING}")" "${CHECK_EXECUTED_COUNT_INTEGER}" "${EXPECTED_COUNT_INTEGER:-${CHECK_EXECUTED_COUNT_INTEGER}}"
}
