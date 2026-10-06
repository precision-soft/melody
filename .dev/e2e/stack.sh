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
#   - OUTBOX FACTORIES   /outbox/enqueue on the dev-supervised example, signed in as the seeded editor, writes
#                        through the lazily-resolved store, melody:outbox:relay publishes it from a separate
#                        process and /outbox/status shows the sent count grow; anonymous, the doors answer 401
#   - MESSAGE BUS        POST /messagebus/dispatch on the dev-supervised example, signed in, hands the message to
#                        the async transport instead of handling it inline, and melody:messagebus:consume handles
#                        it from a separate process — exactly one message, on a queue drained first; anonymous 401
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
#   - V3 BOOT CONFIG     a built binary under --mode=cli, --mode=bogus and --mode=http over a cli default, an empty .env
#                        refusing both modes, .env.dev and .env.dev.local precedence, an empty MELODY_ENV, %% and a
#                        malformed %env( placeholder, and a process variable logged as ignored while .env keeps its value
#   - V3 LOGIN FAILURE   a refused password on the supervised example writes the security login failure line, read out
#                        of band from its journal and tied to the 401 by its request id
#   - V3 DEBUG           the dev-registered family, the armed teardown plan in waves (the hub before the logger), the
#                        middleware order and the route manifest filtered by zone
#   - V3 MIGRATIONS      the db:* family over the v3 example's own one-migration schema — the catalogue, the
#                        journal and the two-factor enrollment table — with the machine document asserted
#                        from a live application and the rollback read straight out of mysql
#   - V3 ROLE GRANT      example:grant:role writes the widened role set through the repository's atomic door, read out of band; a second grant is answered as held
#   - V3 DATABASE RESET  example:db:reset refuses without --force, and with it drops the schema, applies it
#                        again, empties the audit trail the module's table keeps and reseeds all four
#                        nomenclatures, the audited ones as one audit transaction of the run — the one state this
#                        application has, restored from the database side
#   - V3 SCHEMA DRIFT    a column dropped out of band makes product:list refuse the volume by table and column,
#                        naming example:db:reset --force, and the command runs again after the reset
#   - V3 CACHE CLEAR     example:cache:clear empties the shared cache namespace on its own, the databases
#                        untouched, with the entry a listing cached read out of redis before and after
#   - V3 TWO-FACTOR RELEASE  the schema the reset just applied ties an enrollment to its account with a
#                        cascading foreign key, read out of information_schema — the half of the release
#                        that holds when no listener runs
#   - V3 EXCHANGE RATES  the seeded quote, the refresh that replaces it with the provider's, the quote and the
#                        provider's own stamp read back out of band, the provider's clock measured at no offset,
#                        the report export, and the two configurations that gate the door:
#                        a provider that refuses exits non-zero and moves nothing, an absent one is a no-op
#   - V3 READING ARCHIVE the example's SECOND database, on postgres: its own command family pinned to its own
#                        manager, the reading a refresh appends read back out of band, the archived instant
#                        agreeing with the payload that carries it, two concurrent refreshes recording one
#                        reading between them, and the listing refused to an anonymous caller
#   - V1 CRON RUNNER     the v1 example registers the cron module: melody:cron:run boots from the shared
#                        Configuration, reports its user-carrying entries and answers the json envelope
#   - V1 MIGRATIONS      the bunorm/migrate command family runs the same migration set the v1 providers
#                        apply at first resolution: init, status, a rollback/migrate round trip over the
#                        live tables, and the resolutions that reseed what the round trip emptied
#   - V1 DATABASE RESET  example:db:reset over both of this example's databases, the journal half asserted
#                        through db:journal:status because the harness has no postgres reader
#   - V1 DEBUG           the dev-registered debug commands answer from the v1 example, debug:parameters
#                        redacting the marked APP_API_TOKEN
#   - V1 ENVELOPE        the v1 example commands render through the cli/output envelope: one json document
#                        naming the command, the standard --limit, and the framework table
#   - V2 CRON RUNNER     the same for the v2 example, which registers the cron module since the tier-one lot
#   - V2 MIGRATIONS      the db:* family over the v2 example's own one-migration schema, journal included:
#                        this major keeps the journal beside the catalogue instead of in a second database
#   - V2 DATABASE RESET  example:db:reset over this major's single database, journal included
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
EXPECTED_CHECK_COUNT_INTEGER=244
readonly EXPECTED_CHECK_COUNT_INTEGER

# state the scope in the output, so a reader never has to infer which major these checks covered
info "stack checks drive the v3 example application: ${EXAMPLE_DIRECTORY_STRING}"
info "v3-only module: wiring generate, openapi generate, outbox relay, encrypt bulk"
info "v3-only through the example app: exclusive/grant demo commands, command-owned --role, app:info process_role, product:list --limit, cron configuration, optional-env-key parameter"
info "the v3 example registers the bunorm/migrate family over its own one-migration schema (V3 DATABASE MIGRATIONS)"
info "the V1 sections drive the v1 example application: $(e2e_example_directory 1) (cron module, bunorm/migrate family, cli/output envelope, dev debug commands)"
info "the V2 sections drive the v2 example application: $(e2e_example_directory 2) (the same four, over a one-migration schema that carries the journal)"
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

# the ownership line names the APPLICATION, not just the command: the line is read back off the file the run
# wrote, as the exact line the sweep asks for, under the cli name the example declares in its .env
if printf '%s' "${CRONTAB_WITH_USER_STRING}" | grep -qxF '# owned by melody:cron:generate for melody-example'; then
    check_pass "the generated crontab's ownership line names the application (melody-example)"
else
    check_fail "the generated crontab's ownership line does not name the application"
fi

# two applications sharing one output directory: a NEIGHBOUR is a second binary of the example, built into its
# own directory and declaring another cli name in its .env.local (the process environment is ignored by
# design; the same shape as the teardown-budget section). It writes its crontab beside the example's, a
# LEGACY file written before the line named the application is planted with the bare marker, and then the
# example sweeps: its own stale destination is emptied, the neighbour's and the legacy file are left byte
# for byte — the very files the bare, application-blind marker used to empty on every deploy. Both
# survivors are read back from the directory afterwards, not from the sweep's report of itself.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-neighbour-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-neighbour\" . >/tmp/example-neighbour-build.log 2>&1; then
        echo build_failed=1
        cat /tmp/example-neighbour-build.log
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    printf 'MELODY_CLI_NAME=melody-example-neighbour\n' > \"\${WORK_DIRECTORY}/.env.local\"
    rm -rf /tmp/cron-prune-band && mkdir -p /tmp/cron-prune-band
    (cd \"\${WORK_DIRECTORY}\" && ./example-neighbour melody:cron:generate --out /tmp/cron-prune-band/neighbour.crontab >/dev/null 2>&1; echo neighbour_status=\$?)
    printf '#\n# GENERATED FILE\n# DO NOT EDIT LOCALLY\n#\n# owned by melody:cron:generate\n#\n*/5 * * * * root /usr/local/bin/legacy-job\n' > /tmp/cron-prune-band/legacy.crontab
    go run . melody:cron:generate --out /tmp/cron-prune-band/stale.crontab >/dev/null 2>&1
    NEIGHBOUR_BEFORE=\$(md5sum < /tmp/cron-prune-band/neighbour.crontab)
    LEGACY_BEFORE=\$(md5sum < /tmp/cron-prune-band/legacy.crontab)
    go run . melody:cron:generate --out /tmp/cron-prune-band/crontab --prune >/dev/null 2>&1
    echo prune_status=\$?
    NEIGHBOUR_AFTER=\$(md5sum < /tmp/cron-prune-band/neighbour.crontab 2>/dev/null)
    LEGACY_AFTER=\$(md5sum < /tmp/cron-prune-band/legacy.crontab 2>/dev/null)
    if grep -qxF '# owned by melody:cron:generate for melody-example-neighbour' /tmp/cron-prune-band/neighbour.crontab && grep -q 'product:list' /tmp/cron-prune-band/neighbour.crontab; then echo neighbour_named_and_live=1; else echo neighbour_named_and_live=0; fi
    if [ \"\${NEIGHBOUR_BEFORE}\" = \"\${NEIGHBOUR_AFTER}\" ]; then echo neighbour_intact=1; else echo neighbour_intact=0; fi
    if [ \"\${LEGACY_BEFORE}\" = \"\${LEGACY_AFTER}\" ] && grep -qxF '# owned by melody:cron:generate' /tmp/cron-prune-band/legacy.crontab; then echo legacy_intact=1; else echo legacy_intact=0; fi
    if grep -qxF '# owned by melody:cron:generate for melody-example' /tmp/cron-prune-band/stale.crontab && ! grep -q 'product:list' /tmp/cron-prune-band/stale.crontab; then echo own_stale_emptied=1; else echo own_stale_emptied=0; fi"
NEIGHBOUR_SWEEP_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${NEIGHBOUR_SWEEP_STRING}" | grep -qx 'neighbour_named_and_live=1' && printf '%s' "${NEIGHBOUR_SWEEP_STRING}" | grep -qx 'prune_status=0'; then
    check_pass "a second application built from the example writes its own ownership line (melody-example-neighbour) beside the example's"
else
    check_fail "the neighbour application did not write a crontab under its own ownership line: ${NEIGHBOUR_SWEEP_STRING}"
fi

if printf '%s' "${NEIGHBOUR_SWEEP_STRING}" | grep -qx 'neighbour_intact=1'; then
    check_pass "--prune left the neighbour application's crontab byte for byte"
else
    check_fail "--prune touched the neighbour application's crontab: ${NEIGHBOUR_SWEEP_STRING}"
fi

if printf '%s' "${NEIGHBOUR_SWEEP_STRING}" | grep -qx 'legacy_intact=1'; then
    check_pass "--prune left a crontab written before the line named the application byte for byte, bare marker still on it"
else
    check_fail "--prune touched a crontab carrying the bare marker: ${NEIGHBOUR_SWEEP_STRING}"
fi

if printf '%s' "${NEIGHBOUR_SWEEP_STRING}" | grep -qx 'own_stale_emptied=1'; then
    check_pass "--prune still emptied the example's own stale destination down to its named header"
else
    check_fail "--prune no longer sweeps the example's own stale destination: ${NEIGHBOUR_SWEEP_STRING}"
fi

# the k8s manifests open with the same marker as a leading YAML comment, which is what makes a k8s output
# directory reconcilable by the same sweep
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "rm -f /tmp/cron-band-k8s.yaml; go run . melody:cron:generate --template k8s --image registry.example/app:1 --out /tmp/cron-band-k8s.yaml >/dev/null 2>&1; cat /tmp/cron-band-k8s.yaml 2>/dev/null"
K8S_MANIFEST_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${K8S_MANIFEST_STRING}" | grep -qxF '# owned by melody:cron:generate for melody-example' && printf '%s' "${K8S_MANIFEST_STRING}" | grep -q 'apiVersion: batch/v1'; then
    check_pass "the k8s manifests carry the application's ownership line beside their CronJob documents"
else
    check_fail "the k8s manifests do not carry the application's ownership line (or rendered no CronJob)"
fi

check_section_end "CRON CRONTAB-NO-USER TEMPLATE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 CRON GENERATE — the destination, the binary and the heartbeat a bare generate resolves on its own
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 CRON GENERATE" "${TAG_VALIDATE}" "e2e"

# a built binary in a directory of its own, beside a copy of the example's .env, so the project directory is the
# workspace: a generate with no --out writes where the destination parameter points under it, every entry names the
# binary that ran the generate, since neither --binary nor its parameter is set, and the heartbeat the example's
# environment opts into is one more entry touching a file under the logs directory. The k8s arm refuses a namespace
# that is not an RFC 1123 label before any manifest is written, naming it
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-cron-generate-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-cron\" . >/tmp/example-cron-build.log 2>&1; then
        echo build_failed=1
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    ./example-cron melody:cron:generate >/tmp/example-cron-generate.log 2>&1
    echo \"generate_exit=\$?\"
    if [ -s generated_conf/cron/crontab ]; then echo destination_written=1; else echo destination_written=0; fi
    grep -v '^#' generated_conf/cron/crontab 2>/dev/null | grep -c \"^[^ ]* [^ ]* [^ ]* [^ ]* [^ ]* [^ ]* \${WORK_DIRECTORY}/example-cron \" | sed 's/^/entries_on_the_binary=/'
    grep -v '^#' generated_conf/cron/crontab 2>/dev/null | grep -v \"\${WORK_DIRECTORY}/example-cron \" | grep -v '/heartbeat.crontab\$' | grep -c '[a-z]' | sed 's/^/entries_elsewhere=/'
    grep -c \"^\* \* \* \* \* [^ ]* /bin/touch \${WORK_DIRECTORY}/var/log/cron/heartbeat.crontab\$\" generated_conf/cron/crontab 2>/dev/null | sed 's/^/heartbeat_entries=/'
    ./example-cron melody:cron:generate --template k8s --image example:e2e --namespace Bad_NS --out \"\${WORK_DIRECTORY}/k8s.yaml\" >/tmp/example-cron-generate.log 2>&1
    echo \"k8s_bad_namespace_exit=\$?\"
    if grep -q 'k8s namespace \"Bad_NS\" is not a valid RFC 1123 label' /tmp/example-cron-generate.log; then echo k8s_bad_namespace_named=1; else echo k8s_bad_namespace_named=0; fi
    if [ -e \"\${WORK_DIRECTORY}/k8s.yaml\" ]; then echo k8s_manifest_written=1; else echo k8s_manifest_written=0; fi
    cd / && rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-cron-build.log /tmp/example-cron-generate.log"
V3_CRON_GENERATE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
printf '%s\n' "${V3_CRON_GENERATE_OUTPUT_STRING}"

cron_generate_has() {
    printf '%s' "${V3_CRON_GENERATE_OUTPUT_STRING}" | grep -qx "${1}"
}

cron_generate_value() {
    printf '%s' "${V3_CRON_GENERATE_OUTPUT_STRING}" | grep -o "^${1}=.*" | head -1 || true
}

if cron_generate_has 'generate_exit=0' && cron_generate_has 'destination_written=1'; then
    check_pass "melody:cron:generate with no --out writes generated_conf/cron/crontab, where the destination parameter points"
else
    check_fail "the bare generate did not write the parameter's destination ($(cron_generate_value generate_exit) $(cron_generate_value destination_written))"
fi

if [[ 0 -lt "$(cron_generate_value entries_on_the_binary | cut -d= -f2)" ]] && cron_generate_has 'entries_elsewhere=0'; then
    check_pass "every generated command entry runs the binary that ran the generate ($(cron_generate_value entries_on_the_binary | cut -d= -f2) entries), none some other path"
else
    check_fail "the entries do not name the running binary ($(cron_generate_value entries_on_the_binary) $(cron_generate_value entries_elsewhere))"
fi

if cron_generate_has 'heartbeat_entries=1'; then
    check_pass "the heartbeat the environment opts into is one every-minute entry touching heartbeat.crontab under the logs directory"
else
    check_fail "the generated crontab carries no heartbeat entry under the logs directory ($(cron_generate_value heartbeat_entries))"
fi

if cron_generate_has 'k8s_bad_namespace_exit=1' && cron_generate_has 'k8s_bad_namespace_named=1' && cron_generate_has 'k8s_manifest_written=0'; then
    check_pass "the k8s template refuses the namespace Bad_NS as not an RFC 1123 label, naming it, and writes no manifest"
else
    check_fail "the k8s template did not refuse Bad_NS ($(cron_generate_value k8s_bad_namespace_exit) $(cron_generate_value k8s_bad_namespace_named) $(cron_generate_value k8s_manifest_written))"
fi

check_section_end "V3 CRON GENERATE" "${TAG_VALIDATE}" "e2e"

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

# the entry count in the envelope is deterministic whatever the wall minute: configured counts entries, not
# dispatches. v3 schedules FOUR commands where v1 and v2 schedule three — the fourth is the exchange-rate
# refresh, which exists only on this major because only this example calls an outbound provider — so the
# number is asserted per major rather than shared, and it moves in the same edit that adds or removes an
# entry from the Configuration.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once --format=json 2>/dev/null"
RUNNER_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

if printf '%s' "${RUNNER_JSON_STRING}" | grep -q '"configured":4'; then
    check_pass "the v3 runner's json envelope counts the four configured entries"
else
    check_fail "the v3 runner's json envelope does not count the four configured entries: ${RUNNER_JSON_STRING:-<empty>}"
fi

# the information command is the worker's minute heartbeat, so the once-run dispatches it whatever the wall minute
# (on the hour and the half hour the rates and the reading join it): the envelope names its run under the schedule
# every minute matches, the journal holds the run's output record under the same run id, and the evaluated minute is
# stamped in Bucharest, the zone the configuration declares (+02:00 or +03:00 with daylight saving)
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:cron:run --once --format=json 2>/dev/null | tail -1 >/tmp/cron-once.json
    tr -d ' \n\t' </tmp/cron-once.json | grep -o '\"at\":\"[^\"]*\"' | head -1 | sed 's/^/at=/'
    RUN_ID=\$(tr -d ' \n\t' </tmp/cron-once.json | grep -o '{\"command\":\"app:info\",\"schedule\":\"\*\*\*\*\*\",\"arguments\":\[\],\"runId\":\"[0-9a-f-]*\",\"durationMilliseconds\":[0-9]*,\"failed\":false' | grep -o '\"runId\":\"[0-9a-f-]*\"' | cut -d'\"' -f4)
    echo \"heartbeat_run=\${RUN_ID:-none}\"
    echo \"journaled=\$(grep '\"message\":\"cron: scheduled command output\"' var/log/dev.log | grep -c \"\${RUN_ID:-none}\")\"
    rm -f /tmp/cron-once.json"
V3_CRON_HEARTBEAT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_CRON_HEARTBEAT_STRING}" | grep -qE '^at="at":"[0-9T:-]+\+0[23]:00"$' \
    && printf '%s' "${V3_CRON_HEARTBEAT_STRING}" | grep -qE '^heartbeat_run=[0-9a-f]{8}-' \
    && printf '%s' "${V3_CRON_HEARTBEAT_STRING}" | grep -qx 'journaled=1'; then
    check_pass "the once-run dispatched the minute heartbeat app:info, journaled under its run id, on a minute stamped in Bucharest"
else
    check_fail "the minute heartbeat did not hold: ${V3_CRON_HEARTBEAT_STRING:-<empty>}"
fi

check_section_end "CRON IN-PROCESS RUNNER" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# COMMAND-OWNED ROLE FLAG — a command's own --role after the command name is not the runtime process role
# ---------------------------------------------------------------------------------------------------

check_section_start "COMMAND-OWNED ROLE FLAG" "${TAG_VALIDATE}" "e2e"

# the value is one the command REFUSES, and the refusal quotes it back: that is what shows the flag reached
# this command rather than the runtime's process-role parser, and it leaves the directory untouched. The
# command grants for real now, so an assertion built on a successful grant would have to give the role back,
# and there is no door that revokes one.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role 'ROLE_X,ADMIN' ada 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${GRANT_OUTPUT_STRING}" | grep -q 'role "ROLE_X,ADMIN" must not contain commas'; then
    check_pass "a command's own --role after the command name reaches the command (not the runtime role parser)"
else
    check_fail "the command-owned --role flag did not reach the command (${GRANT_OUTPUT_STRING:-<empty>})"
fi

# a spelling outside the application's vocabulary is refused by name, before the directory is read: the
# voter compares a role's spelling exactly, so a misspelt role used to be stored, announced as granted and
# grant nothing
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role ROLE_ADMIM ada 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
MISSPELT_GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${MISSPELT_GRANT_OUTPUT_STRING}" | grep -q 'role "ROLE_ADMIM" is not one this application knows (ROLE_USER, ROLE_EDITOR, ROLE_ADMIN)'; then
    check_pass "a role outside the application's vocabulary is refused by name, with the vocabulary"
else
    check_fail "a misspelt role was not refused (${MISSPELT_GRANT_OUTPUT_STRING:-<empty>})"
fi

check_section_end "COMMAND-OWNED ROLE FLAG" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# LAZY SERVICE RESOLUTION — the container.Lazy handle resolves the user service at first run, not at boot
# ---------------------------------------------------------------------------------------------------

check_section_start "LAZY SERVICE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# one invocation on purpose: the lazy-resolution marker and the grant line must come from the SAME run,
# proving the handle resolved inside the command body and the command still completed its work afterwards
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role ROLE_ADMIN ada 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
LAZY_GRANT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

if printf '%s' "${LAZY_GRANT_OUTPUT_STRING}" | grep -q 'user service resolved lazily: user "ada" known=false'; then
    check_pass "the user service resolved lazily inside the command body (unknown user reported)"
else
    check_fail "the lazy-resolution marker did not print (${LAZY_GRANT_OUTPUT_STRING:-<empty>})"
fi

# the command used to announce the grant here too, for an account it had just reported unknown. It refuses
# now, and the refusal is the second half of the same proof: the lookup that answered known=false is the one
# the lazily resolved service performed.
if printf '%s' "${LAZY_GRANT_OUTPUT_STRING}" | grep -q 'user "ada" does not exist'; then
    check_pass "the same invocation refused the unknown account the lazy resolve had just reported"
else
    check_fail "the refusal did not follow the lazy resolution (${LAZY_GRANT_OUTPUT_STRING:-<empty>})"
fi

check_section_end "LAZY SERVICE RESOLUTION" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# a session on the dev-supervised example for the sections whose routes carry a role — the outbox and
# message bus doors write into a backend, so they sit behind ROLE_EDITOR like the catalogue writes. The
# http client the dev image ships is busybox wget, which has no cookie jar: the sign-in captures the
# Set-Cookie header the login door answers (-S prints the server headers on stderr) and every call of the
# section presents it back through --header. The snippets are container-side bash, spliced verbatim into
# the run_in_dev_capture scripts below, hence the quoted here-documents: nothing in them expands here.
# ---------------------------------------------------------------------------------------------------

read -r -d '' EXAMPLE_SIGN_IN_SNIPPET <<'SNIPPET' || true
    SESSION_COOKIE_VALUE=$(wget -q -S -O /dev/null --header='Content-Type: application/json' --post-data='{"username":"editor","password":"editor"}' --header='Accept: application/json' "${EXAMPLE_BASE_URL}/login/" 2>&1 | sed -n 's/^ *Set-Cookie: *\([^;]*\).*/\1/p' | head -1)
    if [ -z "${SESSION_COOKIE_VALUE}" ]; then
        echo example_session=0
    else
        echo example_session=1
    fi
    SESSION_COOKIE_HEADER="Cookie: ${SESSION_COOKIE_VALUE}"
SNIPPET

read -r -d '' EXAMPLE_SIGN_OUT_SNIPPET <<'SNIPPET' || true
    wget -q -O /dev/null --header="${SESSION_COOKIE_HEADER}" "${EXAMPLE_BASE_URL}/logout/" 2>/dev/null || true
SNIPPET

# ---------------------------------------------------------------------------------------------------
# OUTBOX FACTORIES END-TO-END — http enqueue on the supervised app, relay from a separate cli process
# ---------------------------------------------------------------------------------------------------

check_section_start "OUTBOX FACTORIES END-TO-END" "${TAG_VALIDATE}" "e2e"

# the http half runs against the example the dev container already supervises on EXAMPLE_BASE_URL (the
# same loopback address run.sh uses); wget is the http client the dev image ships (no curl). The relay
# half is a separate cli process, so store and relay factories are exercised across process boundaries.
# The sent count is read before and after: /outbox/status going green on rows sent by EARLIER runs would
# be a vacuous pass, so the assertion is that the count GREW, not that it exists
# the relay publishes onto the broker's outbox_notice queue, which nothing in the development stack consumes, so the
# queue is emptied out of band before the run: every message it holds afterwards is this run's, and the broker is not
# left one message fuller by every run
outbox_queue_depth() {
    docker_compose_no_log exec -T rabbitmq rabbitmqctl list_queues -q name messages </dev/null 2>/dev/null | awk '"outbox_notice" == $1 { print $2 }' || true
}
docker_compose_no_log exec -T rabbitmq rabbitmqctl purge_queue -q outbox_notice </dev/null >/dev/null 2>&1 || true
OUTBOX_QUEUE_BEFORE_STRING="$(outbox_queue_depth)"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "ANONYMOUS_STATUS=\$(wget -q -S -O /dev/null \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)
    echo \"anonymous_status=\${ANONYMOUS_STATUS:-none}\"
${EXAMPLE_SIGN_IN_SNIPPET}
    STATUS_BODY=\$(wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>/dev/null || true)
    case \"\${STATUS_BODY}\" in
        *'\"success\":true'*) echo outbox_reachable=1 ;;
        *) echo outbox_reachable=0 ;;
    esac
    BEFORE_SENT=\$(printf '%s' \"\${STATUS_BODY}\" | grep -o '\"sent\":[0-9]*' | head -1 | cut -d: -f2)
    echo \"before_sent=\${BEFORE_SENT:-0}\"
    wget -q -O- --post-data='' --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/outbox/enqueue?reference=stack-e2e\" 2>/dev/null || true
    echo ''
    # the outbox available_at has second precision, so the relay claims the row only once it is a full second old
    sleep 2
    go run . melody:outbox:relay --limit 1 >/tmp/outbox-relay.log 2>&1
    echo \"relay_status=\$?\"
    AFTER_BODY=\$(wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>/dev/null || true)
    printf '%s\n' \"\${AFTER_BODY}\"
    AFTER_SENT=\$(printf '%s' \"\${AFTER_BODY}\" | grep -o '\"sent\":[0-9]*' | head -1 | cut -d: -f2)
    echo \"after_sent=\${AFTER_SENT:-0}\"
${EXAMPLE_SIGN_OUT_SNIPPET}"
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

# the doors carry ROLE_EDITOR: the anonymous read is the arm that keeps them off the public rule table, and the
# session is what the rest of the section drives them through — without it every number below is a 401 body
if printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q 'anonymous_status=401'; then
    check_pass "GET /outbox/status refuses an anonymous client with 401"
else
    check_fail "GET /outbox/status did not answer 401 to an anonymous client ($(printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -o 'anonymous_status=[a-z0-9]*' | head -1)) — the outbox doors are public"
fi

if printf '%s' "${OUTBOX_OUTPUT_STRING}" | grep -q 'example_session=1'; then
    check_pass "the seeded editor signed in on the supervised example and the section carries its session"
else
    check_fail "the seeded editor could not sign in on the supervised example — the outbox doors were driven without a session"
fi

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

# the message the relay handed the broker, read out of band: the queue holds exactly the one this run published, its
# message id is the outbox row's own — the key a consumer deduplicates redeliveries on — and the row is the one the
# store marked sent. It is taken with an acknowledgement, so the queue is left empty
OUTBOX_QUEUE_AFTER_STRING="$(outbox_queue_depth)"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "AUTHORIZATION=\"Authorization: Basic \$(printf 'guest:guest' | base64)\"
    wget -q -O - --header=\"\${AUTHORIZATION}\" --header='Content-Type: application/json' --post-data='{\"count\":1,\"ackmode\":\"ack_requeue_false\",\"encoding\":\"auto\"}' 'http://rabbitmq:15672/api/queues/%2F/outbox_notice/get' 2>/dev/null"
OUTBOX_MESSAGE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
OUTBOX_MESSAGE_ID_STRING="$(printf '%s' "${OUTBOX_MESSAGE_STRING}" | grep -o '"message_id":"melody-outbox-[0-9]*"' | head -1 | grep -o '[0-9]*"$' | tr -d '"' || true)"
OUTBOX_ROW_STATUS_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT status FROM melody_outbox WHERE id = '${OUTBOX_MESSAGE_ID_STRING:-0}'")"
OUTBOX_QUEUE_LEFT_STRING="$(outbox_queue_depth)"
if [[ "0" == "${OUTBOX_QUEUE_BEFORE_STRING}" ]] && [[ "1" == "${OUTBOX_QUEUE_AFTER_STRING}" ]] && [[ -n "${OUTBOX_MESSAGE_ID_STRING}" ]] \
    && [[ "sent" == "${OUTBOX_ROW_STATUS_STRING}" ]] && printf '%s' "${OUTBOX_MESSAGE_STRING}" | grep -q 'stack-e2e' && [[ "0" == "${OUTBOX_QUEUE_LEFT_STRING}" ]]; then
    check_pass "the relayed notice reached outbox_notice as the one message there, carrying melody-outbox-${OUTBOX_MESSAGE_ID_STRING}, the id of the row the store marked sent (read out of band)"
else
    check_fail "the relayed notice was not the one message on the broker under its row's id (queue ${OUTBOX_QUEUE_BEFORE_STRING:-?} -> ${OUTBOX_QUEUE_AFTER_STRING:-?} -> ${OUTBOX_QUEUE_LEFT_STRING:-?}, message id ${OUTBOX_MESSAGE_ID_STRING:-<none>}, row ${OUTBOX_ROW_STATUS_STRING:-<none>})"
fi

# the relay drains under the application's shared locker, so a replica holding the lease keeps every other one idle:
# with the lease planted out of band in redis under the relay's lock name, a relay run publishes nothing and the row a
# fresh enqueue wrote stays pending; with the lease removed, the next run publishes it. The broker queue is emptied
# afterwards, as the section found it
OUTBOX_LEASE_REFERENCE_STRING="stack-e2e-lease-$(date +%s)"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "redis_command() {
        ARGUMENTS=\"*\$#\r\n\"
        for ARGUMENT in \"\$@\"; do
            ARGUMENTS=\"\${ARGUMENTS}\\\$\${#ARGUMENT}\r\n\${ARGUMENT}\r\n\"
        done
        printf \"\${ARGUMENTS}\" | nc -w 2 redis 6379 | tr -d '\r'
    }
${EXAMPLE_SIGN_IN_SNIPPET}
    wget -q -O /dev/null --post-data='' --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/outbox/enqueue?reference=${OUTBOX_LEASE_REFERENCE_STRING}\" 2>/dev/null || true
${EXAMPLE_SIGN_OUT_SNIPPET}
    sleep 2
    echo \"lease_planted=\$(redis_command SET melody-example-v3:outbox:relay held-by-the-e2e-harness NX PX 30000)\"
    go run . melody:outbox:relay --limit 1 >/dev/null 2>&1
    echo \"held_relay_status=\$?\"
    echo held_run_done=1"
OUTBOX_LEASE_HELD_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
OUTBOX_LEASE_ROW_WHILE_HELD_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT status FROM melody_outbox WHERE payload LIKE '%${OUTBOX_LEASE_REFERENCE_STRING}%' ORDER BY id DESC LIMIT 1")"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "printf '*2\r\n\$3\r\nDEL\r\n\$30\r\nmelody-example-v3:outbox:relay\r\n' | nc -w 2 redis 6379 | tr -d '\r' | sed 's/^/lease_removed=/'
    go run . melody:outbox:relay --limit 1 >/dev/null 2>&1
    echo \"free_relay_status=\$?\""
OUTBOX_LEASE_FREE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
OUTBOX_LEASE_ROW_AFTER_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT status FROM melody_outbox WHERE payload LIKE '%${OUTBOX_LEASE_REFERENCE_STRING}%' ORDER BY id DESC LIMIT 1")"
docker_compose_no_log exec -T rabbitmq rabbitmqctl purge_queue -q outbox_notice </dev/null >/dev/null 2>&1 || true
if printf '%s' "${OUTBOX_LEASE_HELD_STRING}" | grep -qx 'lease_planted=+OK' && printf '%s' "${OUTBOX_LEASE_HELD_STRING}" | grep -qx 'held_relay_status=0' \
    && [[ "pending" == "${OUTBOX_LEASE_ROW_WHILE_HELD_STRING}" ]] && printf '%s' "${OUTBOX_LEASE_FREE_STRING}" | grep -qx 'lease_removed=:1' \
    && [[ "sent" == "${OUTBOX_LEASE_ROW_AFTER_STRING}" ]]; then
    check_pass "a relay run while another holder has the outbox lease publishes nothing (the row stays pending), and the next run after the lease is gone publishes it (read out of band)"
else
    check_fail "the outbox relay did not honour the shared lease: ${OUTBOX_LEASE_HELD_STRING:-<empty>} ${OUTBOX_LEASE_FREE_STRING:-<empty>}, row while held ${OUTBOX_LEASE_ROW_WHILE_HELD_STRING:-<none>}, after ${OUTBOX_LEASE_ROW_AFTER_STRING:-<none>}"
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
    ANONYMOUS_STATUS=\$(wget -q -S -O /dev/null --post-data='' \"\${EXAMPLE_BASE_URL}/messagebus/dispatch\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)
    echo \"anonymous_status=\${ANONYMOUS_STATUS:-none}\"
${EXAMPLE_SIGN_IN_SNIPPET}
    wget -q -O- --post-data='' --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/messagebus/dispatch\" 2>/dev/null || true
    echo ''
${EXAMPLE_SIGN_OUT_SNIPPET}
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
    echo \"after_handled=\${AFTER_HANDLED:-0}\"
    grep 'welcome email sent' var/log/dev.log 2>/dev/null | tail -1 | sed 's/^/welcome_record=/'"
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

# the dispatch door sends a mail per call, so it carries ROLE_EDITOR: the anonymous arm keeps it off the public rule
# table, and the session is what the dispatch below goes through
if printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'anonymous_status=401'; then
    check_pass "POST /messagebus/dispatch refuses an anonymous client with 401"
else
    check_fail "POST /messagebus/dispatch did not answer 401 to an anonymous client ($(printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -o 'anonymous_status=[a-z0-9]*' | head -1)) — the dispatch door is public"
fi

if printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -q 'example_session=1'; then
    check_pass "the seeded editor signed in on the supervised example and the dispatch carries its session"
else
    check_fail "the seeded editor could not sign in on the supervised example — the dispatch was sent without a session"
fi

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

# the record the consumer wrote, as the file journal writes every record: one json object per line whose message and
# level come first, a UTC stamp at full nanosecond width (so the stamps sort as text) and the context carrying the id
# of the consumer's process, which is what ties a line to the process that handled the message
MESSAGE_BUS_WELCOME_RECORD_STRING="$(printf '%s' "${MESSAGE_BUS_OUTPUT_STRING}" | grep -o '^welcome_record=.*' | head -1 | cut -d= -f2- || true)"
if printf '%s' "${MESSAGE_BUS_WELCOME_RECORD_STRING}" | grep -qE '^\{"message":"welcome email sent","level":"info","time":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{9}Z","context":\{.*"processId":"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"'; then
    check_pass "the consumer's record is one json line: message, level, a full-width UTC stamp and the consumer's process id"
else
    check_fail "the consumer's record is not in the journal's json form (${MESSAGE_BUS_WELCOME_RECORD_STRING:-<none>})"
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
# one row the migration has something to do on: planted out of band with both columns in plaintext, for a user with
# no enrollment of its own so no real secret is touched, read back after the run and deleted. The finish record then
# counts exactly the one row, and the columns carry the encryption marker instead of the text that went in
ENCRYPT_PLANT_USER_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT id FROM melody_example_v3_user WHERE id NOT IN (SELECT user_identifier FROM melody_example_v3_two_factor) ORDER BY id LIMIT 1")"
e2e_mysql_scalar "melody_example_v3" "INSERT INTO melody_example_v3_two_factor (user_identifier, secret, recovery_codes, created_at) VALUES ('${ENCRYPT_PLANT_USER_STRING:-none}', 'PLAINTEXT-SECRET-E2E', 'plaintext-recovery-e2e', NOW())" >/dev/null

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

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "grep 'encrypt database migration finished' var/log/dev.log 2>/dev/null | tail -1 | grep -o '\"rows\":[0-9]*' | sed 's/^/finished_/'"
ENCRYPT_ROWS_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
ENCRYPT_PLANTED_STATE_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT CONCAT(LEFT(secret, 11) = CONCAT('<ENC>', CHAR(0), 'gcm1', CHAR(0)), LEFT(recovery_codes, 11) = CONCAT('<ENC>', CHAR(0), 'gcm1', CHAR(0)), secret = 'PLAINTEXT-SECRET-E2E') FROM melody_example_v3_two_factor WHERE user_identifier = '${ENCRYPT_PLANT_USER_STRING:-none}'")"
# the key rotation: the example lists two keys and encrypts under example-2026, so --mode reencrypt --target-key
# example-2027 moves every encrypted column onto the next key, read out of band on the planted row as the key id the
# envelope carries after its marker. A target the example does not list is refused before a row is written, the
# journal naming the key id; both keys stay listed, so every row still decrypts
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:encrypt:database --table melody_example_v3_two_factor --primary-key user_identifier --column secret --column recovery_codes --mode reencrypt --target-key example-2027 >/tmp/encrypt-rotate.log 2>&1; echo rotate_status=\$?
    go run . melody:encrypt:database --table melody_example_v3_two_factor --primary-key user_identifier --column secret --column recovery_codes --mode reencrypt --target-key no-such-key >/tmp/encrypt-rotate.log 2>&1; echo unknown_status=\$?
    grep 'encrypt database migration failed' var/log/dev.log | tail -1 | grep -c '\"keyId\":\"no-such-key\".*\"processedRows\":0' | sed 's/^/unknown_named=/'
    rm -f /tmp/encrypt-rotate.log"
ENCRYPT_ROTATION_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
ENCRYPT_ROTATED_KEYS_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT CONCAT(SUBSTRING_INDEX(SUBSTRING(secret, 12), ':', 1), '/', SUBSTRING_INDEX(SUBSTRING(recovery_codes, 12), ':', 1)) FROM melody_example_v3_two_factor WHERE user_identifier = '${ENCRYPT_PLANT_USER_STRING:-none}'")"
if printf '%s' "${ENCRYPT_ROTATION_STRING}" | grep -qx 'rotate_status=0' && [[ "example-2027/example-2027" == "${ENCRYPT_ROTATED_KEYS_STRING}" ]] \
    && printf '%s' "${ENCRYPT_ROTATION_STRING}" | grep -qx 'unknown_status=1' && printf '%s' "${ENCRYPT_ROTATION_STRING}" | grep -qx 'unknown_named=1'; then
    check_pass "--mode reencrypt --target-key example-2027 moved both columns of the planted row onto the next key (read out of band), and an unlisted key is refused before a row is written"
else
    check_fail "the key rotation did not hold: ${ENCRYPT_ROTATION_STRING:-<empty>}, planted row keys ${ENCRYPT_ROTATED_KEYS_STRING:-<no row>}"
fi

e2e_mysql_scalar "melody_example_v3" "DELETE FROM melody_example_v3_two_factor WHERE user_identifier = '${ENCRYPT_PLANT_USER_STRING:-none}'" >/dev/null
if [[ -n "${ENCRYPT_PLANT_USER_STRING}" ]] && printf '%s' "${ENCRYPT_ROWS_STRING}" | grep -qx 'finished_"rows":1' && [[ "110" == "${ENCRYPT_PLANTED_STATE_STRING}" ]]; then
    check_pass "the plaintext row planted out of band is the one row the migration processed, and both its columns now carry the encryption marker"
else
    check_fail "the planted plaintext row was not encrypted as the one row processed (user ${ENCRYPT_PLANT_USER_STRING:-<none>}, ${ENCRYPT_ROWS_STRING:-<no finish record>}, secret/codes marked and still plain: ${ENCRYPT_PLANTED_STATE_STRING:-<no row>})"
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
        check_fail "the example was still running 30 s after the SIGINT — the cap of this section, not the application's teardown budget — and had to be SIGKILLed"
    fi

    if printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -q 'signal_exit_status=0'; then
        check_pass "one SIGINT while serving http exited zero (the graceful path through NewSignalContext)"
    else
        check_fail "the example did not exit zero on one SIGINT ($(printf '%s' "${SIGNAL_OUTPUT_STRING}" | grep -o 'signal_exit_status=[0-9]*' || echo 'no exit status captured'))"
    fi
fi

check_section_end "GRACEFUL SIGNAL SHUTDOWN" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 DATABASE HANDLES ON EXIT — a graceful stop closes the connections the bunorm registry opened
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 DATABASE HANDLES ON EXIT" "${TAG_VALIDATE}" "e2e"

# the mysql driver names its process in every connection's attributes, so the connections one process holds are
# counted out of band by its pid, apart from every other process on the same user and host. The process is started
# detached, so the count can be read from the harness while it serves, and stopped by one SIGINT in a second call.
# The count alone cannot tell a close from a death, since the kernel closes a dead process's sockets as well; the
# server's Aborted_clients can, because it counts the clients that left without saying goodbye, and a killed process
# moves it by one where a registry that closed its pool leaves it where it was
V3_HANDLES_ABORTED_STATEMENT_STRING="SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Aborted_clients'"
V3_HANDLES_ABORTED_BEFORE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_HANDLES_ABORTED_STATEMENT_STRING}")"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-handles-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-handles\" . >/tmp/example-handles-build.log 2>&1; then
        echo build_failed=1
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    printf 'MELODY_HTTP_ADDRESS=:18086\n' > \"\${WORK_DIRECTORY}/.env.local\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    setsid sh -c './example-handles >/tmp/example-handles.log 2>&1 & echo \$! > example-handles.pid; wait \$!; echo \$? > example-handles.exit' >/dev/null 2>&1 &
    READY=0
    for _ in \$(seq 1 150); do
        if wget -q -O /dev/null http://127.0.0.1:18086/health 2>/dev/null; then
            READY=1
            break
        fi
        sleep 0.2
    done
    echo \"handles_ready=\${READY}\"
    echo \"handles_pid=\$(cat example-handles.pid 2>/dev/null)\""
V3_HANDLES_START_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_HANDLES_PID_STRING="$(printf '%s' "${V3_HANDLES_START_STRING}" | grep -o '^handles_pid=[0-9]*' | cut -d= -f2 || true)"
V3_HANDLES_COUNT_STATEMENT_STRING="SELECT COUNT(*) FROM performance_schema.session_connect_attrs WHERE ATTR_NAME='_pid' AND ATTR_VALUE='${V3_HANDLES_PID_STRING:-none}'"
# the catalogue is opened at its first use, so a process that answered only /health holds no connection; a sign-in
# reads the directory, which opens it, and the count is read again while it serves
V3_HANDLES_IDLE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_HANDLES_COUNT_STATEMENT_STRING}")"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "wget -q -O /dev/null --header='Content-Type: application/json' --post-data='{\"username\":\"handles-probe\",\"password\":\"wrong\"}' http://127.0.0.1:18086/login/ 2>/dev/null || true"
V3_HANDLES_SERVING_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_HANDLES_COUNT_STATEMENT_STRING}")"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "cd /tmp/example-handles-e2e || exit 0
    kill -INT \$(cat example-handles.pid) 2>/dev/null
    for _ in \$(seq 1 150); do
        if [ -f example-handles.exit ]; then
            break
        fi
        sleep 0.2
    done
    if [ ! -f example-handles.exit ]; then
        kill -KILL \$(cat example-handles.pid) 2>/dev/null || true
    fi
    echo \"handles_exit=\$(cat example-handles.exit 2>/dev/null)\"
    cd / && rm -rf /tmp/example-handles-e2e /tmp/example-handles.log /tmp/example-handles-build.log"
V3_HANDLES_STOP_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_HANDLES_AFTER_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_HANDLES_COUNT_STATEMENT_STRING}")"
V3_HANDLES_ABORTED_AFTER_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_HANDLES_ABORTED_STATEMENT_STRING}")"

if printf '%s' "${V3_HANDLES_START_STRING}" | grep -qx 'handles_ready=1' && [[ -n "${V3_HANDLES_PID_STRING}" ]] \
    && [[ "0" == "${V3_HANDLES_IDLE_STRING}" ]] \
    && [[ "${V3_HANDLES_SERVING_STRING}" =~ ^[0-9]+$ ]] && [[ 0 -lt ${V3_HANDLES_SERVING_STRING} ]] \
    && printf '%s' "${V3_HANDLES_STOP_STRING}" | grep -qx 'handles_exit=0' && [[ "0" == "${V3_HANDLES_AFTER_STRING}" ]] \
    && [[ -n "${V3_HANDLES_ABORTED_BEFORE_STRING}" ]] && [[ "${V3_HANDLES_ABORTED_BEFORE_STRING}" == "${V3_HANDLES_ABORTED_AFTER_STRING}" ]]; then
    check_pass "a process that answered only /health held no mysql connection, a sign-in opened the catalogue, and a graceful stop closed the ${V3_HANDLES_SERVING_STRING} connection(s) it held, none of them aborted (read out of band by its pid and the server's Aborted_clients)"
else
    check_fail "the process did not close its connections (ready/pid ${V3_HANDLES_START_STRING:-<none>}, idle ${V3_HANDLES_IDLE_STRING:-?}, serving ${V3_HANDLES_SERVING_STRING:-?}, ${V3_HANDLES_STOP_STRING:-<no exit>}, after ${V3_HANDLES_AFTER_STRING:-?}, Aborted_clients ${V3_HANDLES_ABORTED_BEFORE_STRING:-?} -> ${V3_HANDLES_ABORTED_AFTER_STRING:-?})"
fi

check_section_end "V3 DATABASE HANDLES ON EXIT" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 TRACE EXPORT — spans reach the collector over OTLP, and the ones still batched are flushed on the way out
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 TRACE EXPORT" "${TAG_VALIDATE}" "e2e"

# the development collector prints every span it receives, so its own log is the out-of-band read. A request carrying
# a trace context of the harness's own is found there under that trace id and the example's service name. The flush
# is read on a process of its own: its last request is answered well inside the exporter's batch interval and the
# process is stopped at once, so the span can only reach the collector if the teardown flushes the batch; the same
# arm stopped by SIGKILL, which runs no teardown, is the control, and its span never arrives
new_trace_id() {
    od -An -tx1 -N16 /dev/urandom | tr -d ' \n'
}
V3_TRACE_SERVED_STRING="$(new_trace_id)"
V3_TRACE_FLUSHED_STRING="$(new_trace_id)"
V3_TRACE_KILLED_STRING="$(new_trace_id)"
V3_TRACE_SINCE_STRING="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "wget -q -O /dev/null --header='traceparent: 00-${V3_TRACE_SERVED_STRING}-00f067aa0ba902b7-01' \"\${EXAMPLE_BASE_URL}/health\" 2>/dev/null
    WORK_DIRECTORY=/tmp/example-trace-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-trace\" . >/tmp/example-trace-build.log 2>&1; then
        echo build_failed=1
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    printf 'MELODY_HTTP_ADDRESS=:18087\n' > \"\${WORK_DIRECTORY}/.env.local\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    for ARM in INT:${V3_TRACE_FLUSHED_STRING} KILL:${V3_TRACE_KILLED_STRING}; do
        SIGNAL=\${ARM%%:*}
        TRACE=\${ARM#*:}
        ./example-trace >/tmp/example-trace.log 2>&1 &
        APP_PID=\$!
        READY=0
        for _ in \$(seq 1 150); do
            if wget -q -O /dev/null http://127.0.0.1:18087/health 2>/dev/null; then
                READY=1
                break
            fi
            sleep 0.2
        done
        echo \"trace_ready_\${SIGNAL}=\${READY}\"
        sleep 6
        wget -q -O /dev/null --header=\"traceparent: 00-\${TRACE}-00f067aa0ba902b7-01\" http://127.0.0.1:18087/health 2>/dev/null
        kill -\${SIGNAL} \${APP_PID} 2>/dev/null
        wait \${APP_PID} 2>/dev/null
        echo \"trace_exit_\${SIGNAL}=\$?\"
    done
    cd / && rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-trace.log /tmp/example-trace-build.log"
V3_TRACE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
printf '%s\n' "${V3_TRACE_OUTPUT_STRING}"

sleep 7
V3_TRACE_COLLECTOR_LOG_STRING="$(docker_compose_no_log --profile all logs --no-log-prefix --since "${V3_TRACE_SINCE_STRING}" otel-collector 2>/dev/null || true)"
trace_seen() {
    printf '%s' "${V3_TRACE_COLLECTOR_LOG_STRING}" | grep -c "Trace ID *: ${1}" || true
}

if [[ "1" == "$(trace_seen "${V3_TRACE_SERVED_STRING}")" ]] && printf '%s' "${V3_TRACE_COLLECTOR_LOG_STRING}" | grep -q 'service.name: Str(melody.example)'; then
    check_pass "a request carrying a trace context reached the collector as one span under that trace id, from the melody.example service"
else
    check_fail "the served request's span did not reach the collector (trace ${V3_TRACE_SERVED_STRING} seen $(trace_seen "${V3_TRACE_SERVED_STRING}") times)"
fi

if printf '%s' "${V3_TRACE_OUTPUT_STRING}" | grep -qx 'trace_ready_INT=1' && printf '%s' "${V3_TRACE_OUTPUT_STRING}" | grep -qx 'trace_exit_INT=0' \
    && printf '%s' "${V3_TRACE_OUTPUT_STRING}" | grep -qx 'trace_ready_KILL=1' && printf '%s' "${V3_TRACE_OUTPUT_STRING}" | grep -qx 'trace_exit_KILL=137' \
    && [[ "1" == "$(trace_seen "${V3_TRACE_FLUSHED_STRING}")" ]] && [[ "0" == "$(trace_seen "${V3_TRACE_KILLED_STRING}")" ]]; then
    check_pass "the span of a request answered inside the batch interval reaches the collector when SIGINT stops the process, and is lost when SIGKILL does: the teardown flushed it"
else
    check_fail "the teardown did not flush the batched span ($(printf '%s' "${V3_TRACE_OUTPUT_STRING}" | grep -o '^trace_[a-z_A-Z]*=[0-9]*' | tr '\n' ' '); flushed seen $(trace_seen "${V3_TRACE_FLUSHED_STRING}"), killed seen $(trace_seen "${V3_TRACE_KILLED_STRING}"))"
fi

check_section_end "V3 TRACE EXPORT" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# TEARDOWN BUDGET — MELODY_TEARDOWN_TIMEOUT declared in .env reaches the clean shutdown's shield
# ---------------------------------------------------------------------------------------------------

check_section_start "TEARDOWN BUDGET" "${TAG_VALIDATE}" "e2e"

# the key has no consumer in the example's own .env, so only the default was ever exercised end to end and a
# change that ignored a declared value would leave every band green: this section declares the value itself,
# in the .env.local of a binary built into its own directory (the same shape as the signal section above).
# Two arms separate the declared value from the default. A budget of 1ms cannot hold the teardown, so the
# process exits 1 — the proof of the round trip, since the 10s default exits zero on a teardown that takes a
# millisecond, as the 0s arm shows. The figure itself is read back off whichever of two records the race
# between the shield's two clocks leaves: the step is handed a cooperative deadline of half the budget, and
# when the container close returns on it first the emergency record of that close carries the sub-millisecond
# budget it was cut by, while when the shield's own hard timer fires first the abandon line names the 1ms —
# measured 5 of 8 and 3 of 8 rounds on one host, and 24 of 24 abandon lines with 2 close records BESIDE them on
# another: the two faces are not exclusive, either or both may appear, and pinning one alone was a check red one
# run in three. The budget the close record carries is the cooperative deadline's remainder, which is cut to 0s
# when it has already passed and rendered in ns below a microsecond, so the record is read in all three spellings. A budget of
# 0s is the documented "no deadline": a healthy teardown exits zero under it, where a reader folding zero into
# the default would exit zero as well, which is why the 0s arm only pins that zero is admitted at boot and on
# the exit path. The port is 18084: 18081–18083 are the Go harness's three examples and 18080 the signal
# section's, and the two scripts run in sequence today, not by contract.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-teardown-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-teardown\" . >/tmp/example-teardown-build.log 2>&1; then
        echo build_failed=1
        cat /tmp/example-teardown-build.log
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    for BUDGET in 1ms 0s; do
        printf 'MELODY_HTTP_ADDRESS=:18084\nMELODY_TEARDOWN_TIMEOUT=%s\n' \"\${BUDGET}\" > .env.local
        ./example-teardown > /tmp/example-teardown-\${BUDGET}.log 2>&1 &
        APP_PID=\$!
        READY=0
        for _ in \$(seq 1 150); do
            if wget -q -O /dev/null http://127.0.0.1:18084/health 2>/dev/null; then
                READY=1
                break
            fi
            if ! kill -0 \${APP_PID} 2>/dev/null; then
                break
            fi
            sleep 0.2
        done
        echo \"ready_\${BUDGET}=\${READY}\"
        if [ \"\${READY}\" -ne 1 ]; then
            tail -30 /tmp/example-teardown-\${BUDGET}.log
            kill \${APP_PID} 2>/dev/null || true
            wait \${APP_PID} 2>/dev/null || true
            continue
        fi
        kill -INT \${APP_PID}
        for _ in \$(seq 1 150); do
            if ! kill -0 \${APP_PID} 2>/dev/null; then
                break
            fi
            sleep 0.2
        done
        if kill -0 \${APP_PID} 2>/dev/null; then
            kill -KILL \${APP_PID} 2>/dev/null || true
        fi
        wait \${APP_PID}
        echo \"exit_\${BUDGET}=\$?\"
        grep -c 'did not return within 1ms' /tmp/example-teardown-\${BUDGET}.log | sed \"s/^/abandoned_names_1ms_\${BUDGET}=/\"
        grep -c 'failed to close service container.*\"budget\":\"\([0-9.]*[µn]s\|0s\)\"' /tmp/example-teardown-\${BUDGET}.log | sed \"s/^/close_cut_under_a_millisecond_\${BUDGET}=/\"
    done
    rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-teardown-*.log"
TEARDOWN_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${TEARDOWN_OUTPUT_STRING}"

if printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -q 'build_failed=1'; then
    check_fail "the example did not build, so the teardown budget was not exercised"
elif ! printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -q 'ready_1ms=1'; then
    check_fail "the built example never answered /health under a 1ms teardown budget, so the budget was not exercised"
else
    if printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -qx 'exit_1ms=1'; then
        check_pass "a declared teardown budget of 1ms cuts the teardown short and the process exits 1"
    else
        check_fail "a declared teardown budget of 1ms did not turn the shutdown into an exit 1 ($(printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -o 'exit_1ms=[0-9]*' || echo 'no exit status captured')) — the value in .env is not reaching the shield"
    fi

    if printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -qx 'abandoned_names_1ms_1ms=1' || printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -qx 'close_cut_under_a_millisecond_1ms=1'; then
        check_pass "the declared budget of 1ms is the figure read back on the way out: the shield's abandon line names it, or the container close's record carries the sub-millisecond deadline it was cut by"
    else
        check_fail "neither the abandon line nor the container close's record named the declared budget of 1ms ($(printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -o 'abandoned_names_1ms_1ms=[0-9]*\|close_cut_under_a_millisecond_1ms=[0-9]*' | tr '\n' ' '))"
    fi

    if ! printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -q 'ready_0s=1'; then
        check_fail "the built example never answered /health under a 0s teardown budget — zero must be admitted at boot as the documented no-deadline"
    elif printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -qx 'exit_0s=0'; then
        check_pass "a declared teardown budget of 0s (no deadline) is admitted at boot and a healthy teardown exits zero under it"
    else
        check_fail "a declared teardown budget of 0s did not exit zero ($(printf '%s' "${TEARDOWN_OUTPUT_STRING}" | grep -o 'exit_0s=[0-9]*' || echo 'no exit status captured'))"
    fi
fi

check_section_end "TEARDOWN BUDGET" "${TAG_VALIDATE}" "e2e"

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

# the documented stdout mode is a redirection into a Go file: the stream has to begin with the generated source, not
# with the report lines the command journals in that mode and not with the run banner — the first byte is the comment
# slash of the "Code generated" header
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:wiring:generate --package generated --function RegisterGeneratedServices 2>/dev/null | head -c 1 | od -An -c | tr -d ' '"
if [[ "$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d '[:space:]')" == "/" ]]; then
    check_pass "melody:wiring:generate on stdout begins with the generated source, not with the report"
else
    check_fail "melody:wiring:generate on stdout does not begin with the generated source (first byte: ${RUN_IN_DEV_OUTPUT_STRING})"
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

# the documented stdout mode is a redirection into a file a parser reads: the first byte of the stream has to be the
# document's, not the run banner's escape sequence — the command declares --quiet defaulting to true for exactly this
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:openapi:generate 2>/dev/null | head -c 1 | od -An -c | tr -d ' '"
if [[ "$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d '[:space:]')" == "{" ]]; then
    check_pass "melody:openapi:generate on stdout begins with the document, not with the run banner"
else
    check_fail "melody:openapi:generate on stdout does not begin with the document (first byte: ${RUN_IN_DEV_OUTPUT_STRING})"
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

# the dsn assembled out of the MYSQL_* keys is gone: it had no consumer, and its template failed the boot the
# moment a MYSQL_* line was removed from .env — the unwiring the readme prescribes. The dump above is known
# non-empty (the password entry was found in it), so an absent dsn entry is a measurement, not a tautology
DSN_ENTRY_STRING="$(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"app.database.dsn"[^}]*' | head -1 || true)"
if [[ "" = "${DSN_ENTRY_STRING}" ]]; then
    check_pass "no app.database.dsn parameter is registered any more (the dead template that failed the unwired boot)"
else
    check_fail "the app.database.dsn parameter is back: ${DSN_ENTRY_STRING}"
fi

# and no parameter assembles a connection string out of the integration keys readable: the shape the dead dsn
# had, tcp(host:port)/database, must not appear in any value of the dump
if printf '%s' "${SECRETS_JSON_STRING}" | grep -q 'tcp('; then
    check_fail "a parameter assembles a readable connection string: $(printf '%s' "${SECRETS_JSON_STRING}" | grep -o '"name":"[^"]*"[^}]*tcp([^}]*' | head -1)"
else
    check_pass "no parameter assembles a readable connection string out of the integration keys"
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

# the empty-string fallback is asserted on a key this run BLANKS rather than on one that happened to be
# absent from the committed .env. It used to read app.reporting.export_endpoint straight, which passed only
# because nothing had ever configured an export endpoint — a precondition the check did not state and could
# not defend, and which the first deployment to set the key would have broken. Blanking it here states it:
# what is under test is that %env(default::KEY)% answers "" for a key with no value, and the parameter is
# only the vehicle.
docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'APP_REPORTING_EXPORT_ENDPOINT=\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:parameters --format json 2>/dev/null"
OPTIONAL_BLANKED_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"

EXPORT_ENDPOINT_ENTRY_STRING="$(printf '%s' "${OPTIONAL_BLANKED_JSON_STRING}" | grep -o '"name":"app.reporting.export_endpoint"[^}]*' | head -1 || true)"
if printf '%s' "${EXPORT_ENDPOINT_ENTRY_STRING}" | grep -q '"value":""'; then
    check_pass "the empty-string fallback resolves to an empty value for a blanked key"
else
    check_fail "the empty fallback did not resolve to an empty value: ${EXPORT_ENDPOINT_ENTRY_STRING:-<entry missing>}"
fi

restore_example_env_local

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
# V3 BOOT CONFIGURATION — the runtime mode flag, the .env artifacts and their grammar, read from a built binary
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 BOOT CONFIGURATION" "${TAG_VALIDATE}" "e2e"

# one binary built into its own directory beside a copy of the example's .env (the shape of the signal section),
# so every arm below states its configuration in files it writes itself and removes before the next arm. The
# catalogue title is the vehicle for the file precedence and the percent grammar because app:info prints it
# (catalog_report), so the value read back is the one the booted application resolved, not a listing of the
# parameter table. The http arm runs on 18085 (18080 is the signal section's, 18084 the teardown section's).
# --mode=http is driven over MELODY_DEFAULT_MODE=cli, where the bare binary prints its usage and exits: only
# the flag, not the configured default, can explain a process that then serves /health.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-boot-e2e
    EMPTY_DIRECTORY=/tmp/example-boot-empty-e2e
    rm -rf \"\${WORK_DIRECTORY}\" \"\${EMPTY_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\" \"\${EMPTY_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-boot\" . >/tmp/example-boot-build.log 2>&1; then
        echo build_failed=1
        cat /tmp/example-boot-build.log
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cp -r public \"\${WORK_DIRECTORY}/public\"
    cp \"\${WORK_DIRECTORY}/example-boot\" \"\${EMPTY_DIRECTORY}/example-boot\"
    cp -r public \"\${EMPTY_DIRECTORY}/public\"
    : > \"\${EMPTY_DIRECTORY}/.env\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    ./example-boot --mode=cli app:info >/tmp/example-boot.log 2>&1
    echo \"mode_cli_exit=\$?\"
    ./example-boot --mode=bogus app:info >/tmp/example-boot.log 2>&1
    echo \"mode_bogus_exit=\$?\"
    if grep -q 'invalid mode: --mode is a runtime flag' /tmp/example-boot.log; then echo mode_bogus_named=1; else echo mode_bogus_named=0; fi
    printf 'MELODY_DEFAULT_MODE=cli\nMELODY_HTTP_ADDRESS=:18085\nAPP_JWT_SECRET=stack-chosen-signing-secret-of-its-own\n' > .env.local
    ./example-boot >/tmp/example-boot.log 2>&1
    echo \"default_cli_exit=\$?\"
    ./example-boot --mode=http >/tmp/example-boot-http.log 2>&1 &
    APP_PID=\$!
    READY=0
    for _ in \$(seq 1 150); do
        if wget -q -O /dev/null http://127.0.0.1:18085/health 2>/dev/null; then
            READY=1
            break
        fi
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    echo \"mode_http_ready=\${READY}\"
    OWN_TOKEN=\$(./example-boot auth:token --user user-1 --role ROLE_USER 2>/dev/null | grep -oE '[A-Za-z0-9_-]+[.][A-Za-z0-9_-]+[.][A-Za-z0-9_-]+' | tail -1)
    COMMITTED_TOKEN=\$(\"\${EMPTY_DIRECTORY}/example-boot\" auth:token --user user-1 --role ROLE_USER 2>/dev/null | grep -oE '[A-Za-z0-9_-]+[.][A-Za-z0-9_-]+[.][A-Za-z0-9_-]+' | tail -1)
    OWN_STATUS=\$(wget -q -S -O /dev/null --header=\"Authorization: Bearer \${OWN_TOKEN}\" http://127.0.0.1:18085/secure/me/ 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)
    COMMITTED_STATUS=\$(wget -q -S -O /dev/null --header=\"Authorization: Bearer \${COMMITTED_TOKEN}\" http://127.0.0.1:18085/secure/me/ 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)
    echo \"jwt_own_status=\${OWN_STATUS}\"
    echo \"jwt_committed_status=\${COMMITTED_STATUS}\"
    kill -INT \${APP_PID} 2>/dev/null || true
    for _ in \$(seq 1 150); do
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    kill -KILL \${APP_PID} 2>/dev/null || true
    wait \${APP_PID} 2>/dev/null || true
    rm -f .env.local
    printf 'APP_CATALOG_TITLE=FromEnvDev\n' > .env.dev
    ./example-boot app:info 2>/dev/null | grep -o 'catalog_report: [^:]*' | sed 's/^/env_dev_/'
    printf 'APP_CATALOG_TITLE=FromEnvDevLocal\n' > .env.dev.local
    ./example-boot app:info 2>/dev/null | grep -o 'catalog_report: [^:]*' | sed 's/^/env_dev_local_/'
    rm -f .env.dev .env.dev.local
    printf 'MELODY_ENV=\n' > .env.local
    ./example-boot app:info >/tmp/example-boot.log 2>&1
    echo \"empty_env_name_exit=\$?\"
    if grep -q 'environment may not be empty' /tmp/example-boot.log; then echo empty_env_name_named=1; else echo empty_env_name_named=0; fi
    printf 'APP_CATALOG_TITLE=50%%%%\n' > .env.local
    ./example-boot app:info 2>/dev/null | grep -o 'catalog_report: [^:]*' | sed 's/^/percent_/'
    printf 'APP_CATALOG_TITLE=%%env(A))%%\n' > .env.local
    ./example-boot app:info >/tmp/example-boot.log 2>&1
    echo \"malformed_exit=\$?\"
    if grep -q 'malformed environment placeholder' /tmp/example-boot.log; then echo malformed_named=1; else echo malformed_named=0; fi
    rm -f .env.local
    rm -f var/log/dev.log
    MYSQL_HOST=nope ./example-boot debug:parameters --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"name\":\"MYSQL_HOST\"[^}]*' | head -1 | sed 's/^/process_env_parameter=/'
    if grep -q 'process environment variable is ignored.*\"environmentVariable\":\"MYSQL_HOST\"' var/log/dev.log 2>/dev/null; then echo process_env_warned=1; else echo process_env_warned=0; fi
    printf 'MELODY_ENV=prod\n' > .env.local
    ./example-boot debug:router --limit=1 >/tmp/example-boot.log 2>&1
    echo \"prod_committed_encrypt_exit=\$?\"
    if grep -q 'APP_ENCRYPT_KEYS holds the development value' /tmp/example-boot.log; then echo prod_committed_encrypt_named=1; else echo prod_committed_encrypt_named=0; fi
    if grep -q 'melody-example-cipher-key' /tmp/example-boot.log; then echo prod_committed_encrypt_leaked=1; else echo prod_committed_encrypt_leaked=0; fi
    printf 'MELODY_ENV=prod\nAPP_ENCRYPT_KEYS=stack-1:stack-chosen-production-key-32by\nAPP_ENCRYPT_CURRENT_KEY=stack-1\n' > .env.local
    ./example-boot debug:router --limit=1 >/tmp/example-boot.log 2>&1
    echo \"prod_committed_jwt_exit=\$?\"
    if grep -q 'APP_JWT_SECRET holds the development value' /tmp/example-boot.log; then echo prod_committed_jwt_named=1; else echo prod_committed_jwt_named=0; fi
    if grep -q 'melody-example-signing-secret' /tmp/example-boot.log; then echo prod_committed_jwt_leaked=1; else echo prod_committed_jwt_leaked=0; fi
    printf 'MELODY_ENV=prod\nAPP_ENCRYPT_KEYS=stack-1:stack-chosen-production-key-32by\nAPP_ENCRYPT_CURRENT_KEY=stack-1\nAPP_JWT_SECRET=stack-chosen-signing-secret-of-its-own\nAPP_INTERNAL_AUTH_SECRET=stack-chosen-internal-secret-of-its-own\n' > .env.local
    ./example-boot debug:container >/tmp/example-boot.log 2>&1
    echo \"prod_debug_container_exit=\$?\"
    if grep -q 'cli command not found' /tmp/example-boot.log; then echo prod_debug_container_named=1; else echo prod_debug_container_named=0; fi
    ./example-boot debug:router --limit=1 >/tmp/example-boot.log 2>&1
    echo \"prod_debug_router_exit=\$?\"
    printf 'MYSQL_DATABASE=nope\n' > .env.local
    timeout 60 ./example-boot product:list >/tmp/example-boot.log 2>&1
    echo \"missing_database_exit=\$?\"
    if grep -q 'the catalogue database at mysql:3306/nope could not be opened' /tmp/example-boot.log; then echo missing_database_emergency=1; else echo missing_database_emergency=0; fi
    if grep -q '^melody: exiting with code 1 after unrecovered error: the catalogue database' /tmp/example-boot.log; then echo missing_database_certified=1; else echo missing_database_certified=0; fi
    if grep -q 'goroutine 1 \[running\]' /tmp/example-boot.log; then echo missing_database_dumped=1; else echo missing_database_dumped=0; fi
    rm -f .env.local
    printf 'PGSQL_DATABASE=nope\nMELODY_HTTP_ADDRESS=:18085\n' > .env.local
    rm -rf var/log
    ./example-boot --mode=http >/tmp/example-boot-http.log 2>&1 &
    APP_PID=\$!
    READY=0
    for _ in \$(seq 1 150); do
        if wget -q -O /dev/null http://127.0.0.1:18085/health 2>/dev/null; then
            READY=1
            break
        fi
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    echo \"archive_http_ready=\${READY}\"
    COOKIE=\$(wget -q -S -O /dev/null --header='Content-Type: application/json' --post-data='{\"username\":\"editor\",\"password\":\"editor\"}' --header='Accept: application/json' http://127.0.0.1:18085/login/ 2>&1 | sed -n 's/^ *Set-Cookie: *\([^;]*\).*/\1/p' | head -1)
    wget -q -O /dev/null --header \"Cookie: \${COOKIE}\" --header 'Accept: application/json' --header 'X-Request-Id: claimed-by-the-e2e-client' http://127.0.0.1:18085/reports/api/history/ 2>/dev/null
    SESSION_EXPIRES_AT=\$(grep -o '\"expiresAt\":[0-9]*' var/session/session.json 2>/dev/null | head -1 | cut -d: -f2)
    if [ -n \"\${SESSION_EXPIRES_AT}\" ]; then echo \"session_seconds_left=\$(( \${SESSION_EXPIRES_AT:0:10} - \$(date +%s) ))\"; else echo session_seconds_left=none; fi
    echo \"unbounded_ttl_warnings=\$(grep -c 'unbounded session ttl' var/log/dev.log)\"
    ACCESS_LINE=\$(grep '\"message\":\"request completed\"' var/log/dev.log | grep '\"path\":\"/reports/api/history/\"' | tail -1)
    ACCESS_ID=\$(printf '%s' \"\${ACCESS_LINE}\" | grep -o '\"requestId\":\"[^\"]*\"' | head -1)
    if printf '%s' \"\${ACCESS_LINE}\" | grep -q '\"statusCode\":500'; then echo archive_answered_500=1; else echo archive_answered_500=0; fi
    echo \"archive_error_records=\$(grep '\"message\":\"handler answered a server error\"' var/log/dev.log | grep -c \"\${ACCESS_ID:-no-access-line}\")\"
    if grep '\"message\":\"handler answered a server error\"' var/log/dev.log | grep -q 'causeChain.*database ..nope.. does not exist'; then echo archive_cause_named=1; else echo archive_cause_named=0; fi
    echo \"archive_claim_journaled=\$(grep -c 'claimed-by-the-e2e-client' var/log/dev.log)\"
    mv var/log/dev.log var/log/dev.log.1
    ROTATED_SIZE=\$(wc -c < var/log/dev.log.1)
    kill -HUP \${APP_PID}
    for _ in \$(seq 1 25); do
        wget -q -O /dev/null http://127.0.0.1:18085/health 2>/dev/null
        if [ -s var/log/dev.log ]; then
            break
        fi
        sleep 0.2
    done
    if [ -s var/log/dev.log ]; then echo hup_reopened=1; else echo hup_reopened=0; fi
    if [ \"\${ROTATED_SIZE}\" = \"\$(wc -c < var/log/dev.log.1)\" ]; then echo hup_rotated_unchanged=1; else echo hup_rotated_unchanged=0; fi
    if kill -0 \${APP_PID} 2>/dev/null; then echo hup_alive=1; else echo hup_alive=0; fi
    kill -INT \${APP_PID} 2>/dev/null || true
    for _ in \$(seq 1 150); do
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    kill -KILL \${APP_PID} 2>/dev/null || true
    wait \${APP_PID} 2>/dev/null || true
    rm -f .env.local
    printf 'REDIS_ADDRESS=\nMELODY_HTTP_ADDRESS=:18085\n' > .env.local
    ./example-boot --mode=http >/tmp/example-boot-http.log 2>&1 &
    APP_PID=\$!
    READY=0
    for _ in \$(seq 1 150); do
        if wget -q -O /dev/null http://127.0.0.1:18085/health 2>/dev/null; then
            READY=1
            break
        fi
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    echo \"no_redis_ready=\${READY}\"
    REFUSED_COUNT=0
    LAST_STATUS=none
    for _ in \$(seq 1 31); do
        LAST_STATUS=\$(wget -q -S -O /dev/null --header='Content-Type: application/json' --post-data='{\"username\":\"user\",\"password\":\"wrong\"}' --header='Accept: application/json' http://127.0.0.1:18085/login/ 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | tail -1)
        if [ \"401\" = \"\${LAST_STATUS}\" ]; then REFUSED_COUNT=\$((REFUSED_COUNT + 1)); fi
    done
    echo \"no_redis_login_refused=\${REFUSED_COUNT}\"
    echo \"no_redis_login_last=\${LAST_STATUS}\"
    kill -INT \${APP_PID} 2>/dev/null || true
    for _ in \$(seq 1 150); do
        if ! kill -0 \${APP_PID} 2>/dev/null; then
            break
        fi
        sleep 0.2
    done
    kill -KILL \${APP_PID} 2>/dev/null || true
    wait \${APP_PID} 2>/dev/null || true
    rm -f .env.local
    cd \"\${EMPTY_DIRECTORY}\" || exit 1
    ./example-boot --mode=http >/tmp/example-boot.log 2>&1
    echo \"empty_dotenv_http_exit=\$?\"
    if grep -q 'could not resolve the config parameters' /tmp/example-boot.log; then echo empty_dotenv_http_named=1; else echo empty_dotenv_http_named=0; fi
    ./example-boot app:info >/tmp/example-boot.log 2>&1
    echo \"empty_dotenv_cli_exit=\$?\"
    if grep -q 'no environment keys were loaded from the .env artifacts' /tmp/example-boot.log; then echo empty_dotenv_cli_warned=1; else echo empty_dotenv_cli_warned=0; fi
    rm -rf \"\${WORK_DIRECTORY}\" \"\${EMPTY_DIRECTORY}\" /tmp/example-boot.log /tmp/example-boot-http.log /tmp/example-boot-build.log"
BOOT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${BOOT_OUTPUT_STRING}"

boot_output_has() {
    printf '%s' "${BOOT_OUTPUT_STRING}" | grep -qx "${1}"
}

boot_output_value() {
    printf '%s' "${BOOT_OUTPUT_STRING}" | grep -o "^${1}=.*" | head -1 || true
}

if boot_output_has 'build_failed=1'; then
    check_fail "the example did not build, so the boot configuration was not exercised"
else
    if boot_output_has 'mode_cli_exit=0'; then
        check_pass "--mode=cli ahead of the command runs app:info and exits zero"
    else
        check_fail "--mode=cli app:info did not exit zero ($(boot_output_value mode_cli_exit))"
    fi

    if boot_output_has 'mode_bogus_exit=1' && boot_output_has 'mode_bogus_named=1'; then
        check_pass "--mode=bogus is refused at boot, exit 1, with the diagnostic naming the runtime flag"
    else
        check_fail "--mode=bogus was not refused with the runtime flag's diagnostic ($(boot_output_value mode_bogus_exit) $(boot_output_value mode_bogus_named))"
    fi

    # the default arm is the control: under MELODY_DEFAULT_MODE=cli the bare binary prints its usage and leaves,
    # so a /health answered by the next arm is the flag's doing
    if boot_output_has 'default_cli_exit=0' && boot_output_has 'mode_http_ready=1'; then
        check_pass "--mode=http serves /health over a configured cli default, where the bare binary exits after its usage"
    else
        check_fail "--mode=http did not serve over a cli default ($(boot_output_value default_cli_exit) $(boot_output_value mode_http_ready))"
    fi

    # the http arm runs with an APP_JWT_SECRET of its own: a bearer token minted by the empty-env binary, which
    # signs with the development value .env commits, is refused, and one minted with the configured secret is
    # served, so the token firewall verifies with the configuration and not with a constant compiled in
    if boot_output_has 'jwt_own_status=200' && boot_output_has 'jwt_committed_status=401'; then
        check_pass "with APP_JWT_SECRET of its own the token firewall serves its own token (200) and refuses one signed with the committed development secret (401)"
    else
        check_fail "the token firewall did not verify with the configured secret ($(boot_output_value jwt_own_status) $(boot_output_value jwt_committed_status))"
    fi

    if boot_output_has 'empty_dotenv_http_exit=1' && boot_output_has 'empty_dotenv_http_named=1'; then
        check_pass "an empty .env refuses the http boot, exit 1, on the config parameters it cannot resolve"
    else
        check_fail "an empty .env did not refuse the http boot on its parameters ($(boot_output_value empty_dotenv_http_exit) $(boot_output_value empty_dotenv_http_named))"
    fi

    # the refusal is not the http server's: a console command refuses on the same parameters, and says why
    if boot_output_has 'empty_dotenv_cli_exit=1' && boot_output_has 'empty_dotenv_cli_warned=1'; then
        check_pass "an empty .env refuses a console command as well, warning that no key was loaded from the .env artifacts"
    else
        check_fail "an empty .env did not refuse the console command with the no-keys warning ($(boot_output_value empty_dotenv_cli_exit) $(boot_output_value empty_dotenv_cli_warned))"
    fi

    if boot_output_has 'env_dev_catalog_report: FromEnvDev'; then
        check_pass ".env.dev overrides .env under MELODY_ENV=dev (catalog_report FromEnvDev)"
    else
        check_fail ".env.dev did not override .env ($(boot_output_value env_dev_catalog_report))"
    fi

    if boot_output_has 'env_dev_local_catalog_report: FromEnvDevLocal'; then
        check_pass ".env.dev.local overrides .env.dev (catalog_report FromEnvDevLocal)"
    else
        check_fail ".env.dev.local did not override .env.dev ($(boot_output_value env_dev_local_catalog_report))"
    fi

    if boot_output_has 'empty_env_name_exit=1' && boot_output_has 'empty_env_name_named=1'; then
        check_pass "an empty MELODY_ENV is refused at boot, naming the empty environment"
    else
        check_fail "an empty MELODY_ENV was not refused ($(boot_output_value empty_env_name_exit) $(boot_output_value empty_env_name_named))"
    fi

    if boot_output_has 'percent_catalog_report: 50%'; then
        check_pass "%% in a .env value resolves to one literal percent sign (catalog_report 50%)"
    else
        check_fail "%% did not resolve to a literal percent sign ($(boot_output_value percent_catalog_report))"
    fi

    if boot_output_has 'malformed_exit=1' && boot_output_has 'malformed_named=1'; then
        check_pass "a malformed %env( placeholder refuses the boot, naming the malformed placeholder"
    else
        check_fail "a malformed %env( placeholder was not refused ($(boot_output_value malformed_exit) $(boot_output_value malformed_named))"
    fi

    # the warning alone would also appear if the process value had been APPLIED and then reported, so the value the
    # parameter table holds is read beside it
    if boot_output_has 'process_env_warned=1'; then
        check_pass "a process environment variable shadowing a .env key is logged as ignored, naming MYSQL_HOST"
    else
        check_fail "the ignored process environment variable was not logged ($(boot_output_value process_env_warned))"
    fi

    if printf '%s' "$(boot_output_value process_env_parameter)" | grep -q '"value":"mysql"'; then
        check_pass "MYSQL_HOST keeps the .env value (mysql) while the process environment says nope"
    else
        check_fail "MYSQL_HOST did not keep the .env value ($(boot_output_value process_env_parameter))"
    fi

    # outside development the credentials .env commits are public, so a production boot holding one refuses by the
    # key's name before anything is built, and the refusal carries no byte of the value; the encrypt keys are read
    # first, the signing secret after them, and the arm below with values of its own is the one that boots
    if boot_output_has 'prod_committed_encrypt_exit=1' && boot_output_has 'prod_committed_encrypt_named=1' && boot_output_has 'prod_committed_encrypt_leaked=0' && boot_output_has 'prod_committed_jwt_exit=1' && boot_output_has 'prod_committed_jwt_named=1' && boot_output_has 'prod_committed_jwt_leaked=0'; then
        check_pass "under MELODY_ENV=prod a committed APP_ENCRYPT_KEYS and then a committed APP_JWT_SECRET refuse the boot by name (exit 1), with no byte of the value in the record"
    else
        check_fail "a production boot did not refuse the committed credentials by name ($(boot_output_value prod_committed_encrypt_exit) $(boot_output_value prod_committed_encrypt_named) $(boot_output_value prod_committed_encrypt_leaked) $(boot_output_value prod_committed_jwt_exit) $(boot_output_value prod_committed_jwt_named) $(boot_output_value prod_committed_jwt_leaked))"
    fi

    # the debug family builds services and prints parameters, so outside development it is not registered at all,
    # while debug:router, the one that reads nothing but the route table, stays: the second arm is what makes the
    # first a filter on the environment rather than a broken binary
    if boot_output_has 'prod_debug_container_exit=2' && boot_output_has 'prod_debug_container_named=1' && boot_output_has 'prod_debug_router_exit=0'; then
        check_pass "under MELODY_ENV=prod debug:container is not a command (exit 2) while debug:router still answers"
    else
        check_fail "the debug family was not filtered outside development ($(boot_output_value prod_debug_container_exit) $(boot_output_value prod_debug_container_named) $(boot_output_value prod_debug_router_exit))"
    fi

    # the example opens its catalogue at the first resolution of its handle, so a catalogue that cannot be opened is
    # refused by the first command that reads it: the refusal names the database and its location and ends in the exit
    # line of a refusal inside Run, with exit 1, rather than a goroutine dump and exit 2
    if boot_output_has 'missing_database_exit=1' && boot_output_has 'missing_database_emergency=1' && boot_output_has 'missing_database_certified=1' && boot_output_has 'missing_database_dumped=0'; then
        check_pass "a catalogue that cannot be opened refuses the first command that reads it with exit 1, naming the database and its location, and the exit line, no goroutine dump"
    else
        check_fail "the refused catalogue did not end in a recorded exit ($(boot_output_value missing_database_exit) $(boot_output_value missing_database_emergency) $(boot_output_value missing_database_certified) $(boot_output_value missing_database_dumped))"
    fi

    # a 500 a door answers through the presenter is journaled by the presenter and marked logged, so the kernel files no
    # second record: the one record is read by the request id of the access line that answered 500, and its chain
    # carries the driver's refusal of the missing archive database
    if boot_output_has 'archive_http_ready=1' && boot_output_has 'archive_answered_500=1' && boot_output_has 'archive_error_records=1' && boot_output_has 'archive_cause_named=1'; then
        check_pass "an archive that cannot be opened answers 500 with exactly one server error record, its cause chain naming the missing database"
    else
        check_fail "the 500 was not journaled once with its cause ($(boot_output_value archive_http_ready) $(boot_output_value archive_answered_500) $(boot_output_value archive_error_records) $(boot_output_value archive_cause_named))"
    fi

    # the request carried an X-Request-Id of the client's own; the journal ties the records by the id the kernel minted,
    # so the claim appears in none of them
    if boot_output_has 'archive_answered_500=1' && boot_output_has 'archive_claim_journaled=0'; then
        check_pass "the journal carries the minted request id, never the X-Request-Id the client claimed"
    else
        check_fail "the client's claimed request id reached the journal ($(boot_output_value archive_claim_journaled))"
    fi

    # rename-based rotation: after the rename the process still writes the old descriptor, SIGHUP makes the serving
    # process reopen its path, and the renamed file stops growing while the fresh one takes the next request
    if boot_output_has 'hup_reopened=1' && boot_output_has 'hup_rotated_unchanged=1' && boot_output_has 'hup_alive=1'; then
        check_pass "SIGHUP reopens the journal after a rename: a fresh file takes the next request, the renamed one stops growing, the process lives"
    else
        check_fail "SIGHUP did not reopen the journal ($(boot_output_value hup_reopened) $(boot_output_value hup_rotated_unchanged) $(boot_output_value hup_alive))"
    fi

    # the sessions of the served process are kept in var/session/session.json and expire a day after their last
    # request: the file the sign-in above wrote is read out of band, its entry's expiry a day away, and the boot journal
    # carries no warning of an unbounded in-memory storage
    SESSION_SECONDS_LEFT_STRING="$(boot_output_value session_seconds_left | sed 's/^session_seconds_left=//')"
    if [[ "${SESSION_SECONDS_LEFT_STRING}" =~ ^[0-9]+$ ]] && [[ 86000 -lt "${SESSION_SECONDS_LEFT_STRING}" ]] && [[ 86401 -gt "${SESSION_SECONDS_LEFT_STRING}" ]] && boot_output_has 'unbounded_ttl_warnings=0'; then
        check_pass "the signed-in session is kept in var/session/session.json and expires a day after its request (${SESSION_SECONDS_LEFT_STRING}s left), with no unbounded-ttl warning at boot"
    else
        check_fail "the session file does not carry the day's expiry ($(boot_output_value session_seconds_left) $(boot_output_value unbounded_ttl_warnings))"
    fi

    # without redis the write budget is counted in the process itself: thirty refused sign-ins from one address
    # are answered 401 and the thirty-first 429, where the budget used to be absent and every guess reached the door
    if boot_output_has 'no_redis_ready=1' && boot_output_has 'no_redis_login_refused=30' && boot_output_has 'no_redis_login_last=429'; then
        check_pass "without redis the sign-in is throttled in process: thirty wrong passwords answered 401 and the thirty-first 429"
    else
        check_fail "the sign-in is not throttled without redis ($(boot_output_value no_redis_ready) $(boot_output_value no_redis_login_refused) $(boot_output_value no_redis_login_last))"
    fi
fi

check_section_end "V3 BOOT CONFIGURATION" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 LOGIN FAILURE JOURNAL — a refused password on the supervised example reaches the security journal
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 LOGIN FAILURE JOURNAL" "${TAG_VALIDATE}" "e2e"

# the login door authenticates the credentials itself, so the security.login.failure event is its own to raise; the
# journal line is read out of band, from the supervised example's own log. The request carries a user agent no other
# client sends, which the access log records beside the request id: that id, read from the access line, is what ties
# the failure line to this request and not to one an earlier run left behind. The log is read by its last lines, since
# it outgrows the byte offsets busybox tail accepts, and the failure line must name neither credential that was tried.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "LOG_PATH=var/log/dev.log
    USER_AGENT=e2e-login-failure-\$\$-\$(date +%s%N)
    wget -q -O /dev/null -U \"\${USER_AGENT}\" --header 'Accept: application/json' --header 'Content-Type: application/json' --post-data '{\"username\":\"e2e-login-failure-probe\",\"password\":\"e2e-wrong-password\"}' http://127.0.0.1:8080/login 2>/tmp/example-login-failure.log
    grep -o 'HTTP/[0-9.]* [0-9]*' /tmp/example-login-failure.log | tail -1 | sed 's/^.* /response_status=/'
    sleep 1
    REQUEST_ID=\$(tail -n 4000 \"\${LOG_PATH}\" | grep '\"message\":\"request completed\"' | grep \"\\\"userAgent\\\":\\\"\${USER_AGENT}\\\"\" | grep -o '\"requestId\":\"[^\"]*\"' | tail -1 | sed 's/^\"requestId\":\"//; s/\"\$//')
    echo \"access_request_id=\${REQUEST_ID}\"
    if [ -n \"\${REQUEST_ID}\" ]; then
        tail -n 4000 \"\${LOG_PATH}\" | grep '\"message\":\"security login failure\"' | grep \"\${REQUEST_ID}\" | tail -1 | sed 's/^/journal_line=/'
    fi
    rm -f /tmp/example-login-failure.log"
LOGIN_FAILURE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

printf '%s\n' "${LOGIN_FAILURE_OUTPUT_STRING}"

LOGIN_FAILURE_REQUEST_ID_STRING="$(printf '%s' "${LOGIN_FAILURE_OUTPUT_STRING}" | grep -o '^access_request_id=.*' | head -1 | sed 's/^access_request_id=//' || true)"
LOGIN_FAILURE_JOURNAL_LINE_STRING="$(printf '%s' "${LOGIN_FAILURE_OUTPUT_STRING}" | grep -o '^journal_line=.*' | head -1 | sed 's/^journal_line=//' || true)"

if printf '%s' "${LOGIN_FAILURE_OUTPUT_STRING}" | grep -qx 'response_status=401' && [[ "" != "${LOGIN_FAILURE_REQUEST_ID_STRING}" ]] && [[ "" != "${LOGIN_FAILURE_JOURNAL_LINE_STRING}" ]]; then
    check_pass "a refused password on /login answered 401 and wrote a security login failure line under the request id its access line carries"
else
    check_fail "a refused password left no security login failure line for its request ($(printf '%s' "${LOGIN_FAILURE_OUTPUT_STRING}" | tr '\n' ' '))"
fi

if [[ "" != "${LOGIN_FAILURE_JOURNAL_LINE_STRING}" ]] \
    && printf '%s' "${LOGIN_FAILURE_JOURNAL_LINE_STRING}" | grep -q '"path":"/login"' \
    && ! printf '%s' "${LOGIN_FAILURE_JOURNAL_LINE_STRING}" | grep -q 'e2e-wrong-password\|e2e-login-failure-probe'; then
    check_pass "the journal line names the login path and neither credential that was tried"
else
    check_fail "the journal line does not name the login path or names a credential (${LOGIN_FAILURE_JOURNAL_LINE_STRING:-<none>})"
fi

check_section_end "V3 LOGIN FAILURE JOURNAL" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 DEBUG COMMANDS — the dev-registered family answers from the v3 example, and the two commands that
# used to build in order to list now describe by default. The split is asserted on STATE — the state
# column, the scoped block, and the two exit codes — not on the command's own word about itself.
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:version 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'MELODY: v'; then
    check_pass "v3 debug:version reports the framework version"
else
    check_fail "v3 debug:version did not report the framework version (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the application declares its own version, set by the build through -ldflags: a build that sets one reports it in the
# meta of every document and in debug:version's application row, a build that sets none reports dev, and the melody row
# stays the framework's
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run -ldflags '-X main.applicationVersion=e2e-2026.09.29' . debug:version --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"version\":{\"application\":\"[^\"]*\",\"melody\":\"[^\"]*\"' | head -1 | sed 's/^/stamped=/'
    go run . debug:version --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"version\":{\"application\":\"[^\"]*\"' | head -1 | sed 's/^/unstamped=/'"
V3_APPLICATION_VERSION_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_APPLICATION_VERSION_STRING}" | grep -qx 'stamped="version":{"application":"e2e-2026.09.29","melody":"v3[^"]*"' \
    && printf '%s' "${V3_APPLICATION_VERSION_STRING}" | grep -qx 'unstamped="version":{"application":"dev"'; then
    check_pass "the example reports the version its build stamped through -ldflags, and dev when none was stamped, beside melody's"
else
    check_fail "the example's own version did not follow its build (${V3_APPLICATION_VERSION_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:events --format=json 2>/dev/null"
V3_EVENTS_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
if printf '%s' "${V3_EVENTS_JSON_STRING}" | grep -q '"command":"debug:events"'; then
    check_pass "v3 debug:events answers its json envelope"
else
    check_fail "v3 debug:events did not answer its envelope (${V3_EVENTS_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:middleware 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_MIDDLEWARE_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_MIDDLEWARE_OUTPUT_STRING}" | grep -q 'static'; then
    check_pass "v3 debug:middleware lists the static middleware of the example's pipeline"
else
    check_fail "v3 debug:middleware did not list the pipeline (${V3_MIDDLEWARE_OUTPUT_STRING:-<empty>})"
fi

# the describing listing carries what only the description channel knows: the definition name, the
# priority the ordering used and the status of each entry. The build channel hands back the built chain
# alone, so a listing produced by building could name none of the three
if printf '%s' "${V3_MIDDLEWARE_OUTPUT_STRING}" | grep -q 'priority' && printf '%s' "${V3_MIDDLEWARE_OUTPUT_STRING}" | grep -q 'reason'; then
    check_pass "v3 debug:middleware describes the pipeline rather than building it"
else
    check_fail "v3 debug:middleware did not carry the description columns (${V3_MIDDLEWARE_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --limit=0 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_CONTAINER_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_CONTAINER_OUTPUT_STRING}" | grep -q 'service.example.product.repository'; then
    check_pass "v3 debug:container lists the example's registered services"
else
    check_fail "v3 debug:container did not list the example services (${V3_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

# the scoped block is the half the pre-repair listing could not reach at all: it walked Names(), which
# does not see scoped registrations, so a scoped definition was invisible to the command that exists to
# show what the container holds
if printf '%s' "${V3_CONTAINER_OUTPUT_STRING}" | grep -q 'service-example-reporting-request-trail'; then
    check_pass "v3 debug:container lists the request-scoped report trail"
else
    check_fail "v3 debug:container does not list the scoped report trail (${V3_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

# state, not the command's word: a service the boot never resolved reads registered, which it can only
# do if nothing ran its provider. The old listing called Get on every name, so every row would read built
if printf '%s' "${V3_CONTAINER_OUTPUT_STRING}" | grep -q 'service.example.product.repository *| registered'; then
    check_pass "v3 debug:container ran no provider for the listing"
else
    check_fail "v3 debug:container built a service it only had to list (${V3_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

# the deployment gate, end to end: the bare listing succeeds where the sweep refuses. The scoped report
# trail depends on the request context, which a console process has no scope carrying — so --build has a
# real failure to report here, and that is what makes the two exit codes a measurement rather than a pair
# of zeroes
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --limit=0 >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v3 debug:container exits zero when it only describes"
else
    check_fail "v3 debug:container did not exit zero while describing (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --build --limit=0 --format=json 2>/dev/null"
V3_CONTAINER_BUILD_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
if printf '%s' "${V3_CONTAINER_BUILD_JSON_STRING}" | grep -q '"code":"debug.buildFailed"' && printf '%s' "${V3_CONTAINER_BUILD_JSON_STRING}" | grep -q 'service-example-reporting-request-trail'; then
    check_pass "v3 debug:container --build reports its failures on the envelope, naming them"
else
    check_fail "v3 debug:container --build did not report the sweep failure (${V3_CONTAINER_BUILD_JSON_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --build --limit=0 >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=1'; then
    check_pass "v3 debug:container --build fails a deployment gate on a service that does not resolve"
else
    check_fail "v3 debug:container --build did not fail the gate (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the two discriminators the dispatch breaks ties on: without them two overlapping routes render
# identically and the command cannot answer which one responds
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:router --limit=3 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'priority' && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'order'; then
    check_pass "v3 debug:router reports the priority and the registration order"
else
    check_fail "v3 debug:router did not report the discriminators (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the example arms the parallel teardown in main.go, so the plan the listing prints is the one a stopping process
# walks: the block names its waves, and the one edge the example declares — the notification hub, which logs its
# own close, closes before the logger — is read on both ends of the edge, with the ordering the plan proved for it
if printf '%s' "${V3_CONTAINER_OUTPUT_STRING}" | grep -q 'TEARDOWN (DEPENDENCY WAVES)'; then
    check_pass "v3 debug:container prints the armed teardown plan in dependency waves"
else
    check_fail "v3 debug:container printed no teardown plan in waves (${V3_CONTAINER_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container --limit=0 --format=json 2>/dev/null"
V3_CONTAINER_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
V3_HUB_TEARDOWN_STRING="$(printf '%s' "${V3_CONTAINER_JSON_STRING}" | grep -o '"name":"service.example.catalog.notification.hub"[^}]*}[^}]*}' | head -1 || true)"
V3_LOGGER_TEARDOWN_STRING="$(printf '%s' "${V3_CONTAINER_JSON_STRING}" | grep -o '"name":"service.logger"[^}]*}[^}]*}' | head -1 || true)"
if printf '%s' "${V3_HUB_TEARDOWN_STRING}" | grep -q '"wave":0,"node":"service:service.example.catalog.notification.hub","closedBefore":\["service:service.logger"\]' \
    && printf '%s' "${V3_HUB_TEARDOWN_STRING}" | grep -q '"ordering":"proved"' \
    && printf '%s' "${V3_LOGGER_TEARDOWN_STRING}" | grep -q '"wave":1,' \
    && printf '%s' "${V3_LOGGER_TEARDOWN_STRING}" | grep -q '"closedAfter":\["service:service.example.catalog.notification.hub"\]'; then
    check_pass "v3 teardown plan: the notification hub closes in wave 0 before the logger in wave 1, the edge proved on both ends"
else
    check_fail "v3 teardown plan does not order the hub before the logger (hub ${V3_HUB_TEARDOWN_STRING:-<missing>}; logger ${V3_LOGGER_TEARDOWN_STRING:-<missing>})"
fi

# the order the example registers its middlewares in is the order they wrap the handler: the metrics outermost,
# then the compression of the answer, then the timing that reports the duration, then the journal flush the timing
# measures. The json listing names each
# by its constructor, so the positions are read off the one document
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:middleware --format=json 2>/dev/null"
V3_MIDDLEWARE_ORDER_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t' | grep -o 'NewMetricsMiddleware\|CompressionMiddleware\|NewTimingMiddleware\|NewCatalogJournalFlushMiddleware\|NewTracingMiddleware' | tr '\n' ',' || true)"
if [[ "NewMetricsMiddleware,CompressionMiddleware,NewTimingMiddleware,NewCatalogJournalFlushMiddleware,NewTracingMiddleware," == "${V3_MIDDLEWARE_ORDER_STRING}" ]]; then
    check_pass "v3 debug:middleware lists metrics, compression, timing, the journal flush and tracing in the order the example registers them"
else
    check_fail "v3 debug:middleware lists the example's middlewares out of order: ${V3_MIDDLEWARE_ORDER_STRING:-<none>}"
fi

# the manifest is filtered by zone: the health route is exposed in the public zone only, so the frontend export must
# leave it out while the public one carries it — the second arm is what makes the absence a filter rather than a gap
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:routes:manifest --zone frontend 2>/dev/null | grep -c '\"pattern\": \"/health\"'; go run . melody:routes:manifest --zone public 2>/dev/null | grep -c '\"pattern\": \"/health\"'"
V3_MANIFEST_COUNTS_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr '\n' ',')"
if [[ "0,1" == "${V3_MANIFEST_COUNTS_STRING%,}" ]]; then
    check_pass "melody:routes:manifest --zone frontend leaves out /health, which --zone public carries"
else
    check_fail "the manifest zones did not filter /health (frontend,public counts: ${V3_MANIFEST_COUNTS_STRING:-<none>})"
fi

# the frontend bundle is built from the committed assets/routes.json, which go generate writes from the route table; a
# door added without regenerating it is a door the bundle cannot name, so the committed file is held against a fresh
# export of the same zone
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . melody:routes:manifest --zone frontend --out /tmp/routes-fresh.json >/dev/null 2>&1; echo \"generated_exit=\$?\"
    if cmp -s /tmp/routes-fresh.json assets/routes.json; then echo manifest_current=1; else echo manifest_current=0; diff /tmp/routes-fresh.json assets/routes.json | head -8; fi
    rm -f /tmp/routes-fresh.json"
V3_MANIFEST_FRESHNESS_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_MANIFEST_FRESHNESS_STRING}" | grep -qx 'generated_exit=0' && printf '%s' "${V3_MANIFEST_FRESHNESS_STRING}" | grep -qx 'manifest_current=1'; then
    check_pass "the committed assets/routes.json is the frontend manifest the route table exports"
else
    check_fail "the committed assets/routes.json is not what melody:routes:manifest --zone frontend exports (run go generate): ${V3_MANIFEST_FRESHNESS_STRING:0:400}"
fi

# the standard flags every listing command shares, read on debug:router because its route table is fixed by the
# code: the order, the window and the two refusals the flag validators own, the pretty form of the one document, and
# the verbose table. Each arm is read against the plain ascending listing of the same process, so a flag that did
# nothing would answer the control's value
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "PATTERNS=\$(go run . debug:router --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"pattern\":\"[^\"]*\"')
    printf '%s\n' \"\${PATTERNS}\" | sed -n '1p' | sed 's/^/asc_first=/'
    printf '%s\n' \"\${PATTERNS}\" | sed -n '2p' | sed 's/^/asc_second=/'
    printf '%s\n' \"\${PATTERNS}\" | sed -n '\$p' | sed 's/^/asc_last=/'
    go run . debug:router --format=json --order=desc 2>/dev/null | tr -d ' \n\t' | grep -o '\"pattern\":\"[^\"]*\"' | head -1 | sed 's/^/desc_first=/'
    go run . debug:router --format=json --offset=1 --limit=1 2>/dev/null | tr -d ' \n\t' | grep -o '\"pattern\":\"[^\"]*\"' | tr '\n' ',' | sed 's/^/window=/'
    echo
    go run . debug:router --limit=-1 >/tmp/router-flags.log 2>&1
    echo \"negative_limit_exit=\$?\"
    if grep -q 'limit may not be negative' /tmp/router-flags.log; then echo negative_limit_named=1; else echo negative_limit_named=0; fi
    go run . debug:router --format=bogus >/tmp/router-flags.log 2>&1
    echo \"bogus_format_exit=\$?\"
    if grep -q 'unsupported output format \"bogus\"' /tmp/router-flags.log; then echo bogus_format_named=1; else echo bogus_format_named=0; fi
    go run . debug:router --format=json-pretty 2>/dev/null >/tmp/router-pretty.json
    echo \"pretty_lines=\$(wc -l </tmp/router-pretty.json)\"
    go run . debug:router --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"data\":.*' | md5sum | cut -c1-32 | sed 's/^/json_data=/'
    tr -d ' \n\t' </tmp/router-pretty.json | grep -o '\"data\":.*' | md5sum | cut -c1-32 | sed 's/^/pretty_data=/'
    echo \"verbose_columns=\$(go run . debug:router --limit=1 --verbose --table-width=400 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep -c '| requirements | defaults | attributes')\"
    echo \"plain_columns=\$(go run . debug:router --limit=1 --table-width=400 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep -c '| requirements')\"
    rm -f /tmp/router-flags.log /tmp/router-pretty.json"
ROUTER_FLAGS_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

router_flags_value() {
    printf '%s' "${ROUTER_FLAGS_OUTPUT_STRING}" | grep -o "^${1}=.*" | head -1 | cut -d= -f2- || true
}

if [[ -n "$(router_flags_value asc_last)" ]] && [[ "$(router_flags_value asc_first)" != "$(router_flags_value asc_last)" ]] && [[ "$(router_flags_value asc_last)" == "$(router_flags_value desc_first)" ]]; then
    check_pass "debug:router --order=desc opens on the route the ascending listing closes on ($(router_flags_value desc_first))"
else
    check_fail "debug:router --order=desc did not reverse the listing (asc $(router_flags_value asc_first)..$(router_flags_value asc_last), desc first $(router_flags_value desc_first))"
fi

if [[ -n "$(router_flags_value asc_second)" ]] && [[ "$(router_flags_value asc_second)," == "$(router_flags_value window)" ]]; then
    check_pass "debug:router --offset=1 --limit=1 answers exactly the second route of the listing"
else
    check_fail "debug:router --offset=1 --limit=1 did not answer the second route alone (second $(router_flags_value asc_second), window $(router_flags_value window))"
fi

if [[ "0" != "$(router_flags_value negative_limit_exit)" ]] && [[ "1" == "$(router_flags_value negative_limit_named)" ]] && [[ "0" != "$(router_flags_value bogus_format_exit)" ]] && [[ "1" == "$(router_flags_value bogus_format_named)" ]]; then
    check_pass "debug:router refuses --limit=-1 and --format=bogus with a non-zero exit, each naming the refused value"
else
    check_fail "the flag validators did not refuse (limit $(router_flags_value negative_limit_exit)/$(router_flags_value negative_limit_named), format $(router_flags_value bogus_format_exit)/$(router_flags_value bogus_format_named))"
fi

if [[ 1 -lt "$(router_flags_value pretty_lines)" ]] && [[ -n "$(router_flags_value json_data)" ]] && [[ "$(router_flags_value json_data)" == "$(router_flags_value pretty_data)" ]]; then
    check_pass "--format=json-pretty spreads the same document over $(router_flags_value pretty_lines) lines"
else
    check_fail "--format=json-pretty is not the json document indented ($(router_flags_value pretty_lines) lines, data $(router_flags_value json_data) vs $(router_flags_value pretty_data))"
fi

if [[ "1" == "$(router_flags_value verbose_columns)" ]] && [[ "0" == "$(router_flags_value plain_columns)" ]]; then
    check_pass "debug:router --verbose adds the requirements, defaults and attributes columns the plain table leaves out"
else
    check_fail "debug:router --verbose did not add the detail columns (verbose $(router_flags_value verbose_columns), plain $(router_flags_value plain_columns))"
fi

# the static file server answers ahead of routing and the router takes a door with or without its trailing slash, so
# every door's first segment, and every locale of a localized door, is held against MELODY_STATIC_EXCLUDED_PATHS: a
# file dropped under public/ with a door's name must never be served in place of the door. The route table is the one
# debug:router prints; an entry claims what it prefixes and, with a trailing slash, its bare spelling
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "EXCLUDED=\$(grep '^MELODY_STATIC_EXCLUDED_PATHS=' .env | cut -d= -f2-)
    go run . debug:router --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"pattern\":\"[^\"]*\"[^}]*\"locales\":\"[^\"]*\"' \
        | sed 's/^\"pattern\":\"\([^\"]*\)\".*\"locales\":\"\([^\"]*\)\"$/\1 \2/' \
        | awk -v excluded=\"\${EXCLUDED}\" '
            function covered(path,    count, index_, entry, bare) {
                count = split(excluded, entries, \",\")
                for (index_ = 1; index_ <= count; index_++) {
                    entry = entries[index_]
                    if (\"\" == entry) { continue }
                    if (substr(path, 1, length(entry)) == entry) { return 1 }
                    bare = entry
                    sub(/\/+\$/, \"\", bare)
                    if (\"\" != bare && path == bare) { return 1 }
                }
                return 0
            }
            {
                routes++
                paths[1] = \$1
                pathCount = 1
                if (\$1 ~ /^\/:_locale/) {
                    pathCount = split(\$2, locales, \",\")
                    for (localeIndex = 1; localeIndex <= pathCount; localeIndex++) {
                        paths[localeIndex] = \"/\" locales[localeIndex] substr(\$1, length(\"/:_locale\") + 1)
                    }
                }
                for (pathIndex = 1; pathIndex <= pathCount; pathIndex++) {
                    if (0 == covered(paths[pathIndex])) { uncovered = uncovered paths[pathIndex] \",\" }
                }
            }
            END { print \"routes=\" routes; print \"uncovered=\" uncovered }'"
STATIC_EXCLUSION_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
STATIC_EXCLUSION_ROUTES_STRING="$(printf '%s' "${STATIC_EXCLUSION_STRING}" | grep -o '^routes=.*' | cut -d= -f2- || true)"
STATIC_EXCLUSION_UNCOVERED_STRING="$(printf '%s' "${STATIC_EXCLUSION_STRING}" | grep -o '^uncovered=.*' | cut -d= -f2- || true)"
if [[ "${STATIC_EXCLUSION_ROUTES_STRING:-0}" -gt 40 ]] && [[ -z "${STATIC_EXCLUSION_UNCOVERED_STRING}" ]] && printf '%s' "${STATIC_EXCLUSION_STRING}" | grep -qx 'uncovered='; then
    check_pass "every one of the ${STATIC_EXCLUSION_ROUTES_STRING} doors debug:router lists, each locale of the greeting expanded, is claimed by MELODY_STATIC_EXCLUDED_PATHS"
else
    check_fail "MELODY_STATIC_EXCLUDED_PATHS leaves doors a file under public/ could shadow (routes ${STATIC_EXCLUSION_ROUTES_STRING:-<none>}, uncovered ${STATIC_EXCLUSION_UNCOVERED_STRING:-<none>}): ${STATIC_EXCLUSION_STRING:0:400}"
fi

# the failure the sweep reports carries the context the resolution raised it with, redacted: the unregistered type the
# scoped trail asks for is named, and nothing of a stack or a trace is handed to the document
if printf '%s' "${V3_CONTAINER_BUILD_JSON_STRING}" | grep -qF 'serviceType\":\"*http.RequestContext' \
    && ! printf '%s' "${V3_CONTAINER_BUILD_JSON_STRING}" | grep -qE '(trace|stack|panicStack)(\\)?":'; then
    check_pass "debug:container --build names the unresolved type in the failure's context and carries no stack or trace"
else
    check_fail "the --build failure context did not name the type or carried a stack (${V3_CONTAINER_BUILD_JSON_STRING:-<empty>})"
fi

# the listing a console process prints: the example's required kernel.request listener is in the table, and the two
# security listeners, which only the http serving process registers, are named apart rather than left out
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:events --format=json --verbose 2>/dev/null"
V3_EVENTS_VERBOSE_JSON_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d '\n\t')"
if printf '%s' "${V3_EVENTS_VERBOSE_JSON_STRING}" | tr -d ' ' | grep -q '"eventName":"kernel.request","order":[0-9]*,"priority":-*[0-9]*,"required":true,[^}]*registerRequiredRequestContextListener' \
    && printf '%s' "${V3_EVENTS_VERBOSE_JSON_STRING}" | grep -o '"servingProcessListeners": *\[.*' | grep -q 'security access control listener'; then
    check_pass "debug:events lists the example's required kernel.request listener and names the access control listener as the serving process's"
else
    check_fail "debug:events did not carry the required listener and the serving process's listeners (${V3_EVENTS_VERBOSE_JSON_STRING:-<empty>})"
fi

# --build asks the chain to be constructed rather than described: every entry the example registers must build
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:middleware --build --format=json 2>/dev/null; echo \"status=\$?\""
V3_MIDDLEWARE_BUILD_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | tr -d ' \n\t')"
V3_MIDDLEWARE_BUILT_INTEGER="$(printf '%s' "${V3_MIDDLEWARE_BUILD_STRING}" | { grep -o '"status":"built"' || true; } | wc -l | tr -d ' ')"
V3_MIDDLEWARE_TOTAL_STRING="$(printf '%s' "${V3_MIDDLEWARE_BUILD_STRING}" | grep -o '"total":[0-9]*' | head -1 | cut -d: -f2 || true)"
if printf '%s' "${V3_MIDDLEWARE_BUILD_STRING}" | grep -q 'status=0$' && [[ 0 -lt "${V3_MIDDLEWARE_BUILT_INTEGER}" ]] && [[ "${V3_MIDDLEWARE_TOTAL_STRING}" == "${V3_MIDDLEWARE_BUILT_INTEGER}" ]]; then
    check_pass "debug:middleware --build exits zero with all ${V3_MIDDLEWARE_BUILT_INTEGER} entries of the chain built"
else
    check_fail "debug:middleware --build did not build the whole chain (built ${V3_MIDDLEWARE_BUILT_INTEGER} of ${V3_MIDDLEWARE_TOTAL_STRING:-?}: ${V3_MIDDLEWARE_BUILD_STRING:-<empty>})"
fi

check_section_end "V3 DEBUG COMMANDS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 DATABASE MIGRATIONS — the db:* family and the composition root run one migration over eight tables
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops all eight tables of the v3 example's own
# melody_example_v3 database — the four catalogue ones, the session index, the identifier sequence, the journal, and the two-factor enrollment table
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
# The eight are named one by one rather than matched on a prefix: the audit registry's own table is called
# melody_example_v3_audit and would be counted by any LIKE that catches the eight, while it belongs to the
# framework module that opens it and correctly survives a rollback of this set. Its survival is the check's
# quiet half — a set that had claimed it would take it down with the rest.
V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING="SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'melody_example_v3' AND table_name IN ('melody_example_v3_category', 'melody_example_v3_currency', 'melody_example_v3_product', 'melody_example_v3_user', 'melody_example_v3_user_session', 'melody_example_v3_identifier_sequence', 'melody_example_v3_catalog_journal', 'melody_example_v3_two_factor')"

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

if printf '%s' "${V3_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V3_STATUS_OUTPUT_STRING}" | grep -q '20260907000001'; then
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

# the applied list is asserted CLOSED — the opening brace of the migrations block, the one identifier, and
# the bracket and brace that end it — because the schema is ONE migration. Measured on the live document,
# an empty pending list is not rendered at all, so the closed list is what states that the set holds one
# migration and that it is applied; a check that only looked for the identifier somewhere in the document
# would pass over a set that had grown a second step.
if printf '%s' "${V3_STATUS_JSON_STRING}" | grep -q '"migrations":{"applied":\["20260907000001"\]}' \
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
    check_pass "v3 db:rollback reverted the last group (the live tables are dropped until the next boot)"
else
    check_fail "v3 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# read out of band, between the rollback and the next boot: this is the one window in which the drop is
# observable, because the very next application process applies the set again on its way up.
#
# One rollback clears the WHOLE schema, and that is a property of the schema being one migration rather
# than a hope about how a volume was provisioned. It did not hold while the schema was a set of steps: a
# development volume that already carried the tables when a later step was added held that step in a group
# of its own, and no number of rollbacks reached the older group — every invocation boots first, the
# composition root re-applies the pending step as a NEW group on its way up, and the rollback reverts that
# one. That whole class went away with the steps. What a volume carrying an OLDER set holds now is rows
# naming migrations this schema no longer has: bun ignores them, they never appear in db:status, and
# example:db:reset is what takes them away.
V3_TABLE_COUNT_AFTER_ROLLBACK_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING}")"
if [[ "0" = "${V3_TABLE_COUNT_AFTER_ROLLBACK_STRING}" ]]; then
    check_pass "the v3 example tables are gone from mysql after the rollback (read out of band; the audit table, which the set does not own, stands)"
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
if [[ "8" = "${V3_TABLE_COUNT_AFTER_RESTORE_STRING}" ]]; then
    check_pass "the operator's set and the application's are one: all eight tables are back (read out of band)"
else
    check_fail "the restored schema holds ${V3_TABLE_COUNT_AFTER_RESTORE_STRING:-<no answer>} of the eight v3 example tables"
fi

# the user table has no command of its own; resolving the user repository by name through debug:container
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs;
# the seeded rows are read back out of band, because a resolution that succeeds without reseeding
# exits 0 all the same and the failure would surface only as login failures in the next run
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
V3_USER_ROW_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM melody_example_v3_user")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0' && [[ "${V3_USER_ROW_COUNT_STRING}" =~ ^[1-9][0-9]*$ ]]; then
    check_pass "resolving the v3 user repository reseeded the user directory (rows read out of band)"
else
    check_fail "the v3 user repository resolution did not restore the directory (status ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, rows ${V3_USER_ROW_COUNT_STRING:-<no answer>})"
fi

# the manager a status reads is chosen per call: --manager archive answers the archive's postgres where the bare
# command answers the catalogue's mysql, a manager the registry does not hold is one json failure document naming the
# registered ones, and the text form names the connection it reports as the migration's own
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:status --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"port\":[0-9]*' | head -1 | sed 's/^/default_/'
    go run . db:status --manager archive --format=json 2>/dev/null | tr -d ' \n\t' | grep -o '\"port\":[0-9]*' | head -1 | sed 's/^/archive_/'
    go run . db:status --manager nope --format=json >/tmp/migrate-status.json 2>/dev/null
    echo \"unknown_manager_exit=\$?\"
    echo \"unknown_manager_documents=\$(grep -c '^{' /tmp/migrate-status.json)\"
    tr -d ' \n\t' </tmp/migrate-status.json | grep -o '\"error\":{\"code\":\"[^\"]*\"' | head -1 | sed 's/^/unknown_manager_/'
    if tr -d ' \n\t' </tmp/migrate-status.json | grep -q '\"registered\":\[\"archive\",\"default\"\],\"requested\":\"nope\"'; then echo unknown_manager_named=1; else echo unknown_manager_named=0; fi
    go run . db:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep -c '| manager *| default (dedicated migration connection)' | sed 's/^/dedicated_label=/'
    rm -f /tmp/migrate-status.json"
V3_MIGRATE_MANAGER_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'default_"port":3306' && printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'archive_"port":5432'; then
    check_pass "db:status --manager archive reports the archive's postgres (5432) where the bare command reports the catalogue's mysql (3306)"
else
    check_fail "--manager did not choose the database the status reads ($(printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -o '^\(default\|archive\)_.*' | tr '\n' ' '))"
fi

if printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'unknown_manager_exit=1' && printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'unknown_manager_documents=1' \
    && printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'unknown_manager_"error":{"code":"migrate.failed"' && printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'unknown_manager_named=1'; then
    check_pass "db:status --manager nope --format=json exits 1 with one failure document naming the registered managers and the one requested"
else
    check_fail "the unknown manager was not refused in one json document ($(printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -o '^unknown_manager_.*' | tr '\n' ' '))"
fi

if printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -qx 'dedicated_label=1'; then
    check_pass "db:status names the manager it reports as the migration's dedicated connection"
else
    check_fail "db:status did not label the dedicated migration connection ($(printf '%s' "${V3_MIGRATE_MANAGER_STRING}" | grep -o '^dedicated_label=.*'))"
fi

check_section_end "V3 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 MIGRATION LOCK HELD — a lock row another process left is waited on, then refused with its remedy
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 MIGRATION LOCK HELD" "${TAG_VALIDATE}" "e2e"

# the lock is only waited on while there is something left to migrate, so the probe is a database of its own, empty,
# whose lock table holds the row a crashed migration leaves: the insert of the lock row answers the duplicate key the
# driver reports, which is read as a held lock and waited on for the whole window, and the refusal names the command
# that clears it. The row is read out of band afterwards — the refusal must not have taken it — and the database is
# dropped. The name falls under the grant the example databases carry, so the example's own user reaches it
V3_LOCK_PROBE_DATABASE_STRING="melody_example_v3_lockprobe"
e2e_mysql_scalar "mysql" "DROP DATABASE IF EXISTS \`${V3_LOCK_PROBE_DATABASE_STRING}\`; CREATE DATABASE \`${V3_LOCK_PROBE_DATABASE_STRING}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci; CREATE TABLE \`${V3_LOCK_PROBE_DATABASE_STRING}\`.bun_migration_locks (id bigint NOT NULL AUTO_INCREMENT, table_name varchar(255) DEFAULT NULL, PRIMARY KEY (id), UNIQUE KEY table_name (table_name)); INSERT INTO \`${V3_LOCK_PROBE_DATABASE_STRING}\`.bun_migration_locks (table_name) VALUES ('bun_migrations')" >/dev/null
V3_LOCK_ROW_BEFORE_STRING="$(e2e_mysql_scalar "${V3_LOCK_PROBE_DATABASE_STRING}" "SELECT COUNT(*) FROM bun_migration_locks")"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "WORK_DIRECTORY=/tmp/example-lock-held-e2e
    rm -rf \"\${WORK_DIRECTORY}\"
    mkdir -p \"\${WORK_DIRECTORY}\"
    if ! go build -o \"\${WORK_DIRECTORY}/example-lock\" . >/tmp/example-lock-build.log 2>&1; then
        echo build_failed=1
        exit 0
    fi
    cp .env \"\${WORK_DIRECTORY}/.env\"
    cd \"\${WORK_DIRECTORY}\" || exit 1
    printf 'MYSQL_DATABASE=${V3_LOCK_PROBE_DATABASE_STRING}\n' > .env.local
    STARTED_AT=\$(date +%s)
    timeout 90 ./example-lock app:info >/tmp/example-lock.log 2>&1
    echo \"lock_exit=\$?\"
    echo \"lock_waited_seconds=\$(( \$(date +%s) - STARTED_AT ))\"
    if grep -q '^melody: exiting with code 1 after unrecovered error: migration: the migration lock is held' /tmp/example-lock.log; then echo lock_refusal_certified=1; else echo lock_refusal_certified=0; fi
    if grep -q 'the migration lock is held.*\"unlockCommand\":\"db:unlock\"' var/log/dev.log 2>/dev/null; then echo lock_remedy_journaled=1; else echo lock_remedy_journaled=0; fi
    cd / && rm -rf \"\${WORK_DIRECTORY}\" /tmp/example-lock.log /tmp/example-lock-build.log"
V3_LOCK_HELD_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_LOCK_ROW_AFTER_STRING="$(e2e_mysql_scalar "${V3_LOCK_PROBE_DATABASE_STRING}" "SELECT COUNT(*) FROM bun_migration_locks")"
e2e_mysql_scalar "mysql" "DROP DATABASE IF EXISTS \`${V3_LOCK_PROBE_DATABASE_STRING}\`" >/dev/null
printf '%s\n' "${V3_LOCK_HELD_OUTPUT_STRING}"

V3_LOCK_WAITED_STRING="$(printf '%s' "${V3_LOCK_HELD_OUTPUT_STRING}" | grep -o '^lock_waited_seconds=[0-9]*' | cut -d= -f2 || true)"
if [[ "1" == "${V3_LOCK_ROW_BEFORE_STRING}" ]] && printf '%s' "${V3_LOCK_HELD_OUTPUT_STRING}" | grep -qx 'lock_exit=1' \
    && [[ "${V3_LOCK_WAITED_STRING}" =~ ^[0-9]+$ ]] && [[ 25 -le ${V3_LOCK_WAITED_STRING} ]] \
    && printf '%s' "${V3_LOCK_HELD_OUTPUT_STRING}" | grep -qx 'lock_refusal_certified=1' && printf '%s' "${V3_LOCK_HELD_OUTPUT_STRING}" | grep -qx 'lock_remedy_journaled=1' \
    && [[ "1" == "${V3_LOCK_ROW_AFTER_STRING}" ]]; then
    check_pass "a lock row left in an unmigrated database is waited on for ${V3_LOCK_WAITED_STRING}s, then refused with exit 1 naming db:unlock, the row left in place (read out of band)"
else
    check_fail "the held migration lock was not waited on and refused with its remedy (row before ${V3_LOCK_ROW_BEFORE_STRING:-?}, after ${V3_LOCK_ROW_AFTER_STRING:-?}; $(printf '%s' "${V3_LOCK_HELD_OUTPUT_STRING}" | grep -o '^lock_[a-z_]*=.*' | tr '\n' ' '))"
fi

check_section_end "V3 MIGRATION LOCK HELD" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 ROLE GRANT — example:grant:role writes the role through the repository's atomic door, on mysql
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 ROLE GRANT" "${TAG_VALIDATE}" "e2e"

# the grant is asserted OUT OF BAND, on the row the atomic door wrote: the door runs its own transaction with
# the row locked, and no test of the package drives it against a database. It sits right before the reset,
# which is the door that gives the role back — every section before this one reads the seeded directory,
# and the account it widens is the one the sections above needed narrow
# a session opened BEFORE the grant carries the authority the account holds NOW: the session resolver reads the
# account through the repository on every request, so the write role the grant adds opens the write doors to the
# same cookie, and a password changed behind the application (out of band, past every cache) closes the session.
# The status code is read at the door; the cookie is carried across the separate container shells in this shell
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "wget -q -S -O /dev/null --header='Content-Type: application/json' --post-data='{\"username\":\"user\",\"password\":\"user\"}' --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/login/\" 2>&1 | sed -n 's/^ *Set-Cookie: *\([^;]*\).*/session_cookie=\1/p' | head -1"
V3_AUTHORITY_COOKIE_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | sed -n 's/^session_cookie=//p' | head -1)"
V3_AUTHORITY_STATUS_SNIPPET_STRING="wget -q -S -O /dev/null --header='Cookie: ${V3_AUTHORITY_COOKIE_STRING}' --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/outbox/status\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/status=\1/p' | tail -1"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${V3_AUTHORITY_STATUS_SNIPPET_STRING}"
V3_STATUS_BEFORE_GRANT_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | sed -n 's/^status=//p' | tail -1)"

# the directory is seeded flat, one role per account, and the role hierarchy is what widens it: the admin's stored
# roles are ROLE_ADMIN alone, read out of band, and its session still reads the catalogue (ROLE_USER) and the products
# (ROLE_EDITOR), both granted through the hierarchy the global configuration declares
V3_ADMIN_ROLES_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT roles FROM melody_example_v3_user WHERE id = 'user-3'")"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "COOKIE=\$(wget -q -S -O /dev/null --header='Content-Type: application/json' --post-data='{\"username\":\"admin\",\"password\":\"admin\"}' --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/login/\" 2>&1 | sed -n 's/^ *Set-Cookie: *\([^;]*\).*/\1/p' | head -1)
    for ROUTE in /categories/api/read/ /products/api/read/; do
        wget -q -S -O /dev/null --header \"Cookie: \${COOKIE}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}\${ROUTE}\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/status=\1/p' | tail -1
    done"
V3_ADMIN_STATUSES_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | sed -n 's/^status=//p' | tr '\n' ',')"
if [[ "ROLE_ADMIN" = "${V3_ADMIN_ROLES_STRING}" && "200,200" = "${V3_ADMIN_STATUSES_STRING%,}" ]]; then
    check_pass "the admin is stored with ROLE_ADMIN alone and reads the catalogue and the products through the role hierarchy"
else
    check_fail "the flat seed or the hierarchy did not hold: admin roles ${V3_ADMIN_ROLES_STRING:-<no answer>}, catalogue and products answered ${V3_ADMIN_STATUSES_STRING:-<none>}"
fi

V3_GRANT_AUDIT_BEFORE_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COALESCE(MAX(id), 0) FROM melody_example_v3_audit")"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role --role ROLE_EDITOR user 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
V3_GRANTED_ROLES_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT roles FROM melody_example_v3_user WHERE id = 'user-1'")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'granted role "ROLE_EDITOR" to user "user"' \
    && [[ "ROLE_USER,ROLE_EDITOR" = "${V3_GRANTED_ROLES_STRING}" ]]; then
    check_pass "v3 example:grant:role wrote the widened role set through the atomic door (row read out of band)"
else
    check_fail "the v3 grant did not land on the row: output ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, roles ${V3_GRANTED_ROLES_STRING:-<no answer>}"
fi

# the audit trail names the console run that granted the role: a run has no signed-in user, so the actor is the
# run's own process id, the one its journal records carry — read out of band on the audit row and tied to the
# "starting cli application" record the same process wrote, rather than the bare "system" every run shared
V3_GRANT_ACTOR_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT actor FROM melody_example_v3_audit WHERE id > ${V3_GRANT_AUDIT_BEFORE_STRING:-0} AND entity = 'user' AND entity_id = 'user-1' ORDER BY id DESC LIMIT 1")"
V3_GRANT_PROCESS_ID_STRING="${V3_GRANT_ACTOR_STRING#process:}"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "grep -c '\"message\":\"starting cli application\".*\"processId\":\"${V3_GRANT_PROCESS_ID_STRING:-none}\"' var/log/dev.log"
if [[ "${V3_GRANT_ACTOR_STRING}" =~ ^process:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] && [[ "1" = "${RUN_IN_DEV_OUTPUT_STRING}" ]]; then
    check_pass "the grant's audit row names the console run as its actor (${V3_GRANT_ACTOR_STRING}), the process id of the run's own journal record"
else
    check_fail "the grant's audit actor is ${V3_GRANT_ACTOR_STRING:-<no row>}, with ${RUN_IN_DEV_OUTPUT_STRING:-<no answer>} journal records starting that process: wanted process:<the run's id> and exactly one"
fi

# the second run, spelled with the flag's short alias, finds the role held by the ROW — the repository is the only
# arbiter — and writes nothing
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role -r ROLE_EDITOR user 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
V3_REGRANTED_ROLES_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT roles FROM melody_example_v3_user WHERE id = 'user-1'")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'already holds role "ROLE_EDITOR"' \
    && [[ "ROLE_USER,ROLE_EDITOR" = "${V3_REGRANTED_ROLES_STRING}" ]]; then
    check_pass "a second grant of the same role is answered as held and appends nothing"
else
    check_fail "the second grant did not read the row as held: output ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, roles ${V3_REGRANTED_ROLES_STRING:-<no answer>}"
fi

# --role is required: an invocation that leaves it out is refused by the framework, naming the flag, before the
# command runs, and the row is left as the grants above wrote it
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:grant:role user >/tmp/grant-bare.log 2>&1; echo \"bare_exit=\$?\"; if grep -q 'Required flag \"role\" not set' /tmp/grant-bare.log; then echo bare_named=1; else echo bare_named=0; fi; rm -f /tmp/grant-bare.log"
V3_BARE_GRANT_ROLES_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT roles FROM melody_example_v3_user WHERE id = 'user-1'")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qx 'bare_exit=1' && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qx 'bare_named=1' \
    && [[ "ROLE_USER,ROLE_EDITOR" = "${V3_BARE_GRANT_ROLES_STRING}" ]]; then
    check_pass "example:grant:role without --role is refused before it runs, naming the required flag, and the row is untouched"
else
    check_fail "the grant without --role was not refused by the framework: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, roles ${V3_BARE_GRANT_ROLES_STRING:-<no answer>}"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${V3_AUTHORITY_STATUS_SNIPPET_STRING}"
V3_STATUS_AFTER_GRANT_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | sed -n 's/^status=//p' | tail -1)"
if [[ -n "${V3_AUTHORITY_COOKIE_STRING}" && "403" = "${V3_STATUS_BEFORE_GRANT_STRING}" && "200" = "${V3_STATUS_AFTER_GRANT_STRING}" ]]; then
    check_pass "a session opened before the grant carries the granted role on its next request (403 before, 200 after)"
else
    check_fail "the session did not follow the account's roles: cookie ${V3_AUTHORITY_COOKIE_STRING:-<none>}, before ${V3_STATUS_BEFORE_GRANT_STRING:-<none>}, after ${V3_STATUS_AFTER_GRANT_STRING:-<none>}"
fi

e2e_mysql_scalar "melody_example_v3" "UPDATE melody_example_v3_user SET password = CONCAT(password, 'x') WHERE id = 'user-1'" >/dev/null
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${V3_AUTHORITY_STATUS_SNIPPET_STRING}"
V3_STATUS_AFTER_PASSWORD_STRING="$(printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | sed -n 's/^status=//p' | tail -1)"
if [[ -n "${V3_STATUS_AFTER_PASSWORD_STRING}" && "200" != "${V3_STATUS_AFTER_PASSWORD_STRING}" && "403" != "${V3_STATUS_AFTER_PASSWORD_STRING}" ]]; then
    check_pass "a password changed behind the application closes the session opened under the old one (answered ${V3_STATUS_AFTER_PASSWORD_STRING})"
else
    check_fail "the session outlived the password change: answered ${V3_STATUS_AFTER_PASSWORD_STRING:-<none>}"
fi

check_section_end "V3 ROLE GRANT" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 DATABASE RESET — example:db:reset restores the one state this application has
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

# THIS SECTION DESTROYS LIVE DATA: it is the command whose whole purpose is to. It runs after the migration
# section has already put the schema back, and it leaves the database in exactly the state a fresh volume
# holds, which is what every section after it expects.
#
# The example carries this command because it has no history: an example has one state, the present one, so
# a database left in an older shape is answered here rather than by a migration that repairs its past. What
# the checks read is the database itself — the bookkeeping row count, the table count, the trail — rather
# than the command's word for any of it.

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
V3_RESET_REFUSAL_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_TABLE_COUNT_AFTER_REFUSAL_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING}")"

if printf '%s' "${V3_RESET_REFUSAL_STRING}" | grep -q 'nothing was touched' \
    && printf '%s' "${V3_RESET_REFUSAL_STRING}" | grep -q 'melody_example_v3_two_factor' \
    && [[ "8" = "${V3_TABLE_COUNT_AFTER_REFUSAL_STRING}" ]]; then
    check_pass "v3 example:db:reset without --force names what it would drop and drops nothing (8 tables still standing)"
else
    check_fail "the v3 reset refusal did not hold (${V3_RESET_REFUSAL_STRING:-<empty>}, tables ${V3_TABLE_COUNT_AFTER_REFUSAL_STRING:-<no answer>})"
fi

# the run reports each step as it completes and names the database it ran on — host, port and schema — and
# clears the cache last, which is the half of "the state a fresh volume holds" the database cannot carry
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset --force 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'catalogue reset: the schema was dropped and recreated on mysql:3306/melody_example_v3' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'the nomenclature was reseeded' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'cache cleared: the shared cache'; then
    check_pass "v3 example:db:reset --force reports each step it performed, where, and the cache it cleared"
else
    check_fail "v3 example:db:reset --force did not report its steps (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the bookkeeping is the half a rollback cannot reach: a volume migrated by an older set keeps rows naming
# migrations this schema no longer has, and the reset is where they go. Exactly one row is the statement
# that the whole table was dropped and recreated rather than appended to.
V3_BOOKKEEPING_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM bun_migrations")"
V3_TABLE_COUNT_AFTER_RESET_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_EXAMPLE_TABLE_COUNT_STATEMENT_STRING}")"
if [[ "1" = "${V3_BOOKKEEPING_COUNT_STRING}" ]] && [[ "8" = "${V3_TABLE_COUNT_AFTER_RESET_STRING}" ]]; then
    check_pass "the v3 reset left one bookkeeping row and all eight tables (read out of band)"
else
    check_fail "the v3 reset left ${V3_BOOKKEEPING_COUNT_STRING:-<no answer>} bookkeeping row(s) and ${V3_TABLE_COUNT_AFTER_RESET_STRING:-<no answer>} table(s)"
fi

# the trail's SCHEMA belongs to the audit module, which opens it through its own door, so the reset empties its rows
# and leaves the table standing, and then writes the reseed into it as ONE audit transaction: a trail carried across a
# reset would name entities that no longer exist, while the reseed's own entries are the state the trail now accounts for. What is read out of
# band: the five products and three users seeded, each with its insert entry, all under the one transaction row the
# reset opened, whose actor is the console run and whose extras name the command
V3_AUDIT_ROW_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM melody_example_v3_audit")"
V3_AUDIT_GROUPED_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM melody_example_v3_audit a JOIN melody_audit_transaction t ON t.id = a.transaction_id WHERE a.operation = 'INSERT' AND t.extras LIKE '%example:db:reset%'")"
V3_AUDIT_TRANSACTION_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT CONCAT(COUNT(*), '|', MIN(actor)) FROM melody_audit_transaction")"
V3_AUDIT_TABLE_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'melody_example_v3' AND table_name = 'melody_example_v3_audit'")"
if [[ "8" = "${V3_AUDIT_ROW_COUNT_STRING}" ]] && [[ "8" = "${V3_AUDIT_GROUPED_COUNT_STRING}" ]] && [[ "1" = "${V3_AUDIT_TABLE_COUNT_STRING}" ]] \
    && [[ "${V3_AUDIT_TRANSACTION_STRING}" =~ ^1\|process:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
    check_pass "the v3 reset emptied the trail and wrote the reseed into it as one transaction: 8 insert entries under the run's ${V3_AUDIT_TRANSACTION_STRING#1|} (read out of band)"
else
    check_fail "the v3 trail after the reset holds ${V3_AUDIT_ROW_COUNT_STRING:-<no answer>} row(s), ${V3_AUDIT_GROUPED_COUNT_STRING:-<no answer>} grouped under the reset, transaction rows ${V3_AUDIT_TRANSACTION_STRING:-<no answer>}, table ${V3_AUDIT_TABLE_COUNT_STRING:-<no answer>}"
fi

# the reset seeds ALL FOUR nomenclatures through one door, which is what separates it from the lazy seeding
# a repository resolution performs: a command that resolves only the catalogue leaves the user directory
# empty, measured on this very volume. All four are counted rather than the two ends, because the door is a
# list of four and a list is exactly the shape that loses a member without anything else changing. On this
# major the four counts are also the whole proof of that door: the repository package's test handle renders
# statements and cannot execute them, so there is no package-level double to drive it against.
V3_SEEDED_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT CONCAT_WS('/', (SELECT COUNT(*) FROM melody_example_v3_category), (SELECT COUNT(*) FROM melody_example_v3_currency), (SELECT COUNT(*) FROM melody_example_v3_product), (SELECT COUNT(*) FROM melody_example_v3_user))")"
if [[ "${V3_SEEDED_COUNT_STRING}" =~ ^[1-9][0-9]*/[1-9][0-9]*/[1-9][0-9]*/[1-9][0-9]*$ ]]; then
    check_pass "the v3 reset reseeded all four nomenclatures in one pass (categories/currencies/products/accounts = ${V3_SEEDED_COUNT_STRING})"
else
    check_fail "the v3 reset left categories/currencies/products/accounts = ${V3_SEEDED_COUNT_STRING:-<no answer>}, one of them empty"
fi

check_section_end "V3 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 SCHEMA DRIFT — a volume in another shape than the code is refused by name, not answered with a 500
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 SCHEMA DRIFT" "${TAG_VALIDATE}" "e2e"

# The example's schema is ONE migration recorded as applied by name, and its tables are created IF NOT EXISTS,
# so a volume provisioned before a column was added passes the set untouched. Measured before this section was
# written: the currency column added in the same change made the live harness read a product as a 500, the
# journal saying "Unknown column". The section puts the live volume in that shape on purpose — the column
# dropped out of band — and requires a command that reaches the catalogue to refuse it by name, then resets and
# requires the same command to run.
e2e_mysql_scalar "melody_example_v3" "ALTER TABLE melody_example_v3_currency DROP COLUMN provider_rate_as_of" >/dev/null

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . product:list 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if [[ 0 -ne ${RUN_IN_DEV_STATUS_INTEGER} ]] \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'melody_example_v3_currency lacks provider_rate_as_of' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'run example:db:reset --force'; then
    check_pass "v3 product:list over a volume lacking a column refuses it by table and column and names the reset"
else
    check_fail "v3 product:list over a volume lacking provider_rate_as_of answered status ${RUN_IN_DEV_STATUS_INTEGER}: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset --force >/dev/null 2>&1"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . product:list 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if [[ 0 -eq ${RUN_IN_DEV_STATUS_INTEGER} ]]; then
    check_pass "v3 product:list runs again once example:db:reset --force brought the volume to the present schema"
else
    check_fail "v3 product:list after the reset answered status ${RUN_IN_DEV_STATUS_INTEGER}: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}"
fi

# the columns alone do not show a key or a constraint changed under the same names, which is how the amended
# migration reaches a volume built before it: the set records the hash of the statements it built the volume
# with, and a volume holding another hash is refused by name. The section writes another hash out of band.
e2e_mysql_scalar "melody_example_v3" "UPDATE melody_example_v3_schema_fingerprint SET fingerprint = REPEAT('0', 64) WHERE set_name = 'catalogue'" >/dev/null

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . product:list 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if [[ 0 -ne ${RUN_IN_DEV_STATUS_INTEGER} ]] \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'built from another schema than this code' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'run example:db:reset --force'; then
    check_pass "v3 product:list over a volume built from another schema refuses it by its fingerprint and names the reset"
else
    check_fail "v3 product:list over a volume with another fingerprint answered status ${RUN_IN_DEV_STATUS_INTEGER}: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset --force >/dev/null 2>&1"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . product:list 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if [[ 0 -eq ${RUN_IN_DEV_STATUS_INTEGER} ]]; then
    check_pass "v3 product:list runs again once example:db:reset --force rebuilt the volume under this code's fingerprint"
else
    check_fail "v3 product:list after the fingerprint reset answered status ${RUN_IN_DEV_STATUS_INTEGER}: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}"
fi

check_section_end "V3 SCHEMA DRIFT" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 CACHE CLEAR — the cache emptied by a door of its own, without the reset's two databases
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 CACHE CLEAR" "${TAG_VALIDATE}" "e2e"

# the count is read out of redis over RESP by the dev container's nc, so the check does not trust the line the
# command prints about itself. The pattern ends on the product list's own key and matches it under EVERY layout
# token, so an entry an older build left orphaned is counted before and after alike: what the check owns is that
# the entry THIS build cached is gone, which is "after is smaller than before", not a number
V3_CACHE_PRODUCT_LIST_COUNT_COMMAND_STRING='P="melody-example-v3:cache:*example-product-list"; printf "*2\r\n\$4\r\nKEYS\r\n\$${#P}\r\n${P}\r\n" | nc -w 2 redis 6379 | head -1 | tr -d "\r*"'

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . product:list >/dev/null 2>&1; ${V3_CACHE_PRODUCT_LIST_COUNT_COMMAND_STRING}"
V3_CACHE_COUNT_BEFORE_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . example:cache:clear 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if [[ 0 -eq ${RUN_IN_DEV_STATUS_INTEGER} ]] && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'cache cleared: the shared cache'; then
    check_pass "v3 example:cache:clear exits zero and says it cleared the shared cache"
else
    check_fail "v3 example:cache:clear did not report a cleared shared cache (status ${RUN_IN_DEV_STATUS_INTEGER}: ${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${V3_CACHE_PRODUCT_LIST_COUNT_COMMAND_STRING}"
V3_CACHE_COUNT_AFTER_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if [[ "${V3_CACHE_COUNT_BEFORE_STRING}" =~ ^[0-9]+$ ]] && [[ "${V3_CACHE_COUNT_AFTER_STRING}" =~ ^[0-9]+$ ]] \
    && [[ 0 -lt ${V3_CACHE_COUNT_BEFORE_STRING} ]] && [[ ${V3_CACHE_COUNT_AFTER_STRING} -lt ${V3_CACHE_COUNT_BEFORE_STRING} ]]; then
    check_pass "v3 example:cache:clear removed the product list a listing had cached (read out of redis: ${V3_CACHE_COUNT_BEFORE_STRING} -> ${V3_CACHE_COUNT_AFTER_STRING})"
else
    check_fail "v3 example:cache:clear left the cached product list standing (read out of redis: ${V3_CACHE_COUNT_BEFORE_STRING:-<no answer>} -> ${V3_CACHE_COUNT_AFTER_STRING:-<no answer>})"
fi

# eight signed listings race on the empty key: Remember runs the loader on one of them and hands its answer to the
# other seven, so the database sees ONE execution of the listing's select, read out of performance_schema as root
# beside the one key the race left in redis. With the stampede protection switched off the same race runs the select
# four to seven times, which is what makes the single execution the coalescing's doing and not the requests' pacing
V3_CACHE_LIST_DIGEST_STATEMENT_STRING="SELECT COALESCE(SUM(COUNT_STAR),0) FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME='melody_example_v3' AND DIGEST_TEXT LIKE 'SELECT%FROM \`melody_example_v3_product\` AS \`product\` ORDER BY%'"
V3_CACHE_PRODUCT_LIST_KEYS_COMMAND_STRING='P="melody-example-v3:cache:*example-product-list"; printf "*2\r\n\$4\r\nKEYS\r\n\$${#P}\r\n${P}\r\n" | nc -w 2 redis 6379 | tr -d "\r" | grep -v "^[*$]" | sort'

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:cache:clear >/dev/null 2>&1; ${V3_CACHE_PRODUCT_LIST_KEYS_COMMAND_STRING}"
V3_CACHE_KEYS_AFTER_CLEAR_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_CACHE_DIGEST_BEFORE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_CACHE_LIST_DIGEST_STATEMENT_STRING}")"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${EXAMPLE_SIGN_IN_SNIPPET}
    for _ in 1 2 3 4 5 6 7 8; do
        wget -q -O /dev/null --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/products/api/read/\" 2>/dev/null &
    done
    wait
${EXAMPLE_SIGN_OUT_SNIPPET}
    ${V3_CACHE_PRODUCT_LIST_KEYS_COMMAND_STRING}"
V3_CACHE_KEYS_AFTER_RACE_STRING="$(printf '%s\n' "${RUN_IN_DEV_OUTPUT_STRING}" | grep 'example-product-list' || true)"
V3_CACHE_DIGEST_AFTER_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_CACHE_LIST_DIGEST_STATEMENT_STRING}")"
V3_CACHE_FRESH_KEY_STRING="$(comm -13 <(printf '%s\n' "${V3_CACHE_KEYS_AFTER_CLEAR_STRING}" | grep 'example-product-list' | sort) <(printf '%s\n' "${V3_CACHE_KEYS_AFTER_RACE_STRING}" | sort) || true)"
V3_CACHE_FRESH_KEY_COUNT_INTEGER="$(printf '%s' "${V3_CACHE_FRESH_KEY_STRING}" | grep -c 'example-product-list' || true)"

if [[ "${V3_CACHE_DIGEST_BEFORE_STRING}" =~ ^[0-9]+$ ]] && [[ "${V3_CACHE_DIGEST_AFTER_STRING}" =~ ^[0-9]+$ ]] \
    && [[ 1 -eq $((V3_CACHE_DIGEST_AFTER_STRING - V3_CACHE_DIGEST_BEFORE_STRING)) ]] && [[ 1 -eq ${V3_CACHE_FRESH_KEY_COUNT_INTEGER} ]]; then
    check_pass "eight concurrent listings on an empty cache ran the listing's select once and left one key (read out of performance_schema and redis)"
else
    check_fail "the concurrent listings were not coalesced (select executions ${V3_CACHE_DIGEST_BEFORE_STRING:-?} -> ${V3_CACHE_DIGEST_AFTER_STRING:-?}, fresh keys ${V3_CACHE_FRESH_KEY_COUNT_INTEGER:-?})"
fi

# a payload the serializer cannot decode is a miss, not a failure: a corrupt value planted out of band under the key
# the race just filled is answered by recomputing, and the recompute's write replaces it, so the listing answers 200
# and the key holds a value of another length afterwards
V3_CACHE_CORRUPT_PAYLOAD_STRING='not-a-gob-payload'
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "K='$(printf '%s' "${V3_CACHE_FRESH_KEY_STRING}" | head -1)'
    V='${V3_CACHE_CORRUPT_PAYLOAD_STRING}'
    printf \"*3\r\n\\\$3\r\nSET\r\n\\\$\${#K}\r\n\${K}\r\n\\\$\${#V}\r\n\${V}\r\n\" | nc -w 2 redis 6379 | tr -d '\r' | sed 's/^/planted=/'
    printf \"*2\r\n\\\$6\r\nSTRLEN\r\n\\\$\${#K}\r\n\${K}\r\n\" | nc -w 2 redis 6379 | tr -d '\r:' | sed 's/^/length_planted=/'
${EXAMPLE_SIGN_IN_SNIPPET}
    wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/products/api/read/\" 2>/dev/null | grep -o '\"success\":true' | head -1 | sed 's/^/listing=/'
${EXAMPLE_SIGN_OUT_SNIPPET}
    printf \"*2\r\n\\\$6\r\nSTRLEN\r\n\\\$\${#K}\r\n\${K}\r\n\" | nc -w 2 redis 6379 | tr -d '\r:' | sed 's/^/length_healed=/'"
V3_CACHE_HEAL_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_CACHE_LENGTH_PLANTED_STRING="$(printf '%s' "${V3_CACHE_HEAL_OUTPUT_STRING}" | grep -o '^length_planted=[0-9]*' | cut -d= -f2 || true)"
V3_CACHE_LENGTH_HEALED_STRING="$(printf '%s' "${V3_CACHE_HEAL_OUTPUT_STRING}" | grep -o '^length_healed=[0-9]*' | cut -d= -f2 || true)"
if printf '%s' "${V3_CACHE_HEAL_OUTPUT_STRING}" | grep -q '^planted=+OK' && [[ "${#V3_CACHE_CORRUPT_PAYLOAD_STRING}" == "${V3_CACHE_LENGTH_PLANTED_STRING}" ]] \
    && printf '%s' "${V3_CACHE_HEAL_OUTPUT_STRING}" | grep -q '^listing="success":true' \
    && [[ "${V3_CACHE_LENGTH_HEALED_STRING}" =~ ^[0-9]+$ ]] && [[ 0 -lt ${V3_CACHE_LENGTH_HEALED_STRING} ]] && [[ "${V3_CACHE_LENGTH_PLANTED_STRING}" != "${V3_CACHE_LENGTH_HEALED_STRING}" ]]; then
    check_pass "a corrupt payload planted under the product list is answered as a miss and replaced (${V3_CACHE_LENGTH_PLANTED_STRING} -> ${V3_CACHE_LENGTH_HEALED_STRING} bytes, listing 200)"
else
    check_fail "the corrupt payload did not heal (${V3_CACHE_HEAL_OUTPUT_STRING:-<empty>})"
fi

# a product read counts itself on the cache backend's own increment: three reads of one product answer counts two
# apart between the first and the third, the counter is read out of redis as the decimal text of the last answer, and
# a read of an absent product leaves no counter behind
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${EXAMPLE_SIGN_IN_SNIPPET}
    for READ in 1 2 3; do
        wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/products/api/read/prod-1/\" 2>/dev/null | grep -o '\"views\":[0-9]*' | head -1 | sed \"s/^\\\"views\\\":/views_\${READ}=/\"
    done
    wget -q -O /dev/null --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/products/api/read/zz-e2e-absent-product/\" 2>/dev/null
${EXAMPLE_SIGN_OUT_SNIPPET}
    P='melody-example-v3:cache:*example-product-views-prod-1'
    K=\$(printf \"*2\r\n\\\$4\r\nKEYS\r\n\\\$\${#P}\r\n\${P}\r\n\" | nc -w 2 redis 6379 | tr -d '\r' | grep -v '^[*\$]' | head -1)
    printf \"*2\r\n\\\$3\r\nGET\r\n\\\$\${#K}\r\n\${K}\r\n\" | nc -w 2 redis 6379 | tr -d '\r' | grep -v '^\\\$' | head -1 | sed 's/^/stored=/'
    A='melody-example-v3:cache:*example-product-views-zz-e2e-absent-product'
    printf \"*2\r\n\\\$4\r\nKEYS\r\n\\\$\${#A}\r\n\${A}\r\n\" | nc -w 2 redis 6379 | head -1 | tr -d '\r*' | sed 's/^/absent_keys=/'"
V3_PRODUCT_VIEWS_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
v3_product_views_value() {
    printf '%s' "${V3_PRODUCT_VIEWS_STRING}" | grep -o "^${1}=[0-9]*" | head -1 | cut -d= -f2 || true
}
V3_PRODUCT_VIEWS_FIRST_STRING="$(v3_product_views_value views_1)"
V3_PRODUCT_VIEWS_THIRD_STRING="$(v3_product_views_value views_3)"
if [[ "${V3_PRODUCT_VIEWS_FIRST_STRING}" =~ ^[0-9]+$ ]] && [[ "${V3_PRODUCT_VIEWS_THIRD_STRING}" =~ ^[0-9]+$ ]] \
    && [[ 2 -eq $((V3_PRODUCT_VIEWS_THIRD_STRING - V3_PRODUCT_VIEWS_FIRST_STRING)) ]] \
    && [[ "${V3_PRODUCT_VIEWS_THIRD_STRING}" == "$(v3_product_views_value stored)" ]] \
    && [[ "0" == "$(v3_product_views_value absent_keys)" ]]; then
    check_pass "three reads of prod-1 count ${V3_PRODUCT_VIEWS_FIRST_STRING} -> ${V3_PRODUCT_VIEWS_THIRD_STRING}, the counter read out of redis holds the last, and an absent product leaves none"
else
    check_fail "the product view counter did not hold ($(printf '%s' "${V3_PRODUCT_VIEWS_STRING}" | grep -o '^\(views_[0-9]\|stored\|absent_keys\)=[0-9]*' | tr '\n' ' '))"
fi

check_section_end "V3 CACHE CLEAR" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 PLATFORM CHECK — the readiness door takes the shared lock and leaves no object behind
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 PLATFORM CHECK" "${TAG_VALIDATE}" "e2e"

# the door takes the redis lease named example.platform.check before it touches storage, so a lease another holder
# owns turns it away with 409, the one status only a refused acquire answers; the lease is planted out of band over RESP with a token of the harness's own and
# removed after, and the 200 on either side of it is the control. The storage half is read out of band too: the
# probe object the door writes and deletes must be absent from the bucket afterwards, read unsigned from localstack,
# where an object the harness put there itself reads 200 through the same request
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${EXAMPLE_SIGN_IN_SNIPPET}
    platform_status() {
        wget -q -S -O /dev/null --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/platform/check\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1
    }
    redis_command() {
        ARGUMENTS=\"*\$#\r\n\"
        for ARGUMENT in \"\$@\"; do
            ARGUMENTS=\"\${ARGUMENTS}\\\$\${#ARGUMENT}\r\n\${ARGUMENT}\r\n\"
        done
        printf \"\${ARGUMENTS}\" | nc -w 2 redis 6379 | tr -d '\r'
    }
    echo \"platform_before=\$(platform_status)\"
    echo \"lease_planted=\$(redis_command SET example.platform.check held-by-the-e2e-harness NX PX 15000)\"
    echo \"platform_held=\$(platform_status)\"
    echo \"lease_removed=\$(redis_command DEL example.platform.check)\"
    echo \"platform_after=\$(platform_status)\"
${EXAMPLE_SIGN_OUT_SNIPPET}
    BUCKET=\$(grep '^S3_BUCKET=' .env | cut -d= -f2)
    echo \"probe_object=\$(wget -q -S -O /dev/null \"http://localstack:4566/\${BUCKET}/example/platform-check.txt\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)\"
    printf 'control' > /tmp/platform-control.txt
    wget -q -O /dev/null --method=PUT --body-file=/tmp/platform-control.txt \"http://localstack:4566/\${BUCKET}/example/e2e-platform-control.txt\" 2>/dev/null \
        || printf 'PUT /%s/example/e2e-platform-control.txt HTTP/1.1\r\nHost: localstack:4566\r\nContent-Length: 7\r\nConnection: close\r\n\r\ncontrol' \"\${BUCKET}\" | nc -w 2 localstack 4566 >/dev/null
    echo \"control_object=\$(wget -q -S -O /dev/null \"http://localstack:4566/\${BUCKET}/example/e2e-platform-control.txt\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)\"
    printf 'DELETE /%s/example/e2e-platform-control.txt HTTP/1.1\r\nHost: localstack:4566\r\nConnection: close\r\n\r\n' \"\${BUCKET}\" | nc -w 2 localstack 4566 >/dev/null
    echo \"control_removed=\$(wget -q -S -O /dev/null \"http://localstack:4566/\${BUCKET}/example/e2e-platform-control.txt\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/\1/p' | head -1)\"
    rm -f /tmp/platform-control.txt"
V3_PLATFORM_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
printf '%s\n' "${V3_PLATFORM_OUTPUT_STRING}"

platform_output_has() {
    printf '%s' "${V3_PLATFORM_OUTPUT_STRING}" | grep -qx "${1}"
}

if platform_output_has 'platform_before=200' && platform_output_has 'lease_planted=+OK' && platform_output_has 'platform_held=409' \
    && platform_output_has 'lease_removed=:1' && platform_output_has 'platform_after=200'; then
    check_pass "the platform check answers 409 while its lease is held out of band, and 200 on either side of it"
else
    check_fail "the platform check did not honour the held lease ($(printf '%s' "${V3_PLATFORM_OUTPUT_STRING}" | grep -o '^\(platform\|lease\)_[a-z_]*=.*' | tr '\n' ' '))"
fi

if platform_output_has 'probe_object=404' && platform_output_has 'control_object=200' && platform_output_has 'control_removed=404'; then
    check_pass "the platform check left no probe object in the bucket, where an object the harness put there reads back (read out of band)"
else
    check_fail "the bucket read did not show the probe object gone beside a visible control ($(printf '%s' "${V3_PLATFORM_OUTPUT_STRING}" | grep -o '^\(probe\|control\)_object=.*' | tr '\n' ' '))"
fi

check_section_end "V3 PLATFORM CHECK" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 MAILER SEND — the console door hands one message to the smtp relay, read back out of mailpit
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 MAILER SEND" "${TAG_VALIDATE}" "e2e"

# the command's own exit is not the evidence: the relay's inbox is, read over mailpit's api by a subject carrying a
# nonce, so a message another run left cannot answer for this one. The parsed message carries both bodies, decoded,
# and the raw source their structure: the two alternatives under the related part that holds the inline logo. The
# message is deleted after, so the inbox is left as it was found
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "NONCE=\$(date +%s%N)
    go run . mailer:send --to ada@example.com --subject e2e-mailer-\${NONCE} --text hello-\${NONCE} >/tmp/mailer-send.log 2>&1
    echo \"send_exit=\$?\"
    SEARCH=''
    for _ in \$(seq 1 25); do
        SEARCH=\$(wget -q -O - \"\${MAILPIT_API_URL}/api/v1/search?query=subject:e2e-mailer-\${NONCE}\" 2>/dev/null)
        if printf '%s' \"\${SEARCH}\" | grep -q '\"messages_count\":1'; then
            break
        fi
        sleep 0.2
    done
    printf '%s' \"\${SEARCH}\" | grep -o '\"messages_count\":[0-9]*' | sed 's/^\"messages_count\":/received=/'
    ID=\$(printf '%s' \"\${SEARCH}\" | grep -o '\"ID\":\"[^\"]*\"' | head -1 | cut -d'\"' -f4)
    MESSAGE=\$(wget -q -O - \"\${MAILPIT_API_URL}/api/v1/message/\${ID}\" 2>/dev/null)
    if printf '%s' \"\${MESSAGE}\" | grep -q \"\\\"Text\\\":\\\"hello-\${NONCE}\"; then echo text_body=1; else echo text_body=0; fi
    if printf '%s' \"\${MESSAGE}\" | sed -n 's/.*\"HTML\":\"\(.*\)\",\"Size\".*/\1/p' | grep -q \"hello-\${NONCE}\"; then echo html_body=1; else echo html_body=0; fi
    RAW=\$(wget -q -O - \"\${MAILPIT_API_URL}/api/v1/message/\${ID}/raw\" 2>/dev/null)
    printf '%s\n' \"\${RAW}\" | grep -i '^Content-Type:' | sed 's/;.*//' | tr -d '\r' | tr '\n' ',' | sed 's/^/structure=/'
    echo
    BODY=\"{\\\"IDs\\\":[\\\"\${ID}\\\"]}\"
    printf 'DELETE /api/v1/messages HTTP/1.1\r\nHost: mailpit\r\nContent-Type: application/json\r\nContent-Length: %s\r\nConnection: close\r\n\r\n%s' \"\${#BODY}\" \"\${BODY}\" | nc -w 2 mailpit 8025 >/dev/null
    rm -f /tmp/mailer-send.log"
V3_MAILER_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
printf '%s\n' "${V3_MAILER_OUTPUT_STRING}"

if printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -qx 'send_exit=0' && printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -qx 'received=1' \
    && printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -qx 'text_body=1' && printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -qx 'html_body=1'; then
    check_pass "mailer:send delivered one message to the relay, carrying the text in both its plain and html bodies (read out of mailpit)"
else
    check_fail "mailer:send did not deliver the message it was given ($(printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -o '^\(send_exit\|received\|text_body\|html_body\)=.*' | tr '\n' ' '))"
fi

if printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -q '^structure=.*Content-Type: multipart/alternative,Content-Type: text/plain,Content-Type: text/html,'; then
    check_pass "the message is multipart/alternative, a plain part then an html part"
else
    check_fail "the message is not a plain and an html alternative ($(printf '%s' "${V3_MAILER_OUTPUT_STRING}" | grep -o '^structure=.*'))"
fi

check_section_end "V3 MAILER SEND" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 TWO-FACTOR RELEASE — the enrollment table the reset just recreated cascades its rows with the account
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 TWO-FACTOR RELEASE" "${TAG_VALIDATE}" "e2e"

# An enrollment row that outlives its account is a second factor for nobody, and was the next holder's while
# identifiers were minted as the highest present suffix plus one. A subscriber releases the row on the deletion event, and the e2e harness
# drives that door end to end; what this section reads is the OTHER half, the one that holds when no listener
# runs at all: the foreign key the schema declares, cascading the row with the account. It sits after the
# reset because CREATE TABLE IF NOT EXISTS leaves a table an older volume already held as it was — the reset
# is the door that brings such a volume to the schema as it stands, and only the schema the reset applied can
# be read here as the schema this tree ships.
V3_TWO_FACTOR_CASCADE_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT COUNT(*) FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = 'melody_example_v3' AND TABLE_NAME = 'melody_example_v3_two_factor' AND REFERENCED_TABLE_NAME = 'melody_example_v3_user' AND DELETE_RULE = 'CASCADE'")"
if [[ "1" = "${V3_TWO_FACTOR_CASCADE_COUNT_STRING}" ]]; then
    check_pass "the v3 two-factor table cascades its rows with the account they were enrolled for (foreign key read out of information_schema after the reset)"
else
    check_fail "the v3 two-factor table declares ${V3_TWO_FACTOR_CASCADE_COUNT_STRING:-<no answer>} cascading keys onto the user table, wanted exactly one"
fi

check_section_end "V3 TWO-FACTOR RELEASE" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 EXCHANGE RATES — the outbound http client, driven through the door that needs one
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 EXCHANGE RATES" "${TAG_VALIDATE}" "e2e"

# This section is the only thing in the repository that calls melody/v3/httpclient at all: nothing else
# imports it, on any major, so compilation is all the package had before this. It runs after the reset, which
# is what makes the opening quotes a known value rather than whatever a previous run left behind.
#
# The provider is a vhost of the development load balancer answering under its own name. It is not on the
# public internet on purpose — the gate has twice spent a session diagnosing external DNS — and the failure
# arms are BASES rather than targets, so pointing the application at one exercises a real failure through the
# real wiring instead of a second code path written for a test.

V3_RATE_QUOTE_STATEMENT_STRING="SELECT CONCAT(rate, '@', DATE_FORMAT(rate_as_of, '%Y-%m-%dT%H:%i:%sZ')) FROM melody_example_v3_currency WHERE id = 'cur-usd'"

V3_SEEDED_QUOTE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_RATE_QUOTE_STATEMENT_STRING}")"
if [[ "1.1@2026-01-01T00:00:00Z" = "${V3_SEEDED_QUOTE_STRING}" ]]; then
    check_pass "the reset left the shipped quote in place (cur-usd ${V3_SEEDED_QUOTE_STRING})"
else
    check_fail "the reset left cur-usd quoted ${V3_SEEDED_QUOTE_STRING:-<no answer>}, wanted the shipped 1.1@2026-01-01T00:00:00Z"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:currency:refresh-rates 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_REFRESH_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_REFRESH_OUTPUT_STRING}" | grep -qE '2026-09-07T09:00:00Z[[:space:]]*\|[[:space:]]*1[[:space:]]*\|[[:space:]]*3[[:space:]]*\|[[:space:]]*0'; then
    check_pass "example:currency:refresh-rates read the provider in one attempt and wrote all three quotes"
else
    check_fail "the refresh reported ${V3_REFRESH_OUTPUT_STRING:-<empty>}, wanted the provider's instant with attempts 1, updated 3, skipped 0"
fi

# the command's word for what it wrote is not the evidence; the database is. Both halves move: the number
# and the instant, and the instant is the PROVIDER's rather than the moment the refresh happened to run.
V3_REFRESHED_QUOTE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_RATE_QUOTE_STATEMENT_STRING}")"
if [[ "1.0842@2026-09-07T09:00:00Z" = "${V3_REFRESHED_QUOTE_STRING}" ]]; then
    check_pass "the provider's quote landed in the catalogue (cur-usd ${V3_REFRESHED_QUOTE_STRING}, read out of band)"
else
    check_fail "cur-usd is quoted ${V3_REFRESHED_QUOTE_STRING:-<no answer>}, wanted the provider's 1.0842@2026-09-07T09:00:00Z"
fi

# the provider serves its rates to a client presenting its api key only, so the refresh above landed because the rates
# client sent RATES_API_KEY: the same document asked for without the key, or with another, is refused 401 — the control
# that the provider enforces it — and debug:parameters shows the key redacted, as the credential it is
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "wget -q -S -O /dev/null http://rates.melody.localhost.precision-soft.com/v1/latest 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/keyless=\1/p' | head -1
    wget -q -S -O /dev/null --header='x-api-key: not-the-key' http://rates.melody.localhost.precision-soft.com/v1/latest 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/wrong_key=\1/p' | head -1
    go run . debug:parameters 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep -E '^\| RATES_API_KEY ' | grep -c 'e2e-rates-key-0001' | sed 's/^/key_in_clear=/'"
V3_RATES_KEY_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_RATES_KEY_STRING}" | grep -qx 'keyless=401' && printf '%s' "${V3_RATES_KEY_STRING}" | grep -qx 'wrong_key=401' \
    && printf '%s' "${V3_RATES_KEY_STRING}" | grep -qx 'key_in_clear=0'; then
    check_pass "the provider refuses its rates without the api key the refresh sent, and debug:parameters redacts RATES_API_KEY"
else
    check_fail "the rates api key did not hold: ${V3_RATES_KEY_STRING:-<empty>}"
fi

# the reading is stored in both reference frames: the provider's stamp as it came, beside the same instant moved
# onto this application's clock. The stub answers from the balancer, whose clock is this host's, so the offset is
# measured as none and the two instants are one — the table says so in its last column.
V3_PROVIDER_STAMP_STRING="$(e2e_mysql_scalar "melody_example_v3" "SELECT DATE_FORMAT(provider_rate_as_of, '%Y-%m-%dT%H:%i:%sZ') FROM melody_example_v3_currency WHERE id = 'cur-usd'")"
if [[ "2026-09-07T09:00:00Z" = "${V3_PROVIDER_STAMP_STRING}" ]]; then
    check_pass "the provider's own stamp landed beside the quote (cur-usd provider_rate_as_of ${V3_PROVIDER_STAMP_STRING}, read out of band)"
else
    check_fail "cur-usd carries the provider stamp ${V3_PROVIDER_STAMP_STRING:-<no answer>}, wanted 2026-09-07T09:00:00Z as the document wrote it"
fi

if printf '%s' "${V3_REFRESH_OUTPUT_STRING}" | grep -qE '\|[[:space:]]*\+0s[[:space:]]*\|?[[:space:]]*$'; then
    check_pass "example:currency:refresh-rates measured the provider's clock from its answer and found no offset"
else
    check_fail "the refresh reported ${V3_REFRESH_OUTPUT_STRING:-<empty>}, wanted a measured provider clock at +0s"
fi

# the same document a second time is a provider between two moves: nothing is written and the run says
# so under its own heading, where the previous form issued a full-row UPDATE per currency and, on mysql,
# read its zero affected rows as three currencies deleted inside the run (SKIPPED 3). Columns: AS_OF,
# ATTEMPTS, UPDATED, SKIPPED, UNCHANGED, STALE, REFUSED, PROVIDER_CLOCK
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:currency:refresh-rates 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_SECOND_REFRESH_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_SECOND_REFRESH_OUTPUT_STRING}" | grep -qE '2026-09-07T09:00:00Z[[:space:]]*\|[[:space:]]*1[[:space:]]*\|[[:space:]]*0[[:space:]]*\|[[:space:]]*0[[:space:]]*\|[[:space:]]*3[[:space:]]*\|[[:space:]]*0[[:space:]]*\|[[:space:]]*0'; then
    check_pass "a second refresh of an unmoved document reports UNCHANGED 3 and writes nothing"
else
    check_fail "the second refresh reported ${V3_SECOND_REFRESH_OUTPUT_STRING:-<empty>}, wanted updated 0, skipped 0, unchanged 3"
fi

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . catalog:report:refresh 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_EXPORT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_EXPORT_OUTPUT_STRING}" | grep -q 'true'; then
    check_pass "catalog:report:refresh pushed the reading to the configured sink"
else
    check_fail "the report refresh reported ${V3_EXPORT_OUTPUT_STRING:-<empty>}, wanted an export the sink accepted"
fi

# melody resolves configuration from .env files and never from the process environment, so the two arms below
# land their base in .env.local, which overrides .env and is git-ignored. The trap and the pre-clear are the
# same hygiene the process-role section uses: a run killed before its EXIT trap would otherwise leave a base
# behind that poisons every later section.
restore_example_env_local

trap restore_example_env_local EXIT

docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'RATES_BASE_URL=http://rates.melody.localhost.precision-soft.com/unavailable/\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

# the exit code is the property, not the message: this command runs unattended on a schedule, and a provider
# it could not read has to reach an operator somehow. A zero here would leave the catalogue quoting stale
# rates with a green cron log above it.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . example:currency:refresh-rates >/dev/null 2>&1"
V3_REFRESH_FAILURE_STATUS_INTEGER="${RUN_IN_DEV_STATUS_INTEGER}"
if [[ "0" != "${V3_REFRESH_FAILURE_STATUS_INTEGER}" ]]; then
    check_pass "a provider that refuses every attempt exits the refresh non-zero (${V3_REFRESH_FAILURE_STATUS_INTEGER})"
else
    check_fail "the refresh exited zero over a provider that answered 503 to every attempt"
fi

# the quote must not have moved: a refresh that failed has to leave the catalogue exactly as it found it
V3_QUOTE_AFTER_FAILURE_STRING="$(e2e_mysql_scalar "melody_example_v3" "${V3_RATE_QUOTE_STATEMENT_STRING}")"
if [[ "${V3_REFRESHED_QUOTE_STRING}" = "${V3_QUOTE_AFTER_FAILURE_STRING}" ]]; then
    check_pass "a failed refresh left the quote untouched (cur-usd still ${V3_QUOTE_AFTER_FAILURE_STRING})"
else
    check_fail "a failed refresh moved cur-usd from ${V3_REFRESHED_QUOTE_STRING} to ${V3_QUOTE_AFTER_FAILURE_STRING:-<no answer>}"
fi

# an absent base is how this application spells "this door is unwired", the same switch every optional
# integration carries. It is a no-op that says so and exits zero, so a deployment without a rate provider
# runs the schedule without failing it.
docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'RATES_BASE_URL=\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null

# pipefail is set explicitly because the container runs this string through a fresh `bash -c`, which does
# NOT inherit it from this script: without it the status of a pipeline is sed's, and sed always exits zero,
# so the exit-code half of the assertion below would hold whatever the command did
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . example:currency:refresh-rates 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_UNCONFIGURED_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_UNCONFIGURED_STATUS_INTEGER="${RUN_IN_DEV_STATUS_INTEGER}"
if [[ "0" = "${V3_UNCONFIGURED_STATUS_INTEGER}" ]] \
    && printf '%s' "${V3_UNCONFIGURED_OUTPUT_STRING}" | grep -q 'no rate provider is configured'; then
    check_pass "with no provider configured the refresh is a no-op that says so and exits zero"
else
    check_fail "an unconfigured refresh exited ${V3_UNCONFIGURED_STATUS_INTEGER:-<no status>} saying ${V3_UNCONFIGURED_OUTPUT_STRING:-<empty>}"
fi

# the export client refuses a redirect rather than following it: the sink answered from /v1/moved-sink is a 307 to the
# real sink, so a client that followed would still deliver and report success. The refusal is read on both sides —
# the command reports the export as not done and fails, and the balancer's own log, read out of band, holds the one
# POST it answered 307 and no request at all to the sink it redirected to
docker_compose_no_log exec -T "${E2E_SERVICE_NAME_STRING}" \
    bash -c "printf 'APP_REPORTING_EXPORT_ENDPOINT=http://rates.melody.localhost.precision-soft.com/v1/moved-sink\n' > ${EXAMPLE_ENV_LOCAL_PATH_STRING}" </dev/null
V3_REDIRECT_SINCE_STRING="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . catalog:report:refresh 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | grep '|'"
V3_REDIRECT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_REDIRECT_STATUS_INTEGER="${RUN_IN_DEV_STATUS_INTEGER}"
sleep 1
V3_REDIRECT_BALANCER_LOG_STRING="$(docker_compose_no_log logs --no-log-prefix --since "${V3_REDIRECT_SINCE_STRING}" load-balancer 2>/dev/null | grep -E '"POST /v1/(moved-sink|report-sink) ' || true)"
V3_REDIRECT_ANSWERED_INTEGER="$(printf '%s\n' "${V3_REDIRECT_BALANCER_LOG_STRING}" | grep -c '"POST /v1/moved-sink HTTP/1.1" 307 ' || true)"
V3_REDIRECT_FOLLOWED_INTEGER="$(printf '%s\n' "${V3_REDIRECT_BALANCER_LOG_STRING}" | grep -c '/v1/report-sink' || true)"
if [[ "0" != "${V3_REDIRECT_STATUS_INTEGER}" ]] && printf '%s' "${V3_REDIRECT_OUTPUT_STRING}" | grep -qE '\|  false +\|' \
    && [[ "1" == "${V3_REDIRECT_ANSWERED_INTEGER}" ]] && [[ "0" == "${V3_REDIRECT_FOLLOWED_INTEGER}" ]]; then
    check_pass "an export answered with a 307 is refused, not followed: the refresh fails with EXPORTED false and the balancer saw no request to the sink it redirected to"
else
    check_fail "the redirected export was not refused (status ${V3_REDIRECT_STATUS_INTEGER}, output ${V3_REDIRECT_OUTPUT_STRING:-<empty>}, balancer 307s ${V3_REDIRECT_ANSWERED_INTEGER:-?}, sink requests ${V3_REDIRECT_FOLLOWED_INTEGER:-?})"
fi

restore_example_env_local

check_section_end "V3 EXCHANGE RATES" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V3 READING ARCHIVE — the second database, on postgres, and the command family pinned to it
# ---------------------------------------------------------------------------------------------------

check_section_start "V3 READING ARCHIVE" "${TAG_VALIDATE}" "e2e"

# The v3 example holds two databases: the catalogue on mysql and the archive of catalogue readings on
# postgres. This section is the only thing that drives the pgsql PROVIDER anywhere in the repository —
# the harness's own postgres connection is a bare pgdriver connector, and the advisory-lock check beside
# it builds its locker over that, so before this the provider had compilation and its package tests.
#
# The out-of-band reads go through e2e_pgsql_scalar, which did not exist until this section needed it:
# until now a postgres set could only be asserted through the application's own db:<context>:status,
# which is the application's word for the state rather than the state itself.

V3_ARCHIVE_DATABASE_STRING="melody_example_v3"
V3_ARCHIVE_COUNT_STATEMENT_STRING="SELECT COUNT(*) FROM melody_example_v3_catalog_reading"

# the archive's own command family, pinned to its manager: it reaches postgres and only postgres. Run over
# a set the boot has already applied, so success here is idempotence rather than a first migration.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:archive:migrate >/dev/null 2>&1; echo status=\$?"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0'; then
    check_pass "v3 db:archive:migrate answers success over the set the archive provider already applied"
else
    check_fail "v3 db:archive:migrate failed (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the two families are pinned to two managers, so each names its own database. This is what a context
# buys over a second module registration, and the document each command prints is where it shows.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:archive:status --format=json 2>/dev/null"
V3_ARCHIVE_STATUS_JSON_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_ARCHIVE_STATUS_JSON_STRING}" | grep -q '"20260907100001'; then
    check_pass "v3 db:archive:status names the archive's own set"
else
    check_fail "v3 db:archive:status did not name the archive set (${V3_ARCHIVE_STATUS_JSON_STRING:-<empty>})"
fi

# the catalogue family is unmoved by the archive's arrival: it still reaches mysql, and its own set is
# what it reports. A pin that had gone to the wrong manager would show here first.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "go run . db:status --format=json 2>/dev/null"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q '"database":"melody_example_v3"'; then
    check_pass "v3 db:status still names the catalogue database, so the base family kept its manager"
else
    check_fail "v3 db:status no longer names the catalogue database (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# what the refresh SAYS it did is not the evidence; the archive is. The count is read out of band, before
# and after, so the assertion is a difference rather than a total — the archive carries whatever earlier
# runs left, and a total would be an assertion about the order the sections happen to run in.
V3_ARCHIVE_COUNT_BEFORE_STRING="$(e2e_pgsql_scalar "${V3_ARCHIVE_DATABASE_STRING}" "${V3_ARCHIVE_COUNT_STATEMENT_STRING}")"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go run . catalog:report:refresh 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
V3_ARCHIVE_REFRESH_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
V3_ARCHIVE_COUNT_AFTER_STRING="$(e2e_pgsql_scalar "${V3_ARCHIVE_DATABASE_STRING}" "${V3_ARCHIVE_COUNT_STATEMENT_STRING}")"

if [[ "$(( ${V3_ARCHIVE_COUNT_BEFORE_STRING:-0} + 1 ))" = "${V3_ARCHIVE_COUNT_AFTER_STRING:-0}" ]]; then
    check_pass "catalog:report:refresh added exactly one reading to the archive (${V3_ARCHIVE_COUNT_BEFORE_STRING} -> ${V3_ARCHIVE_COUNT_AFTER_STRING})"
else
    check_fail "the refresh took the archive from ${V3_ARCHIVE_COUNT_BEFORE_STRING:-<no answer>} to ${V3_ARCHIVE_COUNT_AFTER_STRING:-<no answer>}, wanted exactly one more"
fi

if printf '%s' "${V3_ARCHIVE_REFRESH_OUTPUT_STRING}" | grep -qE 'ARCHIVED'; then
    check_pass "catalog:report:refresh reports what it did with the archive"
else
    check_fail "the refresh did not report an archive column (${V3_ARCHIVE_REFRESH_OUTPUT_STRING:-<empty>})"
fi

# the row the archive holds and the payload it carries agree on the instant, which is the property the
# truncation to the second exists for: the payload writes recorded_at as RFC3339, so a key kept finer
# would disagree with the value it keys. Asserted as an EQUALITY between the two halves of one row rather
# than against a clock, so it holds whatever second the run lands in.
V3_ARCHIVE_AGREEMENT_STRING="$(e2e_pgsql_scalar "${V3_ARCHIVE_DATABASE_STRING}" "SELECT CASE WHEN payload LIKE '%recorded_at=' || to_char(taken_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') THEN 'agree' ELSE 'differ' END FROM melody_example_v3_catalog_reading ORDER BY taken_at DESC LIMIT 1")"
if [[ "agree" = "${V3_ARCHIVE_AGREEMENT_STRING}" ]]; then
    check_pass "the archived row and the payload it carries name the same instant"
else
    check_fail "the archived row and its payload disagree on the instant (${V3_ARCHIVE_AGREEMENT_STRING:-<no answer>})"
fi

# two processes running the same schedule record ONE reading between them rather than one each. That is
# the advisory lock doing the only thing it is there for, and it cannot be proved by mutation (a guard
# against a race is not), so it is driven here. The assertion is a difference of one over two runs.
V3_ARCHIVE_CONCURRENT_BEFORE_STRING="$(e2e_pgsql_scalar "${V3_ARCHIVE_DATABASE_STRING}" "${V3_ARCHIVE_COUNT_STATEMENT_STRING}")"

run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "set -o pipefail; go build -o melody-example-e2e . && ( ./melody-example-e2e catalog:report:refresh >/tmp/e2e-refresh-a.out 2>&1 & ./melody-example-e2e catalog:report:refresh >/tmp/e2e-refresh-b.out 2>&1 & wait ); rm -f melody-example-e2e; sed 's/\x1b\[[0-9;]*m//g' /tmp/e2e-refresh-a.out /tmp/e2e-refresh-b.out; rm -f /tmp/e2e-refresh-a.out /tmp/e2e-refresh-b.out"
V3_ARCHIVE_CONCURRENT_OUTPUT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"

V3_ARCHIVE_CONCURRENT_AFTER_STRING="$(e2e_pgsql_scalar "${V3_ARCHIVE_DATABASE_STRING}" "${V3_ARCHIVE_COUNT_STATEMENT_STRING}")"
V3_ARCHIVE_CONCURRENT_DELTA_INTEGER="$(( ${V3_ARCHIVE_CONCURRENT_AFTER_STRING:-0} - ${V3_ARCHIVE_CONCURRENT_BEFORE_STRING:-0} ))"
if [[ "1" = "${V3_ARCHIVE_CONCURRENT_DELTA_INTEGER}" ]]; then
    check_pass "two concurrent refreshes recorded one reading between them, not one each"
else
    check_fail "two concurrent refreshes added ${V3_ARCHIVE_CONCURRENT_DELTA_INTEGER} readings, wanted exactly 1"
fi

# the one reading is the LOCK's doing, not the second's luck: the lock is taken around the whole run, so
# the process that could not take it took no reading at all and said so — where the previous form let both
# take one and left the primary key, on the instant truncated to the second, to fold two readings into one
# only when both landed inside the same second
if printf '%s' "${V3_ARCHIVE_CONCURRENT_OUTPUT_STRING}" | grep -q 'another process is taking this reading'; then
    check_pass "the refresh that lost the archive lock skipped its run and said another process was taking the reading"
else
    check_fail "neither concurrent refresh reported a lost lock (${V3_ARCHIVE_CONCURRENT_OUTPUT_STRING:-<empty>})"
fi

# the read half. Only the anonymous arm is driven here: the authenticated flow belongs where the Go
# harness drives the listing itself, its limit and its refusals (the session snippet above serves the
# sections that have no Go twin). What this arm states is worth stating on its own: the archive carries
# the catalogue's history, so a door onto it that answered an anonymous caller would publish what the
# listings are gated for.
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "wget -q -S -O /dev/null \"\${EXAMPLE_BASE_URL}/reports/api/history/\" 2>&1 | grep -m1 'HTTP/' || true"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q '401'; then
    check_pass "an anonymous caller is refused the archive listing"
else
    check_fail "the archive listing answered ${RUN_IN_DEV_OUTPUT_STRING:-<no answer>} to an anonymous caller, wanted 401"
fi

# the listing's limit is a query value read through the bag: the first value of a repeated key is the one taken,
# and a value that is not a positive whole number is refused. The control is the same listing without a limit, which
# holds at least the two readings the refreshes above recorded, so a limit that did nothing would answer more than one
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${EXAMPLE_SIGN_IN_SNIPPET}
    for QUERY in 'limit=1' 'limit=1&limit=5' ''; do
        COUNT=\$(wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/reports/api/history/?\${QUERY}\" 2>/dev/null | grep -o '\"product_count\"' | wc -l | tr -d ' ')
        echo \"readings_\$(printf '%s' \"\${QUERY:-none}\" | tr '=&' '__')=\${COUNT}\"
    done
    wget -q -S -O /dev/null --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/reports/api/history/?limit=x\" 2>&1 | sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/status_limit_x=\1/p' | head -1
${EXAMPLE_SIGN_OUT_SNIPPET}"
V3_HISTORY_LIMIT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_HISTORY_LIMIT_STRING}" | grep -qx 'readings_limit_1=1' \
    && printf '%s' "${V3_HISTORY_LIMIT_STRING}" | grep -qx 'readings_limit_1_limit_5=1' \
    && printf '%s' "${V3_HISTORY_LIMIT_STRING}" | grep -qx 'status_limit_x=400' \
    && [[ 2 -le "$(printf '%s' "${V3_HISTORY_LIMIT_STRING}" | grep -o '^readings_none=[0-9]*' | cut -d= -f2 || echo 0)" ]]; then
    check_pass "the archive listing honours ?limit=1, takes the first of a repeated limit and refuses ?limit=x with 400, where the bare listing holds more"
else
    check_fail "the archive listing's limit did not hold ($(printf '%s' "${V3_HISTORY_LIMIT_STRING}" | grep -o '^\(readings\|status\)_[a-z0-9_]*=[0-9]*' | tr '\n' ' '))"
fi

# the export answers the same readings as a csv attachment: signed in, ?limit=2 is a header and two rows, named through
# Content-Disposition, and the newest reading's headline the json listing answers is the one the csv carries first —
# the same data through the two doors
run_in_dev_capture "${EXAMPLE_DIRECTORY_STRING}" "${EXAMPLE_SIGN_IN_SNIPPET}
    wget -q -S -O /tmp/readings-export.csv --header=\"\${SESSION_COOKIE_HEADER}\" \"\${EXAMPLE_BASE_URL}/reports/api/export/?limit=2\" 2>/tmp/readings-export.headers
    sed -n 's/^ *HTTP\/[0-9.]* \([0-9]*\).*/export_status=\1/p' /tmp/readings-export.headers | head -1
    if grep -qi '^ *Content-Type: text/csv; charset=utf-8' /tmp/readings-export.headers; then echo export_csv=1; else echo export_csv=0; fi
    if grep -qiE '^ *Content-Disposition: attachment; filename=\"catalog-readings-[0-9]{4}-[0-9]{2}-[0-9]{2}\.csv\"' /tmp/readings-export.headers; then echo export_named=1; else echo export_named=0; fi
    if head -1 /tmp/readings-export.csv | grep -qx 'taken_at,headline,product_count,journal_count,payload'; then echo export_header=1; else echo export_header=0; fi
    echo \"export_rows=\$(grep -cE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T' /tmp/readings-export.csv)\"
    HEADLINE=\$(wget -q -O- --header=\"\${SESSION_COOKIE_HEADER}\" --header='Accept: application/json' \"\${EXAMPLE_BASE_URL}/reports/api/history/?limit=1\" 2>/dev/null | grep -o '\"headline\":\"[^\"]*\"' | head -1 | cut -d'\"' -f4)
    if [ -n \"\${HEADLINE}\" ] && sed -n '2p' /tmp/readings-export.csv | grep -qF \"\${HEADLINE}\"; then echo export_same_newest=1; else echo export_same_newest=0; fi
    rm -f /tmp/readings-export.csv /tmp/readings-export.headers
${EXAMPLE_SIGN_OUT_SNIPPET}"
V3_EXPORT_STRING="${RUN_IN_DEV_OUTPUT_STRING}"
if printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_status=200' \
    && printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_csv=1' \
    && printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_named=1' \
    && printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_header=1' \
    && printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_rows=2' \
    && printf '%s' "${V3_EXPORT_STRING}" | grep -qx 'export_same_newest=1'; then
    check_pass "the archive export answers ?limit=2 as a named csv attachment, a header and two rows, the newest reading the json listing's"
else
    check_fail "the archive export did not hold ($(printf '%s' "${V3_EXPORT_STRING}" | grep -o '^export_[a-z_]*=[0-9]*' | tr '\n' ' '))"
fi

check_section_end "V3 READING ARCHIVE" "${TAG_VALIDATE}" "e2e"

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

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops the five v1 catalog tables of the v1
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

if printf '%s' "${V1_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V1_STATUS_OUTPUT_STRING}" | grep -q '20260907000001'; then
    check_pass "v1 db:status reports the applied set by name with nothing pending"
else
    check_fail "v1 db:status does not report the applied set (${V1_STATUS_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v1 db:rollback reverted the last group (the five live catalog tables are dropped until the next step)"
else
    check_fail "v1 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'applied 1 migration'; then
    check_pass "v1 db:migrate re-applied the catalog schema the rollback reverted"
else
    check_fail "v1 db:migrate did not re-apply the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the session index the sign-in doors keep each account under its cap with is part of the one schema step:
# read out of band, with the key that cascades an account's delete, since a table the migration declared
# but MySQL refused (a foreign key over another collation) would surface only as a 500 at the next sign-in
V1_SESSION_INDEX_STRING="$(e2e_mysql_scalar "melody_example_v1" "SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema = 'melody_example_v1' AND table_name = 'melody_example_v1_user_session' AND referenced_table_name = 'melody_example_v1_user' AND delete_rule = 'CASCADE'")"
if [[ "1" = "${V1_SESSION_INDEX_STRING}" ]]; then
    check_pass "the v1 session index is back with the schema, its rows cascading with their account (read out of band)"
else
    check_fail "the v1 session index or its cascading key is missing after the migrate (${V1_SESSION_INDEX_STRING:-<no answer>})"
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
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs;
# the seeded rows are read back out of band, because a resolution that succeeds without reseeding
# exits 0 all the same and the failure would surface only as login failures in the next run
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
V1_USER_ROW_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v1" "SELECT COUNT(*) FROM melody_example_v1_user")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0' && [[ "${V1_USER_ROW_COUNT_STRING}" =~ ^[1-9][0-9]*$ ]]; then
    check_pass "resolving the v1 user repository reseeded the user directory (rows read out of band)"
else
    check_fail "the v1 user repository resolution did not restore the directory (status ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, rows ${V1_USER_ROW_COUNT_STRING:-<no answer>})"
fi

check_section_end "V1 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V1 JOURNAL DATABASE MIGRATIONS — the db:journal:* context family runs the journal set on postgres
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 JOURNAL DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# the journal is the v1 example's second live database: one process, the catalogue on mysql and the journal
# on postgres, each with its own migration set and its own command family. The rollback here drops the live
# journal table of melody_example_v1 on postgres — this major's own database, not the shared development
# one; the migrate after it puts the schema back, and the journal is append-only with no seeds, so an empty
# journal IS the restored state.
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

if printf '%s' "${V1_JOURNAL_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V1_JOURNAL_STATUS_OUTPUT_STRING}" | grep -q '20260907000002'; then
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
# V1 DATABASE RESET — example:db:reset restores both of this example's databases
# ---------------------------------------------------------------------------------------------------

check_section_start "V1 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

# THIS SECTION DESTROYS LIVE DATA, on both of the v1 example's databases: the catalog on mysql and the
# journal on postgres. It leaves them in the state a fresh volume holds, which is what the sections after
# it expect. The journal half is asserted through the application rather than out of band, the harness
# having no postgres reader: db:journal:status naming the set with nothing pending, over a bookkeeping the
# reset has just dropped and recreated, is the statement that the second set was applied again.

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'nothing was touched' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'melody_example_v1_catalog_journal'; then
    check_pass "v1 example:db:reset without --force names both sets and drops nothing"
else
    check_fail "the v1 reset refusal did not hold (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the run reports each step as it completes and names the database it ran on — host, port and schema — for
# the catalogue and for the journal alike, and clears the cache between the two: the half of "the state a
# fresh volume holds" the databases cannot carry
run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset --force 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
V1_BOOKKEEPING_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v1" "SELECT COUNT(*) FROM bun_migrations")"
V1_SEEDED_USER_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v1" "SELECT COUNT(*) FROM melody_example_v1_user")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'catalogue reset: the schema was dropped and recreated on mysql:3306/melody_example_v1' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'journal reset: the journal table was dropped and recreated on postgres:5432/melody_example_v1' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'cache cleared: the shared cache' \
    && [[ "1" = "${V1_BOOKKEEPING_COUNT_STRING}" ]] \
    && [[ "${V1_SEEDED_USER_COUNT_STRING}" =~ ^[1-9][0-9]*$ ]]; then
    check_pass "the v1 reset left one catalog bookkeeping row and a reseeded directory (${V1_SEEDED_USER_COUNT_STRING} accounts, read out of band)"
else
    check_fail "the v1 reset left ${V1_BOOKKEEPING_COUNT_STRING:-<no answer>} bookkeeping row(s) and ${V1_SEEDED_USER_COUNT_STRING:-<no answer>} account(s)"
fi

run_in_dev_capture "${V1_EXAMPLE_DIRECTORY_STRING}" "go run . db:journal:status 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q '0 pending' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q '20260907000002'; then
    check_pass "the v1 reset applied the journal set again over a bookkeeping it had just dropped"
else
    check_fail "the v1 journal set is not applied after the reset (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

check_section_end "V1 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

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

# THIS SECTION EMPTIES LIVE TABLES MID-FLIGHT: the rollback drops all six tables of the v2 example's own
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

if printf '%s' "${V2_STATUS_OUTPUT_STRING}" | grep -q '0 pending' && printf '%s' "${V2_STATUS_OUTPUT_STRING}" | grep -q '20260907000001'; then
    check_pass "v2 db:status reports the applied set by name with nothing pending"
else
    check_fail "v2 db:status does not report the applied set (${V2_STATUS_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:rollback 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -qi 'rolled back'; then
    check_pass "v2 db:rollback reverted the last group (the six live tables are dropped until the next step)"
else
    check_fail "v2 db:rollback did not report the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . db:migrate 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'applied 1 migration'; then
    check_pass "v2 db:migrate re-applied the schema the rollback reverted"
else
    check_fail "v2 db:migrate did not re-apply the reverted group (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the session index the sign-in doors keep each account under its cap with is part of the one schema step:
# read out of band, with the key that cascades an account's delete, since a table the migration declared
# but MySQL refused (a foreign key over another collation) would surface only as a 500 at the next sign-in
V2_SESSION_INDEX_STRING="$(e2e_mysql_scalar "melody_example_v2" "SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema = 'melody_example_v2' AND table_name = 'melody_example_v2_user_session' AND referenced_table_name = 'melody_example_v2_user' AND delete_rule = 'CASCADE'")"
if [[ "1" = "${V2_SESSION_INDEX_STRING}" ]]; then
    check_pass "the v2 session index is back with the schema, its rows cascading with their account (read out of band)"
else
    check_fail "the v2 session index or its cascading key is missing after the migrate (${V2_SESSION_INDEX_STRING:-<no answer>})"
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
# is the one deterministic door that reseeds it, which the login flow of the dev-supervised app needs;
# the seeded rows are read back out of band, because a resolution that succeeds without reseeding
# exits 0 all the same and the failure would surface only as login failures in the next run
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . debug:container service.example.user.repository >/dev/null 2>&1; echo status=\$?"
V2_USER_ROW_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v2" "SELECT COUNT(*) FROM melody_example_v2_user")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'status=0' && [[ "${V2_USER_ROW_COUNT_STRING}" =~ ^[1-9][0-9]*$ ]]; then
    check_pass "resolving the v2 user repository reseeded the user directory (rows read out of band)"
else
    check_fail "the v2 user repository resolution did not restore the directory (status ${RUN_IN_DEV_OUTPUT_STRING:-<empty>}, rows ${V2_USER_ROW_COUNT_STRING:-<no answer>})"
fi

check_section_end "V2 DATABASE MIGRATIONS" "${TAG_VALIDATE}" "e2e"

# ---------------------------------------------------------------------------------------------------
# V2 DATABASE RESET — example:db:reset restores this example's one database
# ---------------------------------------------------------------------------------------------------

check_section_start "V2 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

# THIS SECTION DESTROYS LIVE DATA and leaves the database in the state a fresh volume holds. This major
# keeps the journal beside the catalogue, so one set and one reset cover the whole schema.

run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'nothing was touched' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'melody_example_v2_catalog_journal'; then
    check_pass "v2 example:db:reset without --force names the set and drops nothing"
else
    check_fail "the v2 reset refusal did not hold (${RUN_IN_DEV_OUTPUT_STRING:-<empty>})"
fi

# the run reports each step as it completes, names the database it ran on — host, port and schema — and
# clears the cache last, the half of "the state a fresh volume holds" the database cannot carry
run_in_dev_capture "${V2_EXAMPLE_DIRECTORY_STRING}" "go run . example:db:reset --force 2>&1 | sed 's/\x1b\[[0-9;]*m//g'"
V2_BOOKKEEPING_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v2" "SELECT COUNT(*) FROM bun_migrations")"
V2_SEEDED_USER_COUNT_STRING="$(e2e_mysql_scalar "melody_example_v2" "SELECT COUNT(*) FROM melody_example_v2_user")"
if printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'database reset: the schema was dropped and recreated on mysql:3306/melody_example_v2' \
    && printf '%s' "${RUN_IN_DEV_OUTPUT_STRING}" | grep -q 'cache cleared: the shared cache' \
    && [[ "1" = "${V2_BOOKKEEPING_COUNT_STRING}" ]] \
    && [[ "${V2_SEEDED_USER_COUNT_STRING}" =~ ^[1-9][0-9]*$ ]]; then
    check_pass "the v2 reset left one bookkeeping row and a reseeded directory (${V2_SEEDED_USER_COUNT_STRING} accounts, read out of band)"
else
    check_fail "the v2 reset left ${V2_BOOKKEEPING_COUNT_STRING:-<no answer>} bookkeeping row(s) and ${V2_SEEDED_USER_COUNT_STRING:-<no answer>} account(s)"
fi

check_section_end "V2 DATABASE RESET" "${TAG_VALIDATE}" "e2e"

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
