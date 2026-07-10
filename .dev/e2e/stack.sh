#!/usr/bin/env bash

# Stack-level e2e checks: the behaviours that only appear when SEPARATE PROCESSES run against the same
# backends, so the in-process harness (.dev/e2e, run with run.sh) cannot reach them.
#
#   ./dc up:all          # bring the backends up first
#   .dev/e2e/stack.sh    # every check
#
# Checks:
#   - EXCLUSIVE COMMAND  two concurrent instances of example:exclusive:demo run the body exactly once;
#                        the loser exits zero so a cron fleet stays green
#   - PROCESS ROLE       the default role, --role, the .env value, --role winning over it, and the panic
#                        an unsupported role must raise
#   - CRON NO-USER       melody:cron:generate --template crontab-no-user emits entries with no user column
#
# Everything runs inside the dev container against the compose stack. The example's .env.local is written
# and restored by the process-role check; it is git-ignored.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "${SCRIPT_DIRECTORY_STRING}/../.." && pwd)"

. "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/utility.sh"

SERVICE_NAME_STRING="dev"
EXAMPLE_DIRECTORY_STRING="/app/v3/.example"
EXAMPLE_ENV_LOCAL_PATH_STRING="${EXAMPLE_DIRECTORY_STRING}/.env.local"

# the cd is a statement of its own: joining it to the command with && would make a trailing & background the
# whole chain, leaving the foreground shell outside the example directory
GO_PREFIX_STRING="export PATH=/usr/local/go/bin:\$PATH; cd ${EXAMPLE_DIRECTORY_STRING} || exit 1;"

CHECK_FAILURE_COUNT_INTEGER=0

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

check_pass() {
    printf 'PASS  %s\n' "${1}"
}

check_fail() {
    printf 'FAIL  %s\n' "${1}" >&2
    CHECK_FAILURE_COUNT_INTEGER="$((CHECK_FAILURE_COUNT_INTEGER + 1))"
}

# run_in_service_shell echoes the docker command it runs, which would end up inside every command
# substitution below; capture through docker_compose_no_log so only the container's own output comes back
in_example() {
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" bash -c "${GO_PREFIX_STRING} ${1}" </dev/null
}

# ---------------------------------------------------------------------------------------------------
# EXCLUSIVE COMMAND — two instances, one run
# ---------------------------------------------------------------------------------------------------

section_start "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "${TAG_VALIDATE}" "e2e"

EXCLUSIVE_OUTPUT_STRING="$(
    in_example "rm -f /tmp/exclusive-first.log /tmp/exclusive-second.log
        go run . example:exclusive:demo --hold 4s > /tmp/exclusive-first.log 2>&1 &
        FIRST_PID=\$!
        # wait for the holder to be demonstrably inside the command body: a fixed sleep races the go build cache, and a
        # contender that starts after the holder already released proves nothing about mutual exclusion
        for _ in \$(seq 1 150); do
            grep -q 'exclusive tick: started' /tmp/exclusive-first.log 2>/dev/null && break
            sleep 0.2
        done
        go run . example:exclusive:demo --hold 1s > /tmp/exclusive-second.log 2>&1
        SECOND_STATUS=\$?
        wait \${FIRST_PID}
        FIRST_STATUS=\$?
        echo \"first_status=\${FIRST_STATUS}\"
        echo \"second_status=\${SECOND_STATUS}\"
        echo \"first_started=\$(grep -c 'exclusive tick: started' /tmp/exclusive-first.log 2>/dev/null || true)\"
        echo \"second_started=\$(grep -c 'exclusive tick: started' /tmp/exclusive-second.log 2>/dev/null || true)\"
        echo '--- first ---'
        cat /tmp/exclusive-first.log
        echo '--- second ---'
        cat /tmp/exclusive-second.log" || true
)"

printf '%s\n' "${EXCLUSIVE_OUTPUT_STRING}"

STARTED_COUNT_INTEGER="$(printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -c 'exclusive tick: started' || true)"

if [[ "1" = "${STARTED_COUNT_INTEGER}" ]]; then
    check_pass "exactly one of two concurrent instances ran the command body"
else
    check_fail "expected exactly one instance to run the body, ${STARTED_COUNT_INTEGER} did"
fi

if printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -q 'second_status=0'; then
    check_pass "the instance that lost the lock exited zero (a cron fleet stays green)"
else
    check_fail "the instance that lost the lock did not exit zero"
fi

# without these the section goes green when the HOLDER never ran: the contender alone would account for the single
# "started" line, and mutual exclusion would never have been exercised at all
if printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -q 'first_status=0'; then
    check_pass "the instance that held the lock exited zero"
else
    check_fail "the instance that held the lock did not exit zero"
fi

if printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -q 'first_started=1'; then
    check_pass "the holder is the instance that ran the body"
else
    check_fail "the holder never entered the command body, so exclusivity was never exercised"
fi

if printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -q 'second_started=0'; then
    check_pass "the contender was turned away while the holder was inside the body"
else
    check_fail "the contender entered the command body while the holder held the lock"
fi

section_end "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# PROCESS ROLE — default, flag, .env, precedence, validation
# ---------------------------------------------------------------------------------------------------

section_start "PROCESS ROLE RESOLUTION" "${TAG_VALIDATE}" "e2e"

restore_example_env_local() {
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" rm -f "${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null || true
}

trap restore_example_env_local EXIT

DEFAULT_ROLE_STRING="$(in_example "go run . app:info 2>/dev/null | grep '^process_role:'" || true)"
if [[ "${DEFAULT_ROLE_STRING}" = *"all"* ]]; then
    check_pass "the default process role is 'all' (${DEFAULT_ROLE_STRING})"
else
    check_fail "the default process role was ${DEFAULT_ROLE_STRING:-<empty>}, wanted 'all'"
fi

FLAG_ROLE_STRING="$(in_example "go run . --role worker app:info 2>/dev/null | grep '^process_role:'" || true)"
if [[ "${FLAG_ROLE_STRING}" = *"worker"* ]]; then
    check_pass "--role worker selects the worker role (${FLAG_ROLE_STRING})"
else
    check_fail "--role worker produced ${FLAG_ROLE_STRING:-<empty>}, wanted 'worker'"
fi

# melody resolves config from .env files, never from the process environment, so the env value under test
# has to land in .env.local (which overrides .env and is git-ignored)
docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" \
    bash -c "printf 'MELODY_PROCESS_ROLE=web\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

ENV_ROLE_STRING="$(in_example "go run . app:info 2>/dev/null | grep '^process_role:'" || true)"
if [[ "${ENV_ROLE_STRING}" = *"web"* ]]; then
    check_pass "MELODY_PROCESS_ROLE from .env selects the web role (${ENV_ROLE_STRING})"
else
    check_fail "MELODY_PROCESS_ROLE=web produced ${ENV_ROLE_STRING:-<empty>}, wanted 'web'"
fi

PRECEDENCE_ROLE_STRING="$(in_example "go run . --role worker app:info 2>/dev/null | grep '^process_role:'" || true)"
if [[ "${PRECEDENCE_ROLE_STRING}" = *"worker"* ]]; then
    check_pass "an explicit --role beats MELODY_PROCESS_ROLE (${PRECEDENCE_ROLE_STRING})"
else
    check_fail "--role worker over MELODY_PROCESS_ROLE=web produced ${PRECEDENCE_ROLE_STRING:-<empty>}, wanted 'worker'"
fi

restore_example_env_local
trap - EXIT

if in_example "go run . --role nonsense app:info" >/dev/null 2>&1; then
    check_fail "an unsupported --role value was accepted"
else
    check_pass "an unsupported --role value is rejected"
fi

section_end "PROCESS ROLE RESOLUTION" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON — the user-less crontab template (busybox crond / per-user crontabs)
# ---------------------------------------------------------------------------------------------------

section_start "CRON CRONTAB-NO-USER TEMPLATE" "${TAG_VALIDATE}" "e2e"

CRONTAB_WITH_USER_STRING="$(
    in_example "rm -f /tmp/crontab-with-user; go run . melody:cron:generate --out /tmp/crontab-with-user >/dev/null 2>&1; cat /tmp/crontab-with-user 2>/dev/null" || true
)"
CRONTAB_NO_USER_STRING="$(
    in_example "rm -f /tmp/crontab-no-user; go run . melody:cron:generate --template crontab-no-user --out /tmp/crontab-no-user >/dev/null 2>&1; cat /tmp/crontab-no-user 2>/dev/null" || true
)"

if [[ "" = "${CRONTAB_NO_USER_STRING}" ]]; then
    check_fail "the crontab-no-user template produced no output"
else
    printf '%s\n' "${CRONTAB_NO_USER_STRING}"

    # an entry line starts with five schedule fields; in the user-column mode the sixth field is the user,
    # in the no-user mode it is already the command (an absolute path or an env assignment)
    WITH_USER_SIXTH_FIELD_STRING="$(printf '%s' "${CRONTAB_WITH_USER_STRING}" | awk '!/^#/ && NF >= 6 {print $6; exit}')"
    NO_USER_SIXTH_FIELD_STRING="$(printf '%s' "${CRONTAB_NO_USER_STRING}" | awk '!/^#/ && NF >= 6 {print $6; exit}')"

    if [[ "" != "${NO_USER_SIXTH_FIELD_STRING}" && "/" = "${NO_USER_SIXTH_FIELD_STRING:0:1}" ]]; then
        check_pass "crontab-no-user entries put the command where the user column would be (${NO_USER_SIXTH_FIELD_STRING})"
    else
        check_fail "crontab-no-user entries do not start the command at the sixth field (got '${NO_USER_SIXTH_FIELD_STRING}')"
    fi

    if [[ "" != "${WITH_USER_SIXTH_FIELD_STRING}" && "/" != "${WITH_USER_SIXTH_FIELD_STRING:0:1}" ]]; then
        check_pass "the default crontab template still emits the user column (${WITH_USER_SIXTH_FIELD_STRING})"
    else
        check_fail "the default crontab template lost its user column (got '${WITH_USER_SIXTH_FIELD_STRING}')"
    fi
fi

section_end "CRON CRONTAB-NO-USER TEMPLATE" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------

if [[ 0 -lt ${CHECK_FAILURE_COUNT_INTEGER} ]]; then
    fail "${CHECK_FAILURE_COUNT_INTEGER} stack check(s) failed"
fi

printf '\nALL STACK CHECKS PASSED\n'
