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

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . --role nonsense app:info >/dev/null 2>&1"
if [[ 0 -eq ${RUN_IN_DEV_STATUS_INTEGER} ]]; then
    check_fail "an unsupported --role value was accepted"
else
    check_pass "an unsupported --role value is rejected"
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

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once >/dev/null 2>&1; echo status=\$?"
RUNNER_ONCE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${RUNNER_ONCE_STRING}" | grep -q 'status=0'; then
    check_pass "melody:cron:run --once evaluated the schedule in-process and exited cleanly"
else
    check_fail "melody:cron:run --once did not exit cleanly (${RUNNER_ONCE_STRING:-<empty>})"
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

OUTBOX_BEFORE_SENT_INTEGER="$(printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -o 'before_sent=[0-9]*' | head -1 | cut -d= -f2)"
OUTBOX_AFTER_SENT_INTEGER="$(printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -o 'after_sent=[0-9]*' | head -1 | cut -d= -f2)"

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

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:encrypt:database --table melody_example_two_factor --primary-key user_identifier --column secret --column recovery_codes --mode encrypt >/tmp/encrypt-database.log 2>&1; echo status=\$?"
ENCRYPT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${ENCRYPT_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "melody:encrypt:database resolved its database through the factory and exited zero"
else
    check_fail "melody:encrypt:database did not exit zero (${ENCRYPT_OUTPUT_STRING:-<empty>})"
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

finish_checks "stack"
