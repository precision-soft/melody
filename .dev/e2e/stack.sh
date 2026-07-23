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
#   - CRON RUNNER        melody:cron:run boots from the same Configuration, evaluates the schedule and
#                        exits cleanly on --once
#   - COMMAND ROLE FLAG  a command's own --role after the command name reaches the command instead of
#                        being consumed as the runtime process role
#   - LAZY SERVICE       example:grant:demo resolves its user service through the container.Lazy handle at
#                        first run — the lazy-resolution marker and the grant line print from one invocation
#   - OUTBOX FACTORIES   /outbox/enqueue on the dev-supervised example writes through the lazily-resolved
#                        store, melody:outbox:relay publishes it from a separate process and /outbox/status
#                        shows the sent count grow
#   - ENCRYPT FACTORY    melody:encrypt:database resolves its database through the module factory at the
#                        first run and bulk-encrypts the two-factor columns
#   - CRON RUNNER FLAG   product:list reads its declared --limit default when the flag is not passed and
#                        the explicit value when it is
#   - SIGNAL SHUTDOWN    a built example serving http exits zero on a single SIGINT (the graceful path
#                        through NewSignalContext)
#   - WIRING GENERATE    melody:wiring:generate --strict runs inside the real application (the project
#                        directory the running configuration reports) and reproduces the committed
#                        generated/wiring_gen.go byte for byte
#   - PARAMETER SECRETS  debug:parameters redacts the marked credentials and the dsn assembled from one,
#                        while an ordinary parameter still prints in clear
#   - OPTIONAL ENV KEY   the default processor falls back when the key is unset, an .env.local override
#                        wins over the fallback, and the empty-string fallback resolves to ""
#
# Everything runs inside the dev container against the compose stack, through the helpers in common.sh.
# The example's .env.local is written and restored by the process-role check; it is git-ignored.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. "${SCRIPT_DIRECTORY_STRING}/common.sh"

e2e_require_dev_service

# ---------------------------------------------------------------------------------------------------
# EXCLUSIVE COMMAND — two instances, one run
# ---------------------------------------------------------------------------------------------------

section_start "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/exclusive-first.log /tmp/exclusive-second.log
    go run . example:exclusive:demo --hold 4s > /tmp/exclusive-first.log 2>&1 &
    FIRST_PID=\$!
    # wait for the holder to be demonstrably inside the command body: a fixed sleep races the go build cache, and a
    # contender that starts after the holder already released proves nothing about mutual exclusion. On a COLD build
    # cache the first 'go run .' compiles the whole framework, so the budget has to cover that; if the marker still
    # never appears we must say so distinctly and NOT launch the contender — otherwise a build delay is misreported
    # as a mutual-exclusion violation the lock never committed
    HOLDER_MARKER_SEEN=0
    for _ in \$(seq 1 900); do
        if grep -q 'exclusive tick: started' /tmp/exclusive-first.log 2>/dev/null; then
            HOLDER_MARKER_SEEN=1
            break
        fi
        sleep 0.2
    done
    if [ \"\${HOLDER_MARKER_SEEN}\" -ne 1 ]; then
        echo 'holder_marker_timeout=1'
        echo '--- first ---'
        cat /tmp/exclusive-first.log
        kill \${FIRST_PID} 2>/dev/null || true
        wait \${FIRST_PID} 2>/dev/null || true
        exit 0
    fi
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
    cat /tmp/exclusive-second.log"
EXCLUSIVE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${EXCLUSIVE_OUTPUT_STRING}"

# the holder never printed the start marker within the wait budget (a cold go build cache still compiling): the
# contender was NOT launched, so nothing about mutual exclusion was exercised. Fail with a distinct diagnostic
# instead of letting the empty output trip the exclusivity assertions below and blame the lock primitive
if printf '%s' "${EXCLUSIVE_OUTPUT_STRING}" | grep -q 'holder_marker_timeout=1'; then
    check_fail "the holder never printed the start marker within the wait budget (cold build cache still compiling?) — mutual exclusion was not exercised, this is not a lock failure"
    section_end "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "success" "${TAG_VALIDATE}" "e2e"
else

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

fi

# ---------------------------------------------------------------------------------------------------
# PROCESS ROLE — default, flag, .env, precedence, validation
# ---------------------------------------------------------------------------------------------------

section_start "PROCESS ROLE RESOLUTION" "${TAG_VALIDATE}" "e2e"

restore_example_env_local() {
    docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" rm -f "${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null || true
}

trap restore_example_env_local EXIT

# a prior run killed before its EXIT trap ran (SIGKILL, host/terminal death, or a docker failure the trap
# swallows with || true) can leave MELODY_PROCESS_ROLE=web behind in .env.local on the repo bind mount, which
# would poison the default-role check below. Clear it first — the same rm -f hygiene the other sections use
restore_example_env_local

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . app:info 2>/dev/null | grep '^process_role:'"
DEFAULT_ROLE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if [[ "${DEFAULT_ROLE_STRING}" = *"all"* ]]; then
    check_pass "the default process role is 'all' (${DEFAULT_ROLE_STRING})"
else
    check_fail "the default process role was ${DEFAULT_ROLE_STRING:-<empty>}, wanted 'all'"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . --role worker app:info 2>/dev/null | grep '^process_role:'"
FLAG_ROLE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if [[ "${FLAG_ROLE_STRING}" = *"worker"* ]]; then
    check_pass "--role worker selects the worker role (${FLAG_ROLE_STRING})"
else
    check_fail "--role worker produced ${FLAG_ROLE_STRING:-<empty>}, wanted 'worker'"
fi

# melody resolves config from .env files, never from the process environment, so the env value under test
# has to land in .env.local (which overrides .env and is git-ignored)
docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'MELODY_PROCESS_ROLE=web\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . app:info 2>/dev/null | grep '^process_role:'"
ENV_ROLE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if [[ "${ENV_ROLE_STRING}" = *"web"* ]]; then
    check_pass "MELODY_PROCESS_ROLE from .env selects the web role (${ENV_ROLE_STRING})"
else
    check_fail "MELODY_PROCESS_ROLE=web produced ${ENV_ROLE_STRING:-<empty>}, wanted 'web'"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . --role worker app:info 2>/dev/null | grep '^process_role:'"
PRECEDENCE_ROLE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if [[ "${PRECEDENCE_ROLE_STRING}" = *"worker"* ]]; then
    check_pass "an explicit --role beats MELODY_PROCESS_ROLE (${PRECEDENCE_ROLE_STRING})"
else
    check_fail "--role worker over MELODY_PROCESS_ROLE=web produced ${PRECEDENCE_ROLE_STRING:-<empty>}, wanted 'worker'"
fi

restore_example_env_local
trap - EXIT

# this check exists to prove the runtime FAILS CLOSED on an unsupported role, so BOTH halves are asserted:
# the process must exit non-zero (a fail-open that merely logged the diagnostic and booted with the widest
# role would satisfy the text alone), and the diagnostic must be the role validation's (any unrelated
# non-zero exit — a compile error, a missing .env — would satisfy the status alone). The exit status is
# carried out of the container explicitly: `go run` sits in a pipeline, whose status is the last command's
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . --role nonsense app:info >/tmp/role-reject.log 2>&1; echo \"role_exit_status=\$?\"; grep -c 'invalid role' /tmp/role-reject.log 2>/dev/null | sed 's/^/role_diagnostic_count=/' || true"
UNSUPPORTED_ROLE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
UNSUPPORTED_ROLE_STATUS_STRING="$(printf '%s' "${UNSUPPORTED_ROLE_OUTPUT_STRING}" | grep -o 'role_exit_status=[0-9]*' | head -1 | cut -d= -f2 || true)"
UNSUPPORTED_ROLE_COUNT_STRING="$(printf '%s' "${UNSUPPORTED_ROLE_OUTPUT_STRING}" | grep -o 'role_diagnostic_count=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${UNSUPPORTED_ROLE_STATUS_STRING:-0}" -ne 0 && "${UNSUPPORTED_ROLE_COUNT_STRING:-0}" -gt 0 ]]; then
    check_pass "an unsupported --role value fails closed: non-zero exit (${UNSUPPORTED_ROLE_STATUS_STRING}) carrying the invalid-role diagnostic"
elif [[ "${UNSUPPORTED_ROLE_STATUS_STRING:-0}" -eq 0 ]]; then
    check_fail "an unsupported --role value exited ZERO — the runtime failed open and booted with an unvalidated role (${UNSUPPORTED_ROLE_OUTPUT_STRING:-<empty>})"
else
    check_fail "an unsupported --role value exited non-zero but without the invalid-role diagnostic, so the rejection came from somewhere else (${UNSUPPORTED_ROLE_OUTPUT_STRING:-<empty>})"
fi

section_end "PROCESS ROLE RESOLUTION" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON — the user-less crontab template (busybox crond / per-user crontabs)
# ---------------------------------------------------------------------------------------------------

section_start "CRON CRONTAB-NO-USER TEMPLATE" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/crontab-with-user; go run . melody:cron:generate --out /tmp/crontab-with-user >/dev/null 2>&1; cat /tmp/crontab-with-user 2>/dev/null"
CRONTAB_WITH_USER_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/crontab-no-user; go run . melody:cron:generate --template crontab-no-user --out /tmp/crontab-no-user >/dev/null 2>&1; cat /tmp/crontab-no-user 2>/dev/null"
CRONTAB_NO_USER_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

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
# CRON IN-PROCESS RUNNER — the same Configuration drives melody:cron:run, which ticks in-process
# ---------------------------------------------------------------------------------------------------

section_start "CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# a clean exit alone would also pass with a runner that never parsed the Configuration; the example
# schedules product:list with a system user, which the in-process runner reports with a warning at Run
# (written to the example's MELODY_LOG_PATH file, var/log/dev.log), so that marker proves the runner
# resolved the entries from the one shared Configuration. The marker count is read before and after —
# the log file persists across runs, so an old marker would be a vacuous pass. Whether an entry actually
# fires depends on the wall minute, so firing itself is asserted by the unit tests
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "BEFORE_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); go run . melody:cron:run --once >/dev/null 2>&1; echo status=\$?; AFTER_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); echo \"user_warning_before=\${BEFORE_COUNT:-0}\"; echo \"user_warning_after=\${AFTER_COUNT:-0}\""
RUNNER_ONCE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${RUNNER_ONCE_STRING}" | grep -q 'status=0'; then
    check_pass "melody:cron:run --once evaluated the schedule in-process and exited cleanly"
else
    check_fail "melody:cron:run --once did not exit cleanly (${RUNNER_ONCE_STRING:-<empty>})"
fi

RUNNER_WARNING_BEFORE_INTEGER="$(printf '%s' "${RUNNER_ONCE_STRING}" | grep -o 'user_warning_before=[0-9]*' | head -1 | cut -d= -f2 || true)"
RUNNER_WARNING_AFTER_INTEGER="$(printf '%s' "${RUNNER_ONCE_STRING}" | grep -o 'user_warning_after=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${RUNNER_WARNING_AFTER_INTEGER:-0}" -gt "${RUNNER_WARNING_BEFORE_INTEGER:-0}" ]]; then
    check_pass "the runner resolved the shared Configuration (it reported the user-carrying product:list entry, ${RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${RUNNER_WARNING_AFTER_INTEGER:-0})"
else
    check_fail "the runner did not report the user-carrying entry (${RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${RUNNER_WARNING_AFTER_INTEGER:-0}), so nothing proves it parsed the Configuration"
fi

section_end "CRON IN-PROCESS RUNNER" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# COMMAND-OWNED ROLE FLAG — a command's own --role after the command name is not the runtime process role
# ---------------------------------------------------------------------------------------------------

section_start "COMMAND-OWNED ROLE FLAG" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:demo --role admin --user ada 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${GRANT_OUTPUT_STRING}" | grep -q 'granted role "admin" to user "ada"'; then
    check_pass "a command's own --role after the command name reaches the command (not the runtime role parser)"
else
    check_fail "the command-owned --role flag did not reach the command (${GRANT_OUTPUT_STRING:-<empty>})"
fi

section_end "COMMAND-OWNED ROLE FLAG" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# LAZY SERVICE RESOLUTION — the container.Lazy handle resolves the user service at first run, not at boot
# ---------------------------------------------------------------------------------------------------

section_start "LAZY SERVICE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# one invocation on purpose: the lazy-resolution marker and the grant line must come from the SAME run,
# proving the handle resolved inside the command body and the command still completed its work afterwards
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:demo --role admin --user ada 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
LAZY_GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${LAZY_GRANT_OUTPUT_STRING}" | grep -q 'user service resolved lazily: user "ada" known=false'; then
    check_pass "the user service resolved lazily inside the command body (unknown user reported)"
else
    check_fail "the lazy-resolution marker did not print (${LAZY_GRANT_OUTPUT_STRING:-<empty>})"
fi

if printf '%s' "${LAZY_GRANT_OUTPUT_STRING}" | grep -q 'granted role "admin" to user "ada"'; then
    check_pass "the same invocation still completed the grant after the lazy resolve"
else
    check_fail "the grant line did not print from the lazy-resolution invocation (${LAZY_GRANT_OUTPUT_STRING:-<empty>})"
fi

section_end "LAZY SERVICE RESOLUTION" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# OUTBOX FACTORIES END-TO-END — http enqueue on the supervised app, relay from a separate cli process
# ---------------------------------------------------------------------------------------------------

section_start "OUTBOX FACTORIES END-TO-END" "${TAG_VALIDATE}" "e2e"

# the http half runs against the example the dev container already supervises on EXAMPLE_BASE_URL (the
# same loopback address run.sh uses); wget is the http client the dev image ships (no curl). The relay
# half is a separate cli process, so store and relay factories are exercised across process boundaries.
# The sent count is read before and after: /outbox/status going green on rows sent by EARLIER runs would
# be a vacuous pass, so the assertion is that the count GREW, not that it exists
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "STATUS_BODY=\$(wget -q -O- \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>/dev/null || true)
    case \"\${STATUS_BODY}\" in
        *'\"success\":true'*) echo outbox_reachable=1 ;;
        *) echo outbox_reachable=0 ;;
    esac
    BEFORE_SENT=\$(printf '%s' \"\${STATUS_BODY}\" | grep -o '\"sent\":[0-9]*' | head -1 | cut -d: -f2)
    echo \"before_sent=\${BEFORE_SENT:-0}\"
    wget -q -O- --post-data='' \"\${EXAMPLE_BASE_URL}/outbox/enqueue?reference=stack-e2e\" 2>/dev/null || true
    echo ''
    # the outbox available_at has second precision, so the relay claims the row only once it is a full second old
    sleep 2
    go run . melody:outbox:relay --limit 1 >/tmp/outbox-relay.log 2>&1
    echo \"relay_status=\$?\"
    AFTER_BODY=\$(wget -q -O- \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>/dev/null || true)
    printf '%s\n' \"\${AFTER_BODY}\"
    AFTER_SENT=\$(printf '%s' \"\${AFTER_BODY}\" | grep -o '\"sent\":[0-9]*' | head -1 | cut -d: -f2)
    echo \"after_sent=\${AFTER_SENT:-0}\""
OUTBOX_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${OUTBOX_OUTPUT_STRING}"

# the supervised app is the other half of this check: when it is down (or serving a binary too old to have
# the outbox routes) nothing about the factories is exercised, so say that distinctly instead of letting
# the empty responses trip the assertions below. Reflex restarts the supervised app on any .go/.env change;
# `./dc restart dev` forces a resync when in doubt
if ! printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q 'outbox_reachable=1'; then
    check_fail "the dev-supervised example on EXAMPLE_BASE_URL is unreachable or lacks the outbox routes (stale supervised binary? reflex restarts it on .go/.env changes; ./dc restart dev forces a resync) — the outbox factories were not exercised"
    section_end "OUTBOX FACTORIES END-TO-END" "success" "${TAG_VALIDATE}" "e2e"
else

if printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q '"enqueued":true'; then
    check_pass "the http enqueue wrote through the lazily-resolved outbox store"
else
    check_fail "the http enqueue did not report \"enqueued\":true"
fi

if printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q 'relay_status=0'; then
    check_pass "melody:outbox:relay --limit 1 ran as a separate process and exited zero"
else
    check_fail "melody:outbox:relay --limit 1 did not exit zero"
fi

OUTBOX_BEFORE_SENT_INTEGER="$(printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -o 'before_sent=[0-9]*' | head -1 | cut -d= -f2 || true)"
OUTBOX_AFTER_SENT_INTEGER="$(printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -o 'after_sent=[0-9]*' | head -1 | cut -d= -f2 || true)"

if printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q '"sent":' \
    && [[ "${OUTBOX_AFTER_SENT_INTEGER:-0}" -gt "${OUTBOX_BEFORE_SENT_INTEGER:-0}" ]]; then
    check_pass "/outbox/status shows the sent count grew (${OUTBOX_BEFORE_SENT_INTEGER:-0} -> ${OUTBOX_AFTER_SENT_INTEGER:-0})"
else
    check_fail "the sent count did not grow (${OUTBOX_BEFORE_SENT_INTEGER:-0} -> ${OUTBOX_AFTER_SENT_INTEGER:-0}) — the relay published nothing"
fi

section_end "OUTBOX FACTORIES END-TO-END" "success" "${TAG_VALIDATE}" "e2e"

fi

# ---------------------------------------------------------------------------------------------------
# ENCRYPT FACTORY COMMAND — the bulk command resolves its database through the module factory at first run
# ---------------------------------------------------------------------------------------------------

section_start "ENCRYPT FACTORY COMMAND" "${TAG_VALIDATE}" "e2e"

# the completion log line is asserted alongside the exit code: a zero exit alone would also pass with a
# command that failed to wire the migration at all, while the "migration finished" marker (written to the
# example's MELODY_LOG_PATH file, var/log/dev.log) proves the bulk path ran end to end over the
# factory-resolved database. The marker count is read before and after — the log file persists across
# runs, so an old marker would be a vacuous pass. The processed row count in that line is legitimately
# zero when the table holds no plaintext rows, so it is not asserted
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "BEFORE_COUNT=\$(grep -c 'encrypt database migration finished' var/log/dev.log 2>/dev/null || true); go run . melody:encrypt:database --table melody_example_two_factor --primary-key user_identifier --column secret --column recovery_codes --mode encrypt >/tmp/encrypt-database.log 2>&1; echo status=\$?; AFTER_COUNT=\$(grep -c 'encrypt database migration finished' var/log/dev.log 2>/dev/null || true); echo \"migration_finished_before=\${BEFORE_COUNT:-0}\"; echo \"migration_finished_after=\${AFTER_COUNT:-0}\""
ENCRYPT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${ENCRYPT_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "melody:encrypt:database resolved its database through the factory and exited zero"
else
    check_fail "melody:encrypt:database did not exit zero (${ENCRYPT_OUTPUT_STRING:-<empty>})"
fi

ENCRYPT_MARKER_BEFORE_INTEGER="$(printf '%s' "${ENCRYPT_OUTPUT_STRING}" | grep -o 'migration_finished_before=[0-9]*' | head -1 | cut -d= -f2 || true)"
ENCRYPT_MARKER_AFTER_INTEGER="$(printf '%s' "${ENCRYPT_OUTPUT_STRING}" | grep -o 'migration_finished_after=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${ENCRYPT_MARKER_AFTER_INTEGER:-0}" -gt "${ENCRYPT_MARKER_BEFORE_INTEGER:-0}" ]]; then
    check_pass "the bulk migration ran to completion over the factory-resolved database (${ENCRYPT_MARKER_BEFORE_INTEGER:-0} -> ${ENCRYPT_MARKER_AFTER_INTEGER:-0})"
else
    check_fail "the migration-finished marker did not appear (${ENCRYPT_MARKER_BEFORE_INTEGER:-0} -> ${ENCRYPT_MARKER_AFTER_INTEGER:-0}), so nothing proves the bulk path ran"
fi

section_end "ENCRYPT FACTORY COMMAND" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON RUNNER FLAG DEFAULT — an unset flag reads back its declared default; an explicit value still wins
# ---------------------------------------------------------------------------------------------------

section_start "CRON RUNNER FLAG DEFAULT" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . product:list 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
PRODUCT_DEFAULT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${PRODUCT_DEFAULT_OUTPUT_STRING}" | grep -q 'product list: limit=5'; then
    check_pass "product:list without --limit reads back the declared default (limit=5)"
else
    check_fail "product:list without --limit did not honor the declared default of 5"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . product:list --limit 2 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
PRODUCT_EXPLICIT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${PRODUCT_EXPLICIT_OUTPUT_STRING}" | grep -q 'product list: limit=2'; then
    check_pass "product:list --limit 2 overrides the declared default (limit=2)"
else
    check_fail "product:list --limit 2 did not override the declared default"
fi

section_end "CRON RUNNER FLAG DEFAULT" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# GRACEFUL SIGNAL SHUTDOWN — one SIGINT while serving http exits zero through NewSignalContext
# ---------------------------------------------------------------------------------------------------

section_start "GRACEFUL SIGNAL SHUTDOWN" "${TAG_VALIDATE}" "e2e"

# melody derives the project directory from the executable location, so the binary is built into its own
# directory beside a copy of the example's .env; a .env.local there overrides the http address to a port
# the dev-supervised app does not hold (it owns 8080). Building outside the bind mount also keeps reflex
# from restarting the supervised app mid-check. Only the FIRST signal is exercised: the second-signal
# force-exit needs a hung shutdown, which the unit tests cover
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-signal-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-signal\" . >/tmp/example-signal-build.log 2>&1; then
        echo build_failed=1
        cat /tmp/example-signal-build.log
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    printf 'MELODY_HTTP_ADDRESS=:18080\n' > \"\${WORK_DIRECTORY}/.env.local\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    ./example-signal > /tmp/example-signal.log 2>&1 &
    APP_PID=\$!
    READY=0
    for _ in \$(seq 1 150); do
        if wget -q -O /dev/null http://127.0.0.1:18080/health 2>/dev/null; then
            READY=1
            break
        fi
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    echo \"ready=\${READY}\"
    if [ \"\${READY}\" -ne 1 ]; then
        echo '--- app log ---'
        tail -30 /tmp/example-signal.log
        kill \${APP_PID} 2>/dev/null || true
        wait \${APP_PID} 2>/dev/null || true
        rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-signal.log /tmp/example-signal-build.log
        exit 0
    fi
    if kill -0 \${APP_PID} 2>/dev/null; then
        echo alive_before_signal=1
    else
        echo alive_before_signal=0
    fi
    kill -INT \${APP_PID}
    FORCED=0
    for _ in \$(seq 1 150); do
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    if kill -0 \${APP_PID} 2>/dev/null; then
        FORCED=1
        kill -KILL \${APP_PID} 2>/dev/null || true
    fi
    wait \${APP_PID}
    APP_STATUS=\$?
    echo \"forced=\${FORCED}\"
    echo \"signal_exit_status=\${APP_STATUS}\"
    rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-signal.log /tmp/example-signal-build.log"
SIGNAL_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${SIGNAL_OUTPUT_STRING}"

# each precondition fails distinctly: a build error or an app that never served /health means the signal
# path was never exercised, and blaming the graceful shutdown for either would point at the wrong layer
if printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'build_failed=1'; then
    check_fail "the example did not build, so the signal path was not exercised"
elif ! printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'ready=1'; then
    check_fail "the built example never answered /health within the readiness budget, so the signal path was not exercised"
else
    # a zero exit proves nothing if the app had already left on its own before the SIGINT was sent, so the
    # liveness probe taken right before the kill is asserted first
    if printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'alive_before_signal=1'; then
        check_pass "the example was still serving when the SIGINT was sent (the exit below is attributable to the signal)"
    else
        check_fail "the example had already exited before the SIGINT was sent, so the graceful path was not exercised"
    fi

    if printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'forced=0'; then
        check_pass "the example left on its own after one SIGINT (no SIGKILL was needed)"
    else
        check_fail "the example was still running after the shutdown budget and had to be SIGKILLed"
    fi

    if printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'signal_exit_status=0'; then
        check_pass "one SIGINT while serving http exited zero (the graceful path through NewSignalContext)"
    else
        check_fail "the example did not exit zero on one SIGINT ($(printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -o 'signal_exit_status=[0-9]*' || echo 'no exit status captured'))"
    fi
fi

section_end "GRACEFUL SIGNAL SHUTDOWN" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# WIRING GENERATE — the generator runs in the real application and reproduces the committed file
# ---------------------------------------------------------------------------------------------------

section_start "WIRING GENERATE" "${TAG_VALIDATE}" "e2e"

# the unit test compares against the same project directory the application runs with, but only this
# invocation proves the command works from inside the app: a drift between the bind-set directories and
# the runtime project directory is exactly what the test alone once missed
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/wiring_gen_check.go
    go run . melody:wiring:generate --package generated --function RegisterGeneratedServices --strict --out /tmp/wiring_gen_check.go >/tmp/wiring-generate.log 2>&1
    echo \"wiring_exit_status=\$?\"
    if diff -q /tmp/wiring_gen_check.go generated/wiring_gen.go >/dev/null 2>&1; then
        echo 'wiring_identical=1'
    else
        echo 'wiring_identical=0'
        diff /tmp/wiring_gen_check.go generated/wiring_gen.go 2>&1 | head -20
    fi
    tail -3 /tmp/wiring-generate.log"
WIRING_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

WIRING_EXIT_STATUS_STRING="$(printf '%s' "${WIRING_OUTPUT_STRING}" | grep -o 'wiring_exit_status=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${WIRING_EXIT_STATUS_STRING:-1}" -eq 0 ]]; then
    check_pass "melody:wiring:generate --strict exits zero from inside the application"
else
    check_fail "melody:wiring:generate --strict exited ${WIRING_EXIT_STATUS_STRING:-<none>} (${WIRING_OUTPUT_STRING})"
fi

if printf '%s' "${WIRING_OUTPUT_STRING}" | grep -q 'wiring_identical=1'; then
    check_pass "the regenerated wiring is identical to the committed generated/wiring_gen.go"
else
    check_fail "the regenerated wiring drifted from the committed file (${WIRING_OUTPUT_STRING})"
fi

section_end "WIRING GENERATE" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# PARAMETER SECRETS — the marked credentials and the dsn assembled from one are redacted
# ---------------------------------------------------------------------------------------------------

section_start "PARAMETER SECRETS" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
# whitespace is stripped so each json object greps as one line whatever the printer's indentation
SECRETS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

MYSQL_PASSWORD_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"MYSQL_PASSWORD"[^}]*' | head -1 || true)"
if printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "MYSQL_PASSWORD is marked secret and its value is redacted"
else
    check_fail "MYSQL_PASSWORD is not redacted: ${MYSQL_PASSWORD_ENTRY_STRING:-<entry missing>}"
fi

if printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q 'melody'; then
    check_fail "the MYSQL_PASSWORD entry leaks the raw credential: ${MYSQL_PASSWORD_ENTRY_STRING}"
else
    check_pass "the MYSQL_PASSWORD entry carries no raw credential"
fi

S3_SECRET_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"S3_SECRET_KEY"[^}]*' | head -1 || true)"
if printf '%s' "${S3_SECRET_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${S3_SECRET_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "S3_SECRET_KEY is marked secret and its value is redacted"
else
    check_fail "S3_SECRET_KEY is not redacted: ${S3_SECRET_ENTRY_STRING:-<entry missing>}"
fi

# the dsn only reads the password, so its redaction is the propagation promise: the marking travelled
# through the template instead of covering the password alone
DSN_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"app.database.dsn"[^}]*' | head -1 || true)"
if printf '%s' "${DSN_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${DSN_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "the dsn assembled from the marked password is redacted along with it"
else
    check_fail "the assembled dsn is not redacted: ${DSN_ENTRY_STRING:-<entry missing>}"
fi

if printf '%s' "${DSN_ENTRY_STRING}" | grep -q 'tcp('; then
    check_fail "the dsn entry leaks the assembled value: ${DSN_ENTRY_STRING}"
else
    check_pass "the dsn entry carries no assembled value"
fi

TITLE_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"app.catalog_title"[^}]*' | head -1 || true)"
if printf '%s' "${TITLE_ENTRY_STRING}" | grep -q 'MelodyExampleCatalog' && printf '%s' "${TITLE_ENTRY_STRING}" | grep -q '"isSecret":false'; then
    check_pass "an ordinary parameter still prints in clear"
else
    check_fail "the ordinary parameter is not printed in clear: ${TITLE_ENTRY_STRING:-<entry missing>}"
fi

section_end "PARAMETER SECRETS" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# OPTIONAL ENV KEY — the default processor's fallback, an .env.local override, the empty fallback
# ---------------------------------------------------------------------------------------------------

section_start "OPTIONAL ENV KEY" "${TAG_VALIDATE}" "e2e"

trap restore_example_env_local EXIT
restore_example_env_local

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
OPTIONAL_DEFAULT_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

REFRESH_DEFAULT_ENTRY_STRING="$(printf '%s' "${OPTIONAL_DEFAULT_JSON_STRING}" | grep -o '"name":"app.reporting.refresh_interval"[^}]*' | head -1 || true)"
if printf '%s' "${REFRESH_DEFAULT_ENTRY_STRING}" | grep -q '"value":"5m"'; then
    check_pass "the unset APP_REPORTING_REFRESH_INTERVAL falls back to the default parameter (5m)"
else
    check_fail "the fallback did not resolve to 5m: ${REFRESH_DEFAULT_ENTRY_STRING:-<entry missing>}"
fi

EXPORT_ENDPOINT_ENTRY_STRING="$(printf '%s' "${OPTIONAL_DEFAULT_JSON_STRING}" | grep -o '"name":"app.reporting.export_endpoint"[^}]*' | head -1 || true)"
if printf '%s' "${EXPORT_ENDPOINT_ENTRY_STRING}" | grep -q '"value":""'; then
    check_pass "the empty-string fallback resolves to an empty value"
else
    check_fail "the empty fallback did not resolve to an empty value: ${EXPORT_ENDPOINT_ENTRY_STRING:-<entry missing>}"
fi

# melody resolves config from .env files, never the process environment, so the override lands in
# .env.local (git-ignored, restored by the trap)
docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'APP_REPORTING_REFRESH_INTERVAL=90s\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
OPTIONAL_OVERRIDE_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

REFRESH_OVERRIDE_ENTRY_STRING="$(printf '%s' "${OPTIONAL_OVERRIDE_JSON_STRING}" | grep -o '"name":"app.reporting.refresh_interval"[^}]*' | head -1 || true)"
if printf '%s' "${REFRESH_OVERRIDE_ENTRY_STRING}" | grep -q '"value":"90s"'; then
    check_pass "a defined APP_REPORTING_REFRESH_INTERVAL wins over the fallback (90s)"
else
    check_fail "the defined key did not win over the fallback: ${REFRESH_OVERRIDE_ENTRY_STRING:-<entry missing>}"
fi

restore_example_env_local
trap - EXIT

section_end "OPTIONAL ENV KEY" "success" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------

finish_checks "stack"
