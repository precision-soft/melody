#!/usr/bin/env bash

# Stack-level e2e checks: the behaviours that only appear when SEPARATE PROCESSES run against the same
# backends, so the in-process harness (.dev/e2e, run with run.sh) cannot reach them.
#
#   ./dc up:all          # bring the backends up first
#   .dev/e2e/stack.sh    # every check
#
# Checks:
#   - EXCLUSIVE COMMAND  two concurrent instances of example:exclusive:tick run the body exactly once;
#                        the loser exits zero so a cron fleet stays green
#   - PROCESS ROLE       the default role, --role, the .env value, --role winning over it, and the panic
#                        an unsupported role must raise
#   - CRON NO-USER       melody:cron:generate --template crontab-no-user emits entries with no user column
#   - CRON RUNNER        melody:cron:run boots from the same Configuration, evaluates the schedule and
#                        exits cleanly on --once
#   - COMMAND ROLE FLAG  a command's own --role after the command name reaches the command instead of
#                        being consumed as the runtime process role
#   - LAZY SERVICE       example:grant:role resolves its user service through the container.Lazy handle at
#                        first run — the lazy-resolution marker and the grant line print from one invocation
#   - OUTBOX FACTORIES   /outbox/enqueue on the dev-supervised example writes through the lazily-resolved
#                        store, melody:outbox:relay publishes it from a separate process and /outbox/status
#                        shows the sent count grow
#   - MESSAGE BUS        POST /messagebus/dispatch on the dev-supervised example hands the message to the async
#                        transport instead of handling it inline, and melody:messagebus:consume handles it
#                        from a separate process — exactly one message, on a queue drained first
#   - ENCRYPT FACTORY    melody:encrypt:database resolves its database through the module factory at the
#                        first run and bulk-encrypts the two-factor columns
#   - CRON RUNNER FLAG   product:list reads its declared --limit default when the flag is not passed and
#                        the explicit value when it is
#   - SIGNAL SHUTDOWN    a built example serving http exits zero on a single SIGINT (the graceful path
#                        through NewSignalContext)
#   - WIRING GENERATE    melody:wiring:generate --strict runs inside the real application (the project
#                        directory the running configuration reports) and reproduces the committed
#                        generated/wiring_gen.go byte for byte
#   - OPENAPI GENERATE   melody:openapi:generate builds a document from the application's real routes and
#                        request types, carrying their operations and component schemas
#   - PARAMETER SECRETS  debug:parameters redacts the marked credentials and the dsn assembled from one,
#                        while an ordinary parameter still prints in clear
#   - OPTIONAL ENV KEY   the default processor falls back when the key is unset, an .env.local override
#                        wins over the fallback, and the empty-string fallback resolves to ""
#   - V3 MIGRATIONS      the db:* family over the v3 example's own six-migration set — the catalogue, the
#                        journal and the two-factor enrollment table — with the machine document asserted
#                        from a live application and the rollback read straight out of mysql
#   - V1 CRON RUNNER     the v1 example registers the cron module: melody:cron:run boots from the shared
#                        Configuration, reports its user-carrying entries and answers the json envelope
#   - V1 MIGRATIONS      the bunorm/migrate command family runs the same migration set the v1 providers
#                        apply at first resolution: init, status, a rollback/migrate round trip over the
#                        live tables, and the resolutions that reseed what the round trip emptied
#   - V1 DEBUG           the dev-registered debug commands answer from the v1 example, debug:parameters
#                        redacting the marked APP_API_TOKEN
#   - V1 ENVELOPE        the v1 example commands render through the cli/output envelope: one json document
#                        naming the command, the standard --limit, and the framework table
#   - V2 CRON RUNNER     the same for the v2 example, which registers the cron module since the tier-one lot
#   - V2 MIGRATIONS      the db:* family over the v2 example's own five-migration set, journal included:
#                        this major keeps the journal beside the catalogue instead of in a second database
#   - V2 DEBUG           the dev-registered debug commands answer from the v2 example, one check short of
#                        its v1 sibling: the scoped registration is wiring this example does not carry yet
#   - V2 ENVELOPE        the v2 example commands render through the cli/output envelope, as v1's do
#
# Everything runs inside the dev container against the compose stack, through the helpers in common.sh.
# The example's .env.local is written and restored by the process-role check; it is git-ignored.
#
# The checks up to V3 MIGRATIONS drive the v3 example, and say so in the banner they print at the start; the
# sections whose banner begins with V1 or V2 drive /app/.example and /app/v2/.example through
# e2e_example_directory. The v3 pin is not an
# oversight to be generalized later. Some of those checks exercise a module that exists only in v3: wiring
# generate, openapi generate, the outbox relay, the encrypt bulk command. The rest reach a framework primitive
# that v1 and v2 do carry, but through something only the v3 EXAMPLE APPLICATION declares — example:exclusive:tick
# and example:grant:role, the command-owned --role flag, the process_role line its app:info prints, the
# product:list --limit flag, the cron configuration the templates render, and the parameter the optional-env-key
# check reads. Generalizing those would mean changing the v1 and v2 example applications, not this script. The
# V1 and V2 sections exist for the mirror-image reason: the cron module registration and the cli/output
# rendering of the example's own commands are wiring the two published majors carry over their own
# migration sets, and each major's set is its own — the tables, the identifiers and the count differ, so
# one section per major is what states them.
#
# The coverage that DOES generalize across the three majors — boot, a public route, the login/session flow,
# path-traversal containment, a 404, the command line and a single-SIGINT shutdown — lives in the run.sh harness,
# one section per major.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. "${SCRIPT_DIRECTORY_STRING}/common.sh"

e2e_require_dev_service

# the number of checks a COMPLETE run executes, asserted by finish_checks at the end. No assertion can notice its
# own absence, so deleting or commenting out a check block would otherwise still end in ALL STACK CHECKS PASSED,
# with the run simply doing less. Adding or removing a check means changing this number in the same edit — the
# mismatch message prints both numbers, so the count to move to is in the failure itself. A run that took one of
# the degraded early-exit branches (an unreachable supervised app, a cold-cache timeout) legitimately executes
# fewer checks; it is already red from the check_fail that branch raised
EXPECTED_CHECK_COUNT_INTEGER=107
readonly EXPECTED_CHECK_COUNT_INTEGER

# state the scope in the output, so a reader never has to infer which major these checks covered
info "stack checks drive the v3 example application: ${EXAMPLE_DIRECTORY_STRING}"
info "v3-only module: wiring generate, openapi generate, outbox relay, encrypt bulk"
info "v3-only through the example app: exclusive/grant demo commands, command-owned --role, app:info process_role, product:list --limit, cron configuration, optional-env-key parameter"
info "the v3 example registers the bunorm/migrate family over its own six-migration set (V3 DATABASE MIGRATIONS)"
info "the V1 sections drive the v1 example application: $(e2e_example_directory 1) (cron module, bunorm/migrate family, cli/output envelope, dev debug commands)"
info "the V2 sections drive the v2 example application: $(e2e_example_directory 2) (the same four, over a five-migration set that carries the journal)"
info "per-major coverage (boot, login/session, traversal, 404, cli, SIGINT) runs in .dev/e2e/run.sh for majors: ${MELODY_E2E_MAJORS:-<none>}"

# ---------------------------------------------------------------------------------------------------
# EXCLUSIVE COMMAND — two instances, one run
# ---------------------------------------------------------------------------------------------------

check_section_start "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/exclusive-first.log /tmp/exclusive-second.log
    go run . example:exclusive:tick --hold 4s > /tmp/exclusive-first.log 2>&1 &
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
    go run . example:exclusive:tick --hold 1s > /tmp/exclusive-second.log 2>&1
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
    check_section_end "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "${TAG_VALIDATE}" "e2e"
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

check_section_end "EXCLUSIVE COMMAND ACROSS TWO INSTANCES" "${TAG_VALIDATE}" "e2e"

fi

# ---------------------------------------------------------------------------------------------------
# PROCESS ROLE — default, flag, .env, precedence, validation
# ---------------------------------------------------------------------------------------------------

check_section_start "PROCESS ROLE RESOLUTION" "${TAG_VALIDATE}" "e2e"

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

check_section_end "PROCESS ROLE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON — the user-less crontab template (busybox crond / per-user crontabs)
# ---------------------------------------------------------------------------------------------------

check_section_start "CRON CRONTAB-NO-USER TEMPLATE" "${TAG_VALIDATE}" "e2e"

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

    # the ownership marker is what --prune reads to prove a file is the generator's own; asserted on the
    # file the run just wrote, not on the command's word
    if printf '%s' "${CRONTAB_WITH_USER_STRING}" | grep -qF '# owned by melody:cron:generate'; then
        check_pass "the generated crontab carries the ownership marker"
    else
        check_fail "the generated crontab does not carry the ownership marker"
    fi

    # EntryConfig.Arguments reach the generated line: the example schedules product:list with --limit=2
    if printf '%s' "${CRONTAB_WITH_USER_STRING}" | grep -q 'product:list --limit=2'; then
        check_pass "EntryConfig.Arguments reach the generated crontab line (product:list --limit=2)"
    else
        check_fail "EntryConfig.Arguments did not reach the generated crontab line"
    fi
fi

# --prune reconciles dir(--out): a stale file the marker proves ours is emptied down to its header, and
# the operator's own file beside it is untouched — both read back from the directory afterwards, which is
# what proves the sweep, not the command's report of it
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -rf /tmp/cron-prune-band && mkdir -p /tmp/cron-prune-band; go run . melody:cron:generate --out /tmp/cron-prune-band/stale.crontab >/dev/null 2>&1; printf '# written by the operator\n*/5 * * * * root /usr/local/bin/backup\n' > /tmp/cron-prune-band/operator.crontab; go run . melody:cron:generate --out /tmp/cron-prune-band/crontab --prune >/dev/null 2>&1; cat /tmp/cron-prune-band/stale.crontab 2>/dev/null"
PRUNED_STALE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${PRUNED_STALE_STRING}" | grep -qF '# owned by melody:cron:generate' && ! printf '%s' "${PRUNED_STALE_STRING}" | grep -q 'product:list'; then
    check_pass "--prune emptied the stale destination down to its marker-carrying header"
else
    check_fail "--prune left the stale destination running or destroyed its marker"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "cat /tmp/cron-prune-band/operator.crontab 2>/dev/null"
OPERATOR_FILE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${OPERATOR_FILE_STRING}" | grep -q '/usr/local/bin/backup'; then
    check_pass "--prune left the operator's unowned file untouched"
else
    check_fail "--prune touched a file it cannot prove it wrote"
fi

# the k8s manifests open with the same marker as a leading YAML comment, which is what makes a k8s output
# directory reconcilable by the same sweep
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/cron-band-k8s.yaml; go run . melody:cron:generate --template k8s --image registry.example/app:1 --out /tmp/cron-band-k8s.yaml >/dev/null 2>&1; cat /tmp/cron-band-k8s.yaml 2>/dev/null"
K8S_MANIFEST_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${K8S_MANIFEST_STRING}" | grep -qF '# owned by melody:cron:generate' && printf '%s' "${K8S_MANIFEST_STRING}" | grep -q 'apiVersion: batch/v1'; then
    check_pass "the k8s manifests carry the ownership marker beside their CronJob documents"
else
    check_fail "the k8s manifests do not carry the ownership marker (or rendered no CronJob)"
fi

check_section_end "CRON CRONTAB-NO-USER TEMPLATE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON IN-PROCESS RUNNER — the same Configuration drives melody:cron:run, which ticks in-process
# ---------------------------------------------------------------------------------------------------

check_section_start "CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

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

# the entry count in the envelope is deterministic whatever the wall minute: configured counts entries,
# not dispatches, and the v3 example schedules exactly three commands — the same shape the v1 and v2
# sections assert, which the v3 runner answered with an empty stream before it rendered the envelope
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once --format=json 2>/dev/null"
RUNNER_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${RUNNER_JSON_STRING}" | grep -q '"configured":3'; then
    check_pass "the v3 runner's json envelope counts the three configured entries"
else
    check_fail "the v3 runner's json envelope does not count the three configured entries: ${RUNNER_JSON_STRING:-<empty>}"
fi

check_section_end "CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# COMMAND-OWNED ROLE FLAG — a command's own --role after the command name is not the runtime process role
# ---------------------------------------------------------------------------------------------------

check_section_start "COMMAND-OWNED ROLE FLAG" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role admin --user ada 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${GRANT_OUTPUT_STRING}" | grep -q 'granted role "admin" to user "ada"'; then
    check_pass "a command's own --role after the command name reaches the command (not the runtime role parser)"
else
    check_fail "the command-owned --role flag did not reach the command (${GRANT_OUTPUT_STRING:-<empty>})"
fi

check_section_end "COMMAND-OWNED ROLE FLAG" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# LAZY SERVICE RESOLUTION — the container.Lazy handle resolves the user service at first run, not at boot
# ---------------------------------------------------------------------------------------------------

check_section_start "LAZY SERVICE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# one invocation on purpose: the lazy-resolution marker and the grant line must come from the SAME run,
# proving the handle resolved inside the command body and the command still completed its work afterwards
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role admin --user ada 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
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

check_section_end "LAZY SERVICE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# OUTBOX FACTORIES END-TO-END — http enqueue on the supervised app, relay from a separate cli process
# ---------------------------------------------------------------------------------------------------

check_section_start "OUTBOX FACTORIES END-TO-END" "${TAG_VALIDATE}" "e2e"

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
    check_section_end "OUTBOX FACTORIES END-TO-END" "${TAG_VALIDATE}" "e2e"
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

check_section_end "OUTBOX FACTORIES END-TO-END" "${TAG_VALIDATE}" "e2e"

fi

# ---------------------------------------------------------------------------------------------------
# MESSAGE BUS ASYNC TRANSPORT — http dispatch hands the message to the broker, a separate consumer handles it
# ---------------------------------------------------------------------------------------------------

check_section_start "MESSAGE BUS ASYNC TRANSPORT" "${TAG_VALIDATE}" "e2e"

# The property is that a dispatch does NOT handle its message inline: the example routes WelcomeEmail to the
# "async" transport (config/messagebus.go), so POST /messagebus/dispatch on the supervised app must hand the message
# to rabbitmq and return, and the handler must run only when melody:messagebus:consume pulls it from a SEPARATE
# process. That is why this check lives here and not in the run.sh harness: the consumer is a second process with
# its own lifecycle, started, bounded, killed and read for its exit status while the supervised app keeps serving.
#
# The handler's own marker ("welcome email sent", written by messagehandler.HandleWelcomeEmail to the example's
# MELODY_LOG_PATH file, var/log/dev.log) is counted at three points, and the MIDDLE one carries the check: after
# the http dispatch it must NOT have grown. A marker that grew there means the message was handled inline and the
# async transport was never involved — and the http response is a 202 either way, so nothing else reveals it.
#
# The queue is DRAINED first, and that is load-bearing rather than hygiene: a message left in rabbitmq by an
# earlier run (or by an interrupted consume) would be picked up by this run's consumer and stand in for this run's
# message, so the +1 assertion would pass without the dispatch having contributed anything.
#
# Neither consumer is bounded with `timeout`, even though the dev image ships it: `timeout go run .` signals
# `go run`, which dies and ORPHANS the compiled binary it started — the consumer then keeps running and keeps
# eating messages out of the queue for the rest of the session (measured: four orphans stealing this check's own
# messages). Each consumer is therefore started as a background job in its OWN process group (`set -m`) and the
# whole group is signalled, which reaches the compiled child. This is the background-PID-and-kill pattern the
# exclusive-command check uses, with the process group added because of the `go run` indirection.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "STATUS_BODY=\$(wget -q -O- \"\${EXAMPLE_BASE_URL}/health\" 2>/dev/null || true)
    case \"\${STATUS_BODY}\" in
        *'\"success\":true'*) echo messagebus_reachable=1 ;;
        *) echo messagebus_reachable=0; exit 0 ;;
    esac
    # drain: consume with no limit until the queue is empty, then stop the consumer by signalling its process group
    set -m
    go run . melody:messagebus:consume --transport async --limit 0 >/tmp/messagebus-drain.log 2>&1 &
    DRAIN_PID=\$!
    set +m
    DRAIN_STARTED=0
    for _ in \$(seq 1 900); do
        if grep -q 'messagebus:consume\\] \\[started' /tmp/messagebus-drain.log 2>/dev/null; then
            DRAIN_STARTED=1
            break
        fi
        sleep 0.2
    done
    if [ \"\${DRAIN_STARTED}\" -ne 1 ]; then
        echo 'drain_start_timeout=1'
        cat /tmp/messagebus-drain.log
        kill -TERM -- -\${DRAIN_PID} 2>/dev/null || true
        wait \${DRAIN_PID} 2>/dev/null || true
        exit 0
    fi
    sleep 3
    kill -TERM -- -\${DRAIN_PID} 2>/dev/null || true
    wait \${DRAIN_PID} 2>/dev/null || true
    BEFORE_HANDLED=\$(grep -c 'welcome email sent' var/log/dev.log 2>/dev/null || true)
    echo \"before_handled=\${BEFORE_HANDLED:-0}\"
    wget -q -O- --post-data='' \"\${EXAMPLE_BASE_URL}/messagebus/dispatch\" 2>/dev/null || true
    echo ''
    sleep 2
    INLINE_HANDLED=\$(grep -c 'welcome email sent' var/log/dev.log 2>/dev/null || true)
    echo \"inline_handled=\${INLINE_HANDLED:-0}\"
    set -m
    go run . melody:messagebus:consume --transport async --limit 1 >/tmp/messagebus-consume.log 2>&1 &
    CONSUME_PID=\$!
    set +m
    CONSUME_EXITED=0
    for _ in \$(seq 1 900); do
        if ! kill -0 \${CONSUME_PID} 2>/dev/null; then
            CONSUME_EXITED=1
            break
        fi
        sleep 0.2
    done
    if [ \"\${CONSUME_EXITED}\" -ne 1 ]; then
        echo 'consume_timeout=1'
        kill -TERM -- -\${CONSUME_PID} 2>/dev/null || true
        wait \${CONSUME_PID} 2>/dev/null || true
        cat /tmp/messagebus-consume.log
        exit 0
    fi
    wait \${CONSUME_PID}
    echo \"consume_status=\$?\"
    AFTER_HANDLED=\$(grep -c 'welcome email sent' var/log/dev.log 2>/dev/null || true)
    echo \"after_handled=\${AFTER_HANDLED:-0}\""
MESSAGE_BUS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${MESSAGE_BUS_OUTPUT_STRING}"

# the supervised app is one half of this check; when it is down nothing about the transport was exercised, so say
# that distinctly instead of letting the empty responses trip the assertions below
if ! printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'messagebus_reachable=1'; then
    check_fail "the dev-supervised example on EXAMPLE_BASE_URL is unreachable (reflex restarts it on .go/.env changes; ./dc restart dev forces a resync) — nothing was exercised: no message was dispatched and no consumer ran, this is not a message bus failure"
    check_section_end "MESSAGE BUS ASYNC TRANSPORT" "${TAG_VALIDATE}" "e2e"
elif printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'drain_start_timeout=1'; then
    check_fail "the draining consumer never printed its start marker within the wait budget (cold build cache still compiling?) — the queue was NOT drained, so nothing below would have been this run's own message; this is not a message bus failure"
    check_section_end "MESSAGE BUS ASYNC TRANSPORT" "${TAG_VALIDATE}" "e2e"
elif printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'consume_timeout=1'; then
    check_fail "melody:messagebus:consume --limit 1 never exited within the wait budget — it received no message to consume, so the async delivery could not be observed"
    check_section_end "MESSAGE BUS ASYNC TRANSPORT" "${TAG_VALIDATE}" "e2e"
else

if printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q '"status":"dispatched"'; then
    check_pass "POST /messagebus/dispatch dispatched the message on the supervised application"
else
    check_fail "POST /messagebus/dispatch did not report \"status\":\"dispatched\""
fi

MESSAGE_BUS_BEFORE_INTEGER="$(printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -o 'before_handled=[0-9]*' | head -1 | cut -d= -f2 || true)"
MESSAGE_BUS_INLINE_INTEGER="$(printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -o 'inline_handled=[0-9]*' | head -1 | cut -d= -f2 || true)"
MESSAGE_BUS_AFTER_INTEGER="$(printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -o 'after_handled=[0-9]*' | head -1 | cut -d= -f2 || true)"

# the assertion that carries the section: an inline handler would already have logged the marker here
if [[ "${MESSAGE_BUS_INLINE_INTEGER:-0}" -eq "${MESSAGE_BUS_BEFORE_INTEGER:-0}" ]]; then
    check_pass "the handler had NOT run after the http dispatch (${MESSAGE_BUS_BEFORE_INTEGER:-0} -> ${MESSAGE_BUS_INLINE_INTEGER:-0}) — the message went to the async transport instead of being handled inline"
else
    check_fail "the handler ran during the http dispatch (${MESSAGE_BUS_BEFORE_INTEGER:-0} -> ${MESSAGE_BUS_INLINE_INTEGER:-0}) — the message was handled inline and never reached the async transport"
fi

if printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'consume_status=0'; then
    check_pass "melody:messagebus:consume --transport async --limit 1 ran as a separate process and exited zero"
else
    check_fail "melody:messagebus:consume --transport async --limit 1 did not exit zero"
fi

# exactly one, not "grew": more than one would mean the drain left a message behind and this run's assertion was
# standing on somebody else's message
if [[ "$((${MESSAGE_BUS_AFTER_INTEGER:-0} - ${MESSAGE_BUS_INLINE_INTEGER:-0}))" -eq 1 ]]; then
    check_pass "the separate consumer handled exactly one message (${MESSAGE_BUS_INLINE_INTEGER:-0} -> ${MESSAGE_BUS_AFTER_INTEGER:-0})"
else
    check_fail "the marker moved by $((${MESSAGE_BUS_AFTER_INTEGER:-0} - ${MESSAGE_BUS_INLINE_INTEGER:-0})) after the consume (${MESSAGE_BUS_INLINE_INTEGER:-0} -> ${MESSAGE_BUS_AFTER_INTEGER:-0}), wanted exactly 1"
fi

check_section_end "MESSAGE BUS ASYNC TRANSPORT" "${TAG_VALIDATE}" "e2e"

fi

# ---------------------------------------------------------------------------------------------------
# ENCRYPT FACTORY COMMAND — the bulk command resolves its database through the module factory at first run
# ---------------------------------------------------------------------------------------------------

check_section_start "ENCRYPT FACTORY COMMAND" "${TAG_VALIDATE}" "e2e"

# the completion log line is asserted alongside the exit code: a zero exit alone would also pass with a
# command that failed to wire the migration at all, while the "migration finished" marker (written to the
# example's MELODY_LOG_PATH file, var/log/dev.log) proves the bulk path ran end to end over the
# factory-resolved database. The marker count is read before and after — the log file persists across
# runs, so an old marker would be a vacuous pass. The processed row count in that line is legitimately
# zero when the table holds no plaintext rows, so it is not asserted
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "BEFORE_COUNT=\$(grep -c 'encrypt database migration finished' var/log/dev.log 2>/dev/null || true); go run . melody:encrypt:database --table melody_example_v3_two_factor --primary-key user_identifier --column secret --column recovery_codes --mode encrypt >/tmp/encrypt-database.log 2>&1; echo status=\$?; AFTER_COUNT=\$(grep -c 'encrypt database migration finished' var/log/dev.log 2>/dev/null || true); echo \"migration_finished_before=\${BEFORE_COUNT:-0}\"; echo \"migration_finished_after=\${AFTER_COUNT:-0}\""
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

check_section_end "ENCRYPT FACTORY COMMAND" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# CRON RUNNER FLAG DEFAULT — an unset flag reads back its declared default; an explicit value still wins
# ---------------------------------------------------------------------------------------------------

check_section_start "CRON RUNNER FLAG DEFAULT" "${TAG_VALIDATE}" "e2e"

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

check_section_end "CRON RUNNER FLAG DEFAULT" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# GRACEFUL SIGNAL SHUTDOWN — one SIGINT while serving http exits zero through NewSignalContext
# ---------------------------------------------------------------------------------------------------

check_section_start "GRACEFUL SIGNAL SHUTDOWN" "${TAG_VALIDATE}" "e2e"

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

check_section_end "GRACEFUL SIGNAL SHUTDOWN" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# WIRING GENERATE — the generator runs in the real application and reproduces the committed file
# ---------------------------------------------------------------------------------------------------

check_section_start "WIRING GENERATE" "${TAG_VALIDATE}" "e2e"

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

check_section_end "WIRING GENERATE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# OPENAPI GENERATE — the generator runs in the real application and emits a well-formed document
# ---------------------------------------------------------------------------------------------------

check_section_start "OPENAPI GENERATE" "${TAG_VALIDATE}" "e2e"

# the unit tests exercise the schema mirror on synthetic types; only this invocation proves the command
# builds a document from the application's real routes and request types
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/openapi_check.json
    go run . melody:openapi:generate --out /tmp/openapi_check.json >/tmp/openapi-generate.log 2>&1
    echo \"openapi_exit_status=\$?\"
    echo \"openapi_operation_count=\$(grep -c '\"operationId\"' /tmp/openapi_check.json 2>/dev/null || echo 0)\"
    echo \"openapi_schema_marker=\$(grep -c '\"schemas\"' /tmp/openapi_check.json 2>/dev/null || echo 0)\"
    tail -3 /tmp/openapi-generate.log"
OPENAPI_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

OPENAPI_EXIT_STATUS_STRING="$(printf '%s' "${OPENAPI_OUTPUT_STRING}" | grep -o 'openapi_exit_status=[0-9]*' | head -1 | cut -d= -f2 || true)"
OPENAPI_OPERATION_COUNT_STRING="$(printf '%s' "${OPENAPI_OUTPUT_STRING}" | grep -o 'openapi_operation_count=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${OPENAPI_EXIT_STATUS_STRING:-1}" -eq 0 ]]; then
    check_pass "melody:openapi:generate exits zero from inside the application"
else
    check_fail "melody:openapi:generate exited ${OPENAPI_EXIT_STATUS_STRING:-<none>} (${OPENAPI_OUTPUT_STRING})"
fi

if [[ "${OPENAPI_OPERATION_COUNT_STRING:-0}" -gt 0 ]] && printf '%s' "${OPENAPI_OUTPUT_STRING}" | grep -q 'openapi_schema_marker=[1-9]'; then
    check_pass "the generated document carries the application's operations and component schemas"
else
    check_fail "the generated document is missing operations or schemas (${OPENAPI_OUTPUT_STRING})"
fi

check_section_end "OPENAPI GENERATE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# PARAMETER SECRETS — the marked credentials and the dsn assembled from one are redacted
# ---------------------------------------------------------------------------------------------------

check_section_start "PARAMETER SECRETS" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
# whitespace is stripped so each json object greps as one line whatever the printer's indentation
SECRETS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

MYSQL_PASSWORD_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"MYSQL_PASSWORD"[^}]*' | head -1 || true)"
if printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "MYSQL_PASSWORD is marked secret and its value is redacted"
else
    check_fail "MYSQL_PASSWORD is not redacted: ${MYSQL_PASSWORD_ENTRY_STRING:-<entry missing>}"
fi

# a negative over an ABSENT entry proves nothing: a renamed parameter, a crashed debug:parameters or a dead
# docker exec all leave an empty string, which carries no credential and would pass. The entry has to exist first
if [[ "" = "${MYSQL_PASSWORD_ENTRY_STRING}" ]]; then
    check_fail "the MYSQL_PASSWORD entry is missing from debug:parameters, so nothing was inspected for a raw credential"
elif printf '%s' "${MYSQL_PASSWORD_ENTRY_STRING}" | grep -q 'melody'; then
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

# same shape as the password negative: an empty entry carries no assembled value either, so it is asserted present
if [[ "" = "${DSN_ENTRY_STRING}" ]]; then
    check_fail "the app.database.dsn entry is missing from debug:parameters, so nothing was inspected for the assembled value"
elif printf '%s' "${DSN_ENTRY_STRING}" | grep -q 'tcp('; then
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

check_section_end "PARAMETER SECRETS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# OPTIONAL ENV KEY — the default processor's fallback, an .env.local override, the empty fallback
# ---------------------------------------------------------------------------------------------------

check_section_start "OPTIONAL ENV KEY" "${TAG_VALIDATE}" "e2e"

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

check_section_end "OPTIONAL ENV KEY" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 DATABASE MIGRATIONS — the db:* family and the composition root run one set over six tables
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops all six tables of the v3 example's own
# melody_example_v3 database — the four catalogue ones, the journal, and the two-factor enrollment table
# neither frozen major carries. Every later step exists to put the state back — the next boot restores the
# schema, and the two resolutions after it reseed the catalogue and the user directory — so the section must
# run to its end whatever the intermediate verdicts, which check_fail already guarantees. The journal is
# append-only with no seeds and nobody is enrolled by default, so an empty journal and an empty enrollment
# table ARE their restored state. The framework's own tables are untouched: the outbox store and the audit
# registry open their schema through the module that owns it, and the set claims neither.
#
# This major differs from the two frozen ones in WHEN the set is applied. Its composition root is eager —
# the connection is dialled at boot and the two-factor build step applies the set there — where v1 and v2
# apply it lazily at the first repository resolution. So every command of this application boots over an
# up-to-date schema, and what the checks below read is the database itself rather than a command's word for
# it: the count of the example's own tables before and after each step.
#
# The six are named one by one rather than matched on a prefix: the audit registry's own table is called
# melody_example_v3_audit and would be counted by any LIKE that catches the six, while it belongs to the
# framework module that opens it and correctly survives a rollback of this set. Its survival is the check's
# quiet half — a set that had claimed it would take it down with the rest.
V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING="SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'melody_example_v3' AND table_name IN ('melody_example_v3_category', 'melody_example_v3_currency', 'melody_example_v3_product', 'melody_example_v3_user', 'melody_example_v3_catalog_journal', 'melody_example_v3_two_factor')"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:init >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v3 db:init is idempotent over the existing bookkeeping tables"
else
    check_fail "v3 db:init failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v3 db:migrate answers success over the set the composition root already applied"
else
    check_fail "v3 db:migrate failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_STATUS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V3_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V3_STATUS_OUTPUT_STRING}" | grep -q '20260819000006'; then
    check_pass "v3 db:status reports the applied set by name with nothing pending"
else
    check_fail "v3 db:status does not report the applied set (${V3_STATUS_OUTPUT_STRING:-<empty>})"
fi

# the machine document is the half of the command family a person never reads: one closed json object on one
# line, keyed on stable names rather than on the headings the table renders, with the server's own identity
# beside the set. Only an application wired to a live database can answer it, which is why it is asserted
# here rather than in the package's own tests.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:status --format=json 2>/dev/null | tail -1"
V3_STATUS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V3_STATUS_JSON_STRING}" | grep -q '"migrations":{"applied":\["20260819000006"' \
    && printf '%s' "${V3_STATUS_JSON_STRING}" | grep -q '"database":"melody_example_v3"' \
    && printf '%s' "${V3_STATUS_JSON_STRING}" | grep -q '"error":null'; then
    check_pass "v3 db:status --format=json renders one document naming the set, the database and no error"
else
    check_fail "the v3 db:status machine document is not the expected envelope (${V3_STATUS_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:unlock >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v3 db:unlock clears the lock table over a database nobody is migrating"
else
    check_fail "v3 db:unlock failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v3 db:rollback reverted the last group (the six live tables are dropped until the next boot)"
else
    check_fail "v3 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# read out of band, between the rollback and the next boot: this is the one window in which the drop is
# observable, because the very next application process applies the set again on its way up
V3_TABLE_COUNT_AFTER_ROLLBACK_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING}")"
if [[ "0" = "${V3_TABLE_COUNT_AFTER_ROLLBACK_STRING}" ]]; then
    check_pass "the six v3 example tables are gone from mysql after the rollback (read out of band; the audit table, which the set does not own, stands)"
else
    check_fail "the rollback left ${V3_TABLE_COUNT_AFTER_ROLLBACK_STRING:-<no answer>} v3 example tables standing"
fi

# a fresh process resolves the product repository, which runs the same migration set programmatically and
# then reseeds the empty catalogue — the first-request tolerance the migration switch had to preserve. The
# boot has already recreated every table by this point; what this step adds is the seed, which belongs to
# the repository rather than to the set
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . product:list 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'prod-1'; then
    check_pass "a fresh v3 resolution migrated and reseeded the emptied catalogue (prod-1 is back)"
else
    check_fail "the fresh v3 resolution did not restore the catalogue (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

V3_TABLE_COUNT_AFTER_RESTORE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING}")"
if [[ "6" = "${V3_TABLE_COUNT_AFTER_RESTORE_STRING}" ]]; then
    check_pass "the operator's set and the application's are one: all six tables are back (read out of band)"
else
    check_fail "the restored schema holds ${V3_TABLE_COUNT_AFTER_RESTORE_STRING:-<no answer>} of the six v3 example tables"
fi

# the user table has no command of its own; resolving the user repository by name through debug:container
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "resolving the v3 user repository reseeded the user directory"
else
    check_fail "the v3 user repository resolution failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V3 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# The V1 sections: the wiring only the v1 example carries today. They address the v1 example explicitly
# instead of reassigning EXAMPLE_DIRECTORY_STRING, so every invocation states which major it drives.
# ---------------------------------------------------------------------------------------------------

V1_EXAMPLE_DIRECTORY_STRING="$(e2e_example_directory 1)"

# ---------------------------------------------------------------------------------------------------
# V1 CRON IN-PROCESS RUNNER — the module-registered runner boots from the shared Configuration
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# the same proof shape as the v3 section above: a clean exit alone would also pass with a runner that
# never parsed the Configuration, and the v1 schedule carries a user on two of its three entries, which
# the runner reports with a warning at every Run (written to var/log/dev.log). Whether an entry fires
# depends on the wall minute, so firing itself is not asserted here
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "BEFORE_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); go run . melody:cron:run --once >/dev/null 2>&1; echo status=\$?; AFTER_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); echo \"user_warning_before=\${BEFORE_COUNT:-0}\"; echo \"user_warning_after=\${AFTER_COUNT:-0}\""
V1_RUNNER_ONCE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V1_RUNNER_ONCE_STRING}" | grep -q 'status=0'; then
    check_pass "v1 melody:cron:run --once evaluated the schedule in-process and exited cleanly"
else
    check_fail "v1 melody:cron:run --once did not exit cleanly (${V1_RUNNER_ONCE_STRING:-<empty>})"
fi

V1_RUNNER_WARNING_BEFORE_INTEGER="$(printf '%s' "${V1_RUNNER_ONCE_STRING}" | grep -o 'user_warning_before=[0-9]*' | head -1 | cut -d= -f2 || true)"
V1_RUNNER_WARNING_AFTER_INTEGER="$(printf '%s' "${V1_RUNNER_ONCE_STRING}" | grep -o 'user_warning_after=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${V1_RUNNER_WARNING_AFTER_INTEGER:-0}" -gt "${V1_RUNNER_WARNING_BEFORE_INTEGER:-0}" ]]; then
    check_pass "the v1 runner resolved the shared Configuration (it reported the user-carrying entries, ${V1_RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${V1_RUNNER_WARNING_AFTER_INTEGER:-0})"
else
    check_fail "the v1 runner did not report a user-carrying entry (${V1_RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${V1_RUNNER_WARNING_AFTER_INTEGER:-0}), so nothing proves it parsed the Configuration"
fi

# the entry count in the envelope is deterministic whatever the wall minute: configured counts entries,
# not dispatches, and the v1 example schedules exactly three commands
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once --format=json 2>/dev/null"
V1_RUNNER_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V1_RUNNER_JSON_STRING}" | grep -q '"configured":3'; then
    check_pass "the v1 runner's json envelope counts the three configured entries"
else
    check_fail "the v1 runner's json envelope does not count the three configured entries: ${V1_RUNNER_JSON_STRING:-<empty>}"
fi

check_section_end "V1 CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V1 DATABASE MIGRATIONS — the db:* family runs the same set the providers apply at first resolution
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops the four v1 catalog tables of the v1
# example's own melody_example_v1 database — the journal lives in its own postgres database and has a section
# of its own below. Every later step exists to put the state back — the second migrate restores the schema,
# and the two resolutions after it reseed the catalogue and the user directory — so the section must run to
# its end whatever the intermediate verdicts, which check_fail already guarantees. The dev-supervised v1
# application keeps working through it: its resolved repositories hold only the *bun.DB handle
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:init >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v1 db:init is idempotent over the existing bookkeeping tables"
else
    check_fail "v1 db:init failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v1 db:migrate answers success over the set the providers already applied"
else
    check_fail "v1 db:migrate failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V1_STATUS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V1_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V1_STATUS_OUTPUT_STRING}" | grep -q '20260814000003'; then
    check_pass "v1 db:status reports the applied set by name with nothing pending"
else
    check_fail "v1 db:status does not report the applied set (${V1_STATUS_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v1 db:rollback reverted the last group (the four live catalog tables are dropped until the next step)"
else
    check_fail "v1 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'applied 4 migrations'; then
    check_pass "v1 db:migrate re-applied the four catalog migrations the rollback reverted"
else
    check_fail "v1 db:migrate did not re-apply the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# a fresh process resolves the product provider, which runs the same migration set programmatically and
# then reseeds the empty catalogue — the first-request tolerance the migration switch had to preserve,
# and the step that restores three of the four seeded tables
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . product:list 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'prod-1'; then
    check_pass "a fresh v1 resolution migrated and reseeded the emptied catalogue (prod-1 is back)"
else
    check_fail "the fresh v1 resolution did not restore the catalogue (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the user table has no command of its own; resolving the user repository by name through debug:container
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "resolving the v1 user repository reseeded the user directory"
else
    check_fail "the v1 user repository resolution failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V1 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V1 JOURNAL DATABASE MIGRATIONS — the db:journal:* context family runs the journal set on postgres
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 JOURNAL DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# the journal is the v1 example's second live database: one process, the catalogue on mysql and the journal
# on postgres, each with its own migration set and its own command family. The rollback here drops the live
# journal table of the shared melody_test database; the migrate after it puts the schema back, and the
# journal is append-only with no seeds, so an empty journal IS the restored state.
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:init >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v1 db:journal:init is idempotent over the journal database's bookkeeping tables"
else
    check_fail "v1 db:journal:init failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:migrate >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v1 db:journal:migrate answers success over the set the journal provider already applied"
else
    check_fail "v1 db:journal:migrate failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V1_JOURNAL_STATUS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V1_JOURNAL_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V1_JOURNAL_STATUS_OUTPUT_STRING}" | grep -q '20260814000005'; then
    check_pass "v1 db:journal:status reports the applied journal set by name with nothing pending"
else
    check_fail "v1 db:journal:status does not report the applied journal set (${V1_JOURNAL_STATUS_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v1 db:journal:rollback reverted the journal group (the live journal table is dropped until the next step)"
else
    check_fail "v1 db:journal:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:migrate 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'applied 1 migration'; then
    check_pass "v1 db:journal:migrate re-applied the journal migration the rollback reverted"
else
    check_fail "v1 db:journal:migrate did not re-apply the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the restored table must actually answer: catalog:journal reads it through the same lazy handle a live
# write dials, so a success here proves the postgres connection and the recreated schema end to end
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . catalog:journal --format=json 2>/dev/null"
V1_JOURNAL_RESTORED_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
if printf '%s' "${V1_JOURNAL_RESTORED_JSON_STRING}" | grep -q '"command":"catalog:journal"'; then
    check_pass "v1 catalog:journal reads the restored journal table over postgres"
else
    check_fail "v1 catalog:journal did not answer over the restored journal table (${V1_JOURNAL_RESTORED_JSON_STRING:-<empty>})"
fi

check_section_end "V1 JOURNAL DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V1 DEBUG COMMANDS — the dev-registered family answers from the v1 example
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
V1_SECRETS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

V1_API_TOKEN_ENTRY_STRING="$(printf '%s' "${V1_SECRETS_JSON_STRING}" | grep -o '"name":"APP_API_TOKEN"[^}]*' | head -1 || true)"
if printf '%s' "${V1_API_TOKEN_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${V1_API_TOKEN_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "APP_API_TOKEN is marked secret and its value is redacted"
else
    check_fail "APP_API_TOKEN is not redacted: ${V1_API_TOKEN_ENTRY_STRING:-<entry missing>}"
fi

# a negative over an ABSENT entry proves nothing: the entry has to exist before its content is inspected
if [[ "" = "${V1_API_TOKEN_ENTRY_STRING}" ]]; then
    check_fail "the APP_API_TOKEN entry is missing from debug:parameters, so nothing was inspected for a raw credential"
elif printf '%s' "${V1_API_TOKEN_ENTRY_STRING}" | grep -q 'example-api-token'; then
    check_fail "the APP_API_TOKEN entry leaks the raw credential: ${V1_API_TOKEN_ENTRY_STRING}"
else
    check_pass "the APP_API_TOKEN entry carries no raw credential"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:version 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'MELODY: v'; then
    check_pass "v1 debug:version reports the framework version"
else
    check_fail "v1 debug:version did not report the framework version (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:events --format=json 2>/dev/null"
V1_EVENTS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
if printf '%s' "${V1_EVENTS_JSON_STRING}" | grep -q '"command":"debug:events"'; then
    check_pass "v1 debug:events answers its json envelope"
else
    check_fail "v1 debug:events did not answer its envelope (${V1_EVENTS_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:middleware 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'static'; then
    check_pass "v1 debug:middleware lists the static middleware of the example's pipeline"
else
    check_fail "v1 debug:middleware did not list the pipeline (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --limit=0 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V1_CONTAINER_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V1_CONTAINER_OUTPUT_STRING}" | grep -q 'service.example.product.repository'; then
    check_pass "v1 debug:container lists the example's registered services"
else
    check_fail "v1 debug:container did not list the example services (${V1_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

# the scoped registration is the console-visible half of the request-scoped attribution: debug:container
# renders scoped definitions in a block of their own, so the name appearing here proves the example's
# RegisterScopedServices hook ran and the definition reached the container
if printf '%s' "${V1_CONTAINER_OUTPUT_STRING}" | grep -q 'app.example.change.attribution'; then
    check_pass "v1 debug:container lists the request-scoped change attribution"
else
    check_fail "v1 debug:container does not list the scoped change attribution (${V1_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V1 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V1 COMMAND OUTPUT ENVELOPE — the example's own commands render through cli/output
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 COMMAND OUTPUT ENVELOPE" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . product:list --format=json 2>/dev/null"
V1_PRODUCT_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V1_PRODUCT_JSON_STRING}" | grep -q '"command":"product:list"'; then
    check_pass "v1 product:list --format=json answers one envelope naming the command"
else
    check_fail "v1 product:list --format=json did not answer its envelope (${V1_PRODUCT_JSON_STRING:-<empty>})"
fi

if printf '%s' "${V1_PRODUCT_JSON_STRING}" | grep -q '"prod-1"'; then
    check_pass "the v1 product envelope carries the seeded catalogue"
else
    check_fail "the v1 product envelope does not carry the seeded catalogue (${V1_PRODUCT_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . catalog:journal --limit=1 --format=json 2>/dev/null"
V1_JOURNAL_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V1_JOURNAL_JSON_STRING}" | grep -q '"limit":1'; then
    check_pass "v1 catalog:journal honours the standard --limit and echoes it in the payload"
else
    check_fail "v1 catalog:journal did not echo the standard limit (${V1_JOURNAL_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . catalog:report:refresh 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'RECORDED_AT'; then
    check_pass "v1 catalog:report:refresh renders the reading through the framework table"
else
    check_fail "v1 catalog:report:refresh did not render the framework table (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V1 COMMAND OUTPUT ENVELOPE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# The V2 sections: the same four wirings, driven against the v2 example, which carries them since the
# tier-one lot. The fifth V1 section has no counterpart here — the journal migrations are a second
# database on postgres, and this major keeps its journal in the one mysql database beside the catalogue.
# ---------------------------------------------------------------------------------------------------

V2_EXAMPLE_DIRECTORY_STRING="$(e2e_example_directory 2)"

# ---------------------------------------------------------------------------------------------------
# V2 CRON IN-PROCESS RUNNER — the module-registered runner boots from the shared Configuration
# ---------------------------------------------------------------------------------------------------

check_section_start "V2 CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# the same proof shape as the V1 section above: a clean exit alone would also pass with a runner that
# never parsed the Configuration, and the v2 schedule carries a user on two of its three entries, which
# the runner reports with a warning at every Run (written to var/log/dev.log). Whether an entry fires
# depends on the wall minute, so firing itself is not asserted here
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "BEFORE_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); go run . melody:cron:run --once >/dev/null 2>&1; echo status=\$?; AFTER_COUNT=\$(grep -c 'cron runner ignores EntryConfig.User' var/log/dev.log 2>/dev/null || true); echo \"user_warning_before=\${BEFORE_COUNT:-0}\"; echo \"user_warning_after=\${AFTER_COUNT:-0}\""
V2_RUNNER_ONCE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V2_RUNNER_ONCE_STRING}" | grep -q 'status=0'; then
    check_pass "v2 melody:cron:run --once evaluated the schedule in-process and exited cleanly"
else
    check_fail "v2 melody:cron:run --once did not exit cleanly (${V2_RUNNER_ONCE_STRING:-<empty>})"
fi

V2_RUNNER_WARNING_BEFORE_INTEGER="$(printf '%s' "${V2_RUNNER_ONCE_STRING}" | grep -o 'user_warning_before=[0-9]*' | head -1 | cut -d= -f2 || true)"
V2_RUNNER_WARNING_AFTER_INTEGER="$(printf '%s' "${V2_RUNNER_ONCE_STRING}" | grep -o 'user_warning_after=[0-9]*' | head -1 | cut -d= -f2 || true)"

if [[ "${V2_RUNNER_WARNING_AFTER_INTEGER:-0}" -gt "${V2_RUNNER_WARNING_BEFORE_INTEGER:-0}" ]]; then
    check_pass "the v2 runner resolved the shared Configuration (it reported the user-carrying entries, ${V2_RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${V2_RUNNER_WARNING_AFTER_INTEGER:-0})"
else
    check_fail "the v2 runner did not report a user-carrying entry (${V2_RUNNER_WARNING_BEFORE_INTEGER:-0} -> ${V2_RUNNER_WARNING_AFTER_INTEGER:-0}), so nothing proves it parsed the Configuration"
fi

# the entry count in the envelope is deterministic whatever the wall minute: configured counts entries,
# not dispatches, and the v2 example schedules exactly three commands
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once --format=json 2>/dev/null"
V2_RUNNER_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V2_RUNNER_JSON_STRING}" | grep -q '"configured":3'; then
    check_pass "the v2 runner's json envelope counts the three configured entries"
else
    check_fail "the v2 runner's json envelope does not count the three configured entries: ${V2_RUNNER_JSON_STRING:-<empty>}"
fi

check_section_end "V2 CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V2 DATABASE MIGRATIONS — the db:* family runs the same set the providers apply at first resolution
# ---------------------------------------------------------------------------------------------------

check_section_start "V2 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops all five tables of the v2 example's own
# melody_example_v2 database, the journal among them, because this major keeps the journal beside the
# catalogue in one set instead of a context of its own. Every later step exists to put the state back —
# the second migrate restores the schema, and the two resolutions after it reseed the catalogue and the
# user directory — so the section must run to its end whatever the intermediate verdicts, which check_fail
# already guarantees. The journal is append-only with no seeds, so an empty journal IS its restored state.
# The dev-supervised v2 application keeps working through it: its resolved repositories hold only the
# *bun.DB handle
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:init >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v2 db:init is idempotent over the existing bookkeeping tables"
else
    check_fail "v2 db:init failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v2 db:migrate answers success over the set the providers already applied"
else
    check_fail "v2 db:migrate failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V2_STATUS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${V2_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V2_STATUS_OUTPUT_STRING}" | grep -q '20260818000003'; then
    check_pass "v2 db:status reports the applied set by name with nothing pending"
else
    check_fail "v2 db:status does not report the applied set (${V2_STATUS_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v2 db:rollback reverted the last group (the five live tables are dropped until the next step)"
else
    check_fail "v2 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'applied 5 migrations'; then
    check_pass "v2 db:migrate re-applied the five migrations the rollback reverted"
else
    check_fail "v2 db:migrate did not re-apply the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# a fresh process resolves the product provider, which runs the same migration set programmatically and
# then reseeds the empty catalogue — the first-request tolerance the migration switch had to preserve,
# and the step that restores three of the four seeded tables
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . product:list 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'prod-1'; then
    check_pass "a fresh v2 resolution migrated and reseeded the emptied catalogue (prod-1 is back)"
else
    check_fail "the fresh v2 resolution did not restore the catalogue (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the user table has no command of its own; resolving the user repository by name through debug:container
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "resolving the v2 user repository reseeded the user directory"
else
    check_fail "the v2 user repository resolution failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V2 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V2 DEBUG COMMANDS — the dev-registered family answers from the v2 example
# ---------------------------------------------------------------------------------------------------

check_section_start "V2 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
V2_SECRETS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

V2_API_TOKEN_ENTRY_STRING="$(printf '%s' "${V2_SECRETS_JSON_STRING}" | grep -o '"name":"APP_API_TOKEN"[^}]*' | head -1 || true)"
if printf '%s' "${V2_API_TOKEN_ENTRY_STRING}" | grep -q '"value":"\*\*\*\*\*\*\*\*"' && printf '%s' "${V2_API_TOKEN_ENTRY_STRING}" | grep -q '"isSecret":true'; then
    check_pass "v2 APP_API_TOKEN is marked secret and its value is redacted"
else
    check_fail "v2 APP_API_TOKEN is not redacted: ${V2_API_TOKEN_ENTRY_STRING:-<entry missing>}"
fi

# a negative over an ABSENT entry proves nothing: the entry has to exist before its content is inspected
if [[ "" = "${V2_API_TOKEN_ENTRY_STRING}" ]]; then
    check_fail "the v2 APP_API_TOKEN entry is missing from debug:parameters, so nothing was inspected for a raw credential"
elif printf '%s' "${V2_API_TOKEN_ENTRY_STRING}" | grep -q 'example-api-token'; then
    check_fail "the v2 APP_API_TOKEN entry leaks the raw credential: ${V2_API_TOKEN_ENTRY_STRING}"
else
    check_pass "the v2 APP_API_TOKEN entry carries no raw credential"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:version 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'MELODY: v'; then
    check_pass "v2 debug:version reports the framework version"
else
    check_fail "v2 debug:version did not report the framework version (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:events --format=json 2>/dev/null"
V2_EVENTS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
if printf '%s' "${V2_EVENTS_JSON_STRING}" | grep -q '"command":"debug:events"'; then
    check_pass "v2 debug:events answers its json envelope"
else
    check_fail "v2 debug:events did not answer its envelope (${V2_EVENTS_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:middleware 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'static'; then
    check_pass "v2 debug:middleware lists the static middleware of the example's pipeline"
else
    check_fail "v2 debug:middleware did not list the pipeline (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the v1 sibling asserts one check more here — the request-scoped registration debug:container renders in
# a block of its own. This major's example declares no scoped service yet, so the probe would have nothing
# to look for; it arrives with the scoped wiring, not before it
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --limit=0 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V2_CONTAINER_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V2_CONTAINER_OUTPUT_STRING}" | grep -q 'service.example.product.repository'; then
    check_pass "v2 debug:container lists the example's registered services"
else
    check_fail "v2 debug:container did not list the example services (${V2_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V2 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V2 COMMAND OUTPUT ENVELOPE — the example's own commands render through cli/output
# ---------------------------------------------------------------------------------------------------

check_section_start "V2 COMMAND OUTPUT ENVELOPE" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . product:list --format=json 2>/dev/null"
V2_PRODUCT_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V2_PRODUCT_JSON_STRING}" | grep -q '"command":"product:list"'; then
    check_pass "v2 product:list --format=json answers one envelope naming the command"
else
    check_fail "v2 product:list --format=json did not answer its envelope (${V2_PRODUCT_JSON_STRING:-<empty>})"
fi

if printf '%s' "${V2_PRODUCT_JSON_STRING}" | grep -q '"prod-1"'; then
    check_pass "the v2 product envelope carries the seeded catalogue"
else
    check_fail "the v2 product envelope does not carry the seeded catalogue (${V2_PRODUCT_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . catalog:journal --limit=1 --format=json 2>/dev/null"
V2_JOURNAL_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${V2_JOURNAL_JSON_STRING}" | grep -q '"limit":1'; then
    check_pass "v2 catalog:journal honours the standard --limit and echoes it in the payload"
else
    check_fail "v2 catalog:journal did not echo the standard limit (${V2_JOURNAL_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . catalog:report:refresh 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'RECORDED_AT'; then
    check_pass "v2 catalog:report:refresh renders the reading through the framework table"
else
    check_fail "v2 catalog:report:refresh did not render the framework table (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V2 COMMAND OUTPUT ENVELOPE" "${TAG_VALIDATE}" "e2e"
# ---------------------------------------------------------------------------------------------------

finish_checks "stack" "${EXPECTED_CHECK_COUNT_INTEGER}"
