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
E2E_MYSQL_SERVICE_NAME_STRING="mysql"

# every major's database is provisioned whatever MELODY_E2E_MAJORS narrows the RUN to: the variable picks
# which majors are exercised, not which ones are allowed to exist, and a database missing because a previous
# run was narrowed would surface much later as an unrelated failure.
#
# An ARRAY rather than a space-separated string: the e2e scripts run under IFS=$'\n\t', so a string would
# not split on the spaces and the whole list would arrive as one element — and mysql accepts a backtick-quoted
# identifier containing spaces, so the run would report success having created one junk database and none of
# the three real ones
E2E_EXAMPLE_MAJOR_NUMBER_LIST=(1 2 3)

E2E_MYSQL_PROVISION_ATTEMPT_LIMIT_INTEGER=15

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

# stack.sh drives the v3 example through this default: those checks either exercise a module that exists only in
# v3 (wiring, openapi, outbox, encrypt) or reach their property through a command, a parameter or an app:info line
# that only the v3 example application declares. It names v3 explicitly here so the choice is visible. The
# sections whose banner begins with V1 address the v1 example through e2e_example_directory 1 instead of this
# variable, for wiring only the v1 example carries; the coverage that DOES generalize across the majors lives in
# the run.sh harness, one section per major.
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
# The database it names is v3's, because the sections that read through it drive the v3 example. The per-major
# sections do not inherit it: each major keeps its schema in a database of its own, and the Go harness swaps
# the database of this dsn for the major it is driving (exampleMysqlDsn in .dev/e2e/example.go).
MYSQL_DSN="${MYSQL_DSN-melody:melody@tcp(mysql:3306)/melody_example_v3}"
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

    e2e_ensure_example_databases
}

# each .example application holds its schema in a database of its own, all three in the one mysql container:
# the bunorm/migrate command family builds its migrator on bun's default bookkeeping tables and bun matches
# an applied migration by NAME, so three majors sharing one database would share one bun_migrations and the
# first set to land would answer for the others.
#
# .dev/docker/mysql/init.sql creates them, but only on a FRESH volume — every development volume provisioned
# before that file existed would otherwise refuse the examples with "unknown database". These statements are
# the door for those: idempotent, run as root because the melody user cannot create a database, and cheap
# enough to pay on every harness invocation.
#
# Clearing MYSQL_DSN opts out, the same way it opts out of the out-of-band reads: a run told there is no
# database to reach is not asked to provision one.
e2e_ensure_example_databases() {
    if [[ "" = "${MYSQL_DSN}" ]]; then
        info "MYSQL_DSN is cleared, so the per-major example databases were not provisioned"

        return 0
    fi

    # the mysql service sits behind the 'all' compose profile, so the profile-blind helpers do not see it:
    # docker_compose_service_exists reads `config --services` and ensure_service_running reads `ps -q`, and
    # both answer empty for a profiled service. The profile is named here rather than the service looked up.
    if [[ "" = "$(docker_compose_no_log --profile all ps -q "${E2E_MYSQL_SERVICE_NAME_STRING}" 2>/dev/null || true)" ]]; then
        info "service ${E2E_MYSQL_SERVICE_NAME_STRING} is not running [starting]"
        docker_compose --profile all up -d "${E2E_MYSQL_SERVICE_NAME_STRING}"
    fi

    local STATEMENT_STRING=""
    local MAJOR_STRING
    for MAJOR_STRING in "${E2E_EXAMPLE_MAJOR_NUMBER_LIST[@]}"; do
        STATEMENT_STRING="${STATEMENT_STRING}CREATE DATABASE IF NOT EXISTS \`melody_example_v${MAJOR_STRING}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci; "
    done
    STATEMENT_STRING="${STATEMENT_STRING}GRANT ALL PRIVILEGES ON \`melody\\_example\\_%\`.* TO 'melody'@'%'; FLUSH PRIVILEGES;"

    # the mysql client is asked for a machine answer and the whole thing is retried: the service can report
    # healthy through mysqladmin while the server is still finishing its own initialisation, and a create
    # that lands then fails on a connection refused rather than on anything about the statement
    local ATTEMPT_INTEGER=0
    while true; do
        if docker_compose_no_log --profile all exec -T "${E2E_MYSQL_SERVICE_NAME_STRING}" \
            mysql -uroot -proot --batch --skip-column-names -e "${STATEMENT_STRING}" </dev/null >/dev/null 2>&1; then
            return 0
        fi

        ATTEMPT_INTEGER=$((ATTEMPT_INTEGER + 1))
        if [[ "${ATTEMPT_INTEGER}" -ge "${E2E_MYSQL_PROVISION_ATTEMPT_LIMIT_INTEGER}" ]]; then
            fail "could not create the per-major example databases on the ${E2E_MYSQL_SERVICE_NAME_STRING} service after ${ATTEMPT_INTEGER} attempts"
        fi

        sleep 2
    done
}

# e2e_mysql_scalar <database> <statement> prints the single value a statement answers, read straight from the
# mysql service rather than through an application. It is how a check states what the database holds when the
# application cannot say so — a rollback that dropped live tables leaves nothing to ask a command about, and a
# log line saying it happened is the application's word for it rather than the state itself. An unreachable
# server prints nothing, which every caller reads as a mismatch rather than as agreement.
e2e_mysql_scalar() {
    local DATABASE_STRING="${1:?}"
    local STATEMENT_STRING="${2:?}"

    docker_compose_no_log --profile all exec -T "${E2E_MYSQL_SERVICE_NAME_STRING}" \
        mysql -uroot -proot --batch --skip-column-names -D "${DATABASE_STRING}" -e "${STATEMENT_STRING}" \
        </dev/null 2>/dev/null | tr -d '\r\n' || true
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
