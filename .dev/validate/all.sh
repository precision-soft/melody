#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

REPOSITORY_ROOT_DIRECTORY_STRING="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ "" = "${REPOSITORY_ROOT_DIRECTORY_STRING}" ]]; then
    SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    DEV_DIRECTORY_STRING="$(cd -P "${SCRIPT_DIRECTORY_STRING}/.." && pwd)"
    REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "${DEV_DIRECTORY_STRING}/.." && pwd)"
fi

. "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/utility.sh"

MODE_STRING="all"
SKIP_LIVE_BOOLEAN="false"
FRESH_BOOLEAN="false"

ARGUMENT_LIST=()
for ARGUMENT_STRING in "$@"; do
    if [[ "--fresh" = "${ARGUMENT_STRING}" ]]; then
        FRESH_BOOLEAN="true"
        continue
    fi
    ARGUMENT_LIST+=("${ARGUMENT_STRING}")
done
set -- "${ARGUMENT_LIST[@]+"${ARGUMENT_LIST[@]}"}"

if [[ "" = "${1-}" ]]; then
    :
elif [[ "-h" = "${1-}" ]]; then
    println "usage: all.sh [-h] [--all | --staged | --live | --e2e | --vulncheck | --compatibility | --apidiff] [--skip-live] [--fresh]"
    println ""
    println "  -h               show this help and exit"
    println "  --all            validate all modules, the live integration suites and the live e2e run (default)"
    println "  --staged         validate only modules with staged changes (the vulnerability check runs only when a go.mod or go.sum is staged, the compatibility and api surface checks when any go source, go.mod or go.sum outside the example applications and the dev harness is)"
    println "  --live           run only the live integration suites (mirrors the ci live job)"
    println "  --e2e            run only the live e2e harness and stack checks (mirrors the ci e2e job)"
    println "  --vulncheck      run only the govulncheck lane against the vulncheck baseline"
    println "  --compatibility  run only the lane that builds every integration against the version its go.mod pins"
    println "  --apidiff        run only the lane that compares the exported surface of every published module with its last tag"
    println "  --skip-live      leave the live suites and the e2e run out of --all, for a hand run with no backends"
    println "  --fresh          run every unit of --all, ignoring the unit cache, and record them again"
    println ""
    println "--all skips a unit (a module of a lane, or a whole lane) whose inputs are unchanged since it last ran green,"
    println "recorded under .temp/validate-cache/; the documentary lanes always run, and vulncheck is recorded per utc day"
    exit 0
elif [[ "--staged" = "${1-}" ]]; then
    MODE_STRING="staged"
elif [[ "--all" = "${1-}" ]]; then
    MODE_STRING="all"
elif [[ "--live" = "${1-}" ]]; then
    MODE_STRING="live"
elif [[ "--e2e" = "${1-}" ]]; then
    MODE_STRING="e2e"
elif [[ "--vulncheck" = "${1-}" ]]; then
    MODE_STRING="vulncheck"
elif [[ "--compatibility" = "${1-}" ]]; then
    MODE_STRING="compatibility"
elif [[ "--apidiff" = "${1-}" ]]; then
    MODE_STRING="apidiff"
elif [[ "--skip-live" = "${1-}" ]]; then
    MODE_STRING="all"
    SKIP_LIVE_BOOLEAN="true"
else
    fail "unknown flag: ${1}"
fi

if [[ "--skip-live" = "${2-}" ]]; then
    SKIP_LIVE_BOOLEAN="true"
fi

if [[ "true" = "${FRESH_BOOLEAN}" && "all" != "${MODE_STRING}" ]]; then
    fail "--fresh goes with --all or --skip-live only: the other modes keep no unit cache"
fi

SERVICE_NAME_STRING="dev"

acquire_validation_lock

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

GO_TAG_LIST_STRING_LIST=(
    ""
    "melody_env_embedded"
    "melody_static_embedded"
    "melody_env_embedded melody_static_embedded"
)

CONTAINER_ROOT_PATH="/app"

# the files a generator writes are not committed, and the v3 and v4 examples compile only with their generated wiring; the
# container writes them at start, but a constructor edited since would leave them stale, so every run writes them
# again before its lanes, inside the service the lanes run in
run_section "generated files" "${TAG_VALIDATE}" "go" -- \
    run_in_service_shell "${SERVICE_NAME_STRING}" "bash ${CONTAINER_ROOT_PATH}/.dev/generate.sh"

container_path_for() {
    local HOST_DIRECTORY_STRING="${1:?}"

    local RELATIVE_PATH_STRING="${HOST_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}}"
    if [[ "" = "${RELATIVE_PATH_STRING}" || "/" = "${RELATIVE_PATH_STRING}" ]]; then
        printf '%s' "${CONTAINER_ROOT_PATH}"
        return 0
    fi

    printf '%s%s' "${CONTAINER_ROOT_PATH}" "${RELATIVE_PATH_STRING}"
}

run_go_checks() {
    local COMPONENT_DIRECTORY_STRING="${1:?}"
    local COMPONENT_TITLE_STRING="${2:?}"

    local CONTAINER_DIRECTORY_STRING
    CONTAINER_DIRECTORY_STRING="$(container_path_for "${COMPONENT_DIRECTORY_STRING}")"

    local BATCH_COMMAND_LIST=()

    local TAGS_STRING
    for TAGS_STRING in "${GO_TAG_LIST_STRING_LIST[@]}"; do
        if [[ "" = "${TAGS_STRING}" ]]; then
            BATCH_COMMAND_LIST+=("cd ${CONTAINER_DIRECTORY_STRING} && go vet ./...")
            BATCH_COMMAND_LIST+=("cd ${CONTAINER_DIRECTORY_STRING} && go test ./...")
        else
            BATCH_COMMAND_LIST+=("cd ${CONTAINER_DIRECTORY_STRING} && go vet -tags '${TAGS_STRING}' ./...")
            BATCH_COMMAND_LIST+=("cd ${CONTAINER_DIRECTORY_STRING} && go test -tags '${TAGS_STRING}' ./...")
        fi
    done

    local MODULE_DIRECTORY_STRING="${COMPONENT_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
    if [[ "${COMPONENT_DIRECTORY_STRING}" = "${REPOSITORY_ROOT_DIRECTORY_STRING}" ]]; then
        MODULE_DIRECTORY_STRING="."
    fi

    run_cached_unit "go" "${MODULE_DIRECTORY_STRING}" "$(validation_cache_module_key "go" "${MODULE_DIRECTORY_STRING}" "${MODULE_DIRECTORY_STRING}")" \
        "${COMPONENT_TITLE_STRING}" "${TAG_VALIDATE}" "go" -- \
        run_batch_in_service_shell "${SERVICE_NAME_STRING}" "${BATCH_COMMAND_LIST[@]}"
}

LIVE_SERVICE_NAME_STRING_LIST=(
    "rabbitmq"
    "redis"
    "mysql"
    "postgres"
    "localstack"
)

LIVE_ENVIRONMENT_EXPORT_STRING="export AMQP_DSN='amqp://guest:guest@rabbitmq:5672/' REDIS_ADDRESS='redis:6379' MYSQL_DSN='melody:melody@tcp(mysql:3306)/melody_example' PGSQL_HOST='postgres' PGSQL_PORT='5432' PGSQL_DATABASE='melody_test' PGSQL_USER='melody' PGSQL_PASSWORD='melody' POSTGRES_DSN='postgres://melody:melody@postgres:5432/melody_test?sslmode=disable' MINIO_ENDPOINT='localstack:4566' MINIO_ACCESS_KEY='test' MINIO_SECRET_KEY='test'"


# every integration module is discovered rather than listed, so a module added later joins this lane without
# anyone remembering to register it — the list that had to be maintained by hand is exactly the one that gets
# forgotten. The suites are gated on their backend environment variables, so a module with no live test simply
# runs its ordinary tests again, and the detector runs over all of them because a race in an integration is
# caught by nothing else.
run_live_go_suites() {
    local UNIT_MODULE_LIST=()
    local UNIT_COMMAND_LIST=()

    local LIVE_MODULE_DIRECTORY_STRING
    while IFS= read -r LIVE_MODULE_DIRECTORY_STRING; do
        if [[ "" = "${LIVE_MODULE_DIRECTORY_STRING}" ]]; then
            continue
        fi

        local LIVE_MODULE_RELATIVE_PATH_STRING="${LIVE_MODULE_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"

        UNIT_MODULE_LIST+=("${LIVE_MODULE_RELATIVE_PATH_STRING}")
        UNIT_COMMAND_LIST+=("${LIVE_ENVIRONMENT_EXPORT_STRING} && cd ${CONTAINER_ROOT_PATH}/${LIVE_MODULE_RELATIVE_PATH_STRING} && go test -race -count=1 ./...")
    done < <(get_integration_module_directory_list)

    # the bookkeeping init of the examples whose journal lives on postgres (v1, v3 and v4; v2's is mysql-only) against a
    # live postgres: twelve connections creating the same bun tables at once, the race two replicas booting together
    # run; the examples are not integration modules, so they are named
    for LIVE_EXAMPLE_RELATIVE_PATH_STRING in .example v3/.example v4/.example; do
        UNIT_MODULE_LIST+=("${LIVE_EXAMPLE_RELATIVE_PATH_STRING}")
        UNIT_COMMAND_LIST+=("${LIVE_ENVIRONMENT_EXPORT_STRING} && cd ${CONTAINER_ROOT_PATH}/${LIVE_EXAMPLE_RELATIVE_PATH_STRING} && go test -race -count=1 -run TestInitializeMigrationBookkeepingSurvivesConcurrentCreatorsOnPostgres ./migration")
    done

    local UNIT_KEY_LIST=()
    local BACKEND_NEEDED_BOOLEAN="false"
    local UNIT_INDEX_INTEGER
    for UNIT_INDEX_INTEGER in "${!UNIT_MODULE_LIST[@]}"; do
        local UNIT_KEY_STRING=""
        if [[ "true" = "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
            UNIT_KEY_STRING="$(validation_cache_module_key "live" "${UNIT_COMMAND_LIST[${UNIT_INDEX_INTEGER}]}" "${UNIT_MODULE_LIST[${UNIT_INDEX_INTEGER}]}")"
            UNIT_KEY_STRING="$(validation_cache_key "${UNIT_KEY_STRING}" "backend ${VALIDATION_CACHE_BACKEND_IMAGE_DIGEST_STRING}")"
        fi
        UNIT_KEY_LIST+=("${UNIT_KEY_STRING}")
        if ! validation_cache_has "${UNIT_KEY_STRING}"; then
            BACKEND_NEEDED_BOOLEAN="true"
        fi
    done

    section_start "melody live integration suites (mirrors the ci live job)" "${TAG_VALIDATE}" "go"

    if [[ "true" = "${BACKEND_NEEDED_BOOLEAN}" ]]; then
        docker_compose up -d --wait "${LIVE_SERVICE_NAME_STRING_LIST[@]}"
    fi

    for UNIT_INDEX_INTEGER in "${!UNIT_MODULE_LIST[@]}"; do
        run_cached_unit "live" "${UNIT_MODULE_LIST[${UNIT_INDEX_INTEGER}]}" "${UNIT_KEY_LIST[${UNIT_INDEX_INTEGER}]}" \
            "melody live suite: ${UNIT_MODULE_LIST[${UNIT_INDEX_INTEGER}]}" "${TAG_VALIDATE}" "go" -- \
            run_batch_in_service_shell "${SERVICE_NAME_STRING}" "${UNIT_COMMAND_LIST[${UNIT_INDEX_INTEGER}]}"
    done

    section_end "melody live integration suites (mirrors the ci live job)" "success" "${TAG_VALIDATE}" "go"
}

# the race detector over the concurrency-carrying packages of the core majors and the cron runner, mirroring
# the ci race job. The build-tag matrix above runs these without it, so a lost mutex or an unsynchronized memo
# stays green there: these packages hold goroutines that outlive a call (the lazy service memo, the signal
# watcher, the smtp cancellation watcher, the http client's redirect policy reading the header map, the
# logger's shared writer, the message bus dispatch, the cron runner's parallel dispatch), and only the
# detector sees them race — several of those tests assert nothing at all, so without this lane they can only
# ever pass. validation carries the process-lifetime parse and constraint memos that concurrent requests share,
# and openapi the schema memo, so both belong here too. session, http and internal carry the state a request
# actually touches — the storages every concurrent request reads and writes, the middleware chain, the deep copy
# that guards one request's data from another's — and their suites hold tests written specifically to catch a
# race, with a goroutine and a wait group, which without this lane could only ever pass. exception belongs here
# for the same reason: a creation failure the container memoizes is handed to the owner and to every waiter at
# once, so one error's context map is written in place while another goroutine iterates it, and the mutex that
# orders them is only observable to the detector. bunorm's manager registry belongs here for the same class of
# reason and needs no database to show it: it coalesces concurrent opens of one definition onto a single dial,
# publishes the result to parked waiters through a channel, and races a Close against an open still in flight —
# and its suite holds the tests written for exactly those windows, which without this lane could only ever pass.
# bag belongs here because its readers hold a read lock against its writers, which only the detector can see.
# No service containers needed; the three add some four seconds per major.
RACE_SUITE_SPECIFICATION_STRING_LIST=(
    ". ./bag/... ./cache/... ./clock/... ./container/... ./application/... ./config/... ./event/... ./exception/... ./httpclient/... ./logging/... ./cli/... ./validation/... ./session/... ./security/... ./http/... ./internal/..."
    "v2 ./bag/... ./cache/... ./clock/... ./container/... ./application/... ./config/... ./event/... ./exception/... ./httpclient/... ./logging/... ./cli/... ./validation/... ./session/... ./security/... ./http/... ./internal/..."
    "v3 ./bag/... ./cache/... ./clock/... ./container/... ./application/... ./config/... ./event/... ./exception/... ./httpclient/... ./logging/... ./cli/... ./mailer/... ./lock/... ./messagebus/... ./validation/... ./openapi/... ./session/... ./security/... ./http/... ./internal/..."
    "v4 ./bag/... ./cache/... ./clock/... ./container/... ./application/... ./config/... ./event/... ./exception/... ./httpclient/... ./logging/... ./cli/... ./mailer/... ./lock/... ./messagebus/... ./validation/... ./openapi/... ./session/... ./security/... ./http/... ./internal/..."
    "integrations/cron ./..."
    "integrations/cron/v2 ./..."
    "integrations/cron/v3 ./..."
    "integrations/cron/v4 ./..."
    "integrations/bunorm ./..."
    "integrations/bunorm/v2 ./..."
    "integrations/bunorm/v3 ./..."
    "integrations/bunorm/v4 ./..."
)

run_race_go_suites() {
    section_start "melody race detector on the concurrency-carrying core packages (mirrors the ci race job)" "${TAG_VALIDATE}" "go"

    local RACE_SUITE_SPECIFICATION_STRING
    for RACE_SUITE_SPECIFICATION_STRING in "${RACE_SUITE_SPECIFICATION_STRING_LIST[@]}"; do
        local RACE_MODULE_RELATIVE_PATH_STRING="${RACE_SUITE_SPECIFICATION_STRING%% *}"
        local RACE_PACKAGE_LIST_STRING="${RACE_SUITE_SPECIFICATION_STRING#* }"
        local RACE_COMMAND_STRING="cd ${CONTAINER_ROOT_PATH}/${RACE_MODULE_RELATIVE_PATH_STRING} && go test -race -count=1 ${RACE_PACKAGE_LIST_STRING}"

        run_cached_unit "race" "${RACE_MODULE_RELATIVE_PATH_STRING}" "$(validation_cache_module_key "race" "${RACE_COMMAND_STRING}" "${RACE_MODULE_RELATIVE_PATH_STRING}")" \
            "melody race detector: ${RACE_MODULE_RELATIVE_PATH_STRING}" "${TAG_VALIDATE}" "go" -- \
            run_batch_in_service_shell "${SERVICE_NAME_STRING}" "${RACE_COMMAND_STRING}"
    done

    section_end "melody race detector on the concurrency-carrying core packages (mirrors the ci race job)" "success" "${TAG_VALIDATE}" "go"
}

# the e2e harness module is deliberately outside go.work (it builds GOWORK=off against the local replaces),
# so no other lane compiles it: a break in the harness — or a stale framework pin in its go.mod — would
# otherwise surface only when someone runs run.sh or stack.sh by hand. Its own unit tests run here too:
# they cover the parsing the harness does before it ever reaches a backend — the section catalogue, the
# server-sent-event frame reader, the prometheus exposition reader, the token minters — and nothing else
# executes them, so without this lane a harness assertion could stop working and every live run would
# still report a pass.
run_e2e_harness_checks() {
    if [[ ! -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/go.mod" ]]; then
        fail "the e2e harness module is missing: .dev/e2e/go.mod does not exist, so the harness lane cannot run. It is not optional — reporting success here would mean the whole lane silently contributed nothing"
    fi

    run_cached_unit "e2e-harness" ".dev/e2e" "$(validation_cache_module_key "e2e-harness" ".dev/e2e" ".dev/e2e" ".dev/validate/typecheck")" \
        "melody e2e harness module (.dev/e2e, GOWORK=off)" "${TAG_VALIDATE}" "go" -- \
        run_batch_in_service_shell "${SERVICE_NAME_STRING}" \
        "cd ${CONTAINER_ROOT_PATH}/.dev/e2e && GOWORK=off go vet ./..." \
        "cd ${CONTAINER_ROOT_PATH}/.dev/e2e && GOWORK=off go test -count=1 ./..." \
        "cd ${CONTAINER_ROOT_PATH}/.dev/validate/typecheck && GOWORK=off go vet ./..."
}

# the package documents against the code of every major. The three majors are near-copies whose documents
# drifted independently, and nothing compared them: a symbol one major documents while another ships it
# undocumented is invisible to every compiler and every test. Reads the tree only, so it needs no container
# and costs seconds.
run_documentation_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/documentation.sh" ]]; then
        fail "the documentation check is missing or not executable: .dev/validate/documentation.sh. It is not optional — the ci job invokes it directly, so a skip here reports a pass locally against a lane that fails in ci; restore it or chmod +x it"
    fi

    run_section "melody package documentation against every major (mirrors the ci documentation job)" "${TAG_VALIDATE}" "docs" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/documentation.sh"
}

# one published major against the next, after the import path is rewritten. v1 and v2 are the same framework
# twice over — the port was a copy with the module path rewritten, not a translation — and they are separate
# modules, so no build, no vet run and no test binary ever sees them together: a fix that lands on one major
# and is forgotten on the other compiles, tests and reads correctly on both sides, and nothing else in this
# script can tell. Reads the tree only, so it needs no container and costs seconds.
run_parity_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/parity.sh" ]]; then
        fail "the parity check is missing or not executable: .dev/validate/parity.sh. It is not optional — the ci job invokes it directly, so a skip here reports a pass locally against a lane that fails in ci, and it is the only lane that compares one major against another at all; restore it or chmod +x it"
    fi

    run_section "melody v1 against v2 after the import path rewrite (mirrors the ci parity job)" "${TAG_VALIDATE}" "parity" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/parity.sh"
}

# what every document of a major cites, against what that major declares. The documentation check above asks
# whether a symbol a major ships is documented; it structurally cannot ask the opposite, and the opposite is
# where a document rots — an upgrade note that keeps naming a method the code no longer has reads as correct
# to every check in this file. It also reaches the documents nothing else does: the per-integration README
# and CHANGELOG are outside `<major>/.documentation`, so neither the documentation check nor the parity check
# has ever looked at them.
#
# The --samples pass is deliberately not passed here. It type-checks the fenced go blocks that are whole
# programs and therefore needs a Go toolchain, which on this project lives in the development container,
# while this lane runs on the host like its two neighbours. Run it there when documents change:
# `./dc exec dev bash -lc '.dev/validate/citation.sh --samples'`.
run_citation_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/citation.sh" ]]; then
        fail "the citation check is missing or not executable: .dev/validate/citation.sh. It is not optional — the ci job invokes it directly, so a skip here reports a pass locally against a lane that fails in ci, and it is the only lane that asks whether what the documents claim exists at all; restore it or chmod +x it"
    fi

    run_section "melody document citations against the code of every major (mirrors the ci citation job)" "${TAG_VALIDATE}" "citation" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/citation.sh"
}

# the shape of every changelog block: which sections it opens, in which order, and whether the two published
# majors file the same entry under the same one. The three checks above read what a document CLAIMS; none of
# them reads a heading, so a session that opened a second `### Fixed` at the head of a block instead of
# writing into the one already there left half a cycle where no reader of that block would find it — which
# is what ten of the fourteen `[Unreleased]` blocks of the published majors were measured doing. Reads the
# tree only, so it needs no container and costs seconds.
run_changelog_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/changelog.sh" ]]; then
        fail "the changelog check is missing or not executable: .dev/validate/changelog.sh. It is not optional — the ci job invokes it directly, so a skip here reports a pass locally against a lane that fails in ci, and it is the only lane that reads a changelog heading at all; restore it or chmod +x it"
    fi

    run_section "melody changelog block shape across every major (mirrors the ci changelog job)" "${TAG_VALIDATE}" "changelog" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/changelog.sh"
}

# govulncheck over every module, gated on the REACHABLE findings against .dev/validate/vulncheck.baseline.
# No other lane asks the question: vet, the tests and the race detector judge the code this repository
# writes, and the documentary bands judge what it claims — none of them reads a security advisory, so a
# vulnerable function this code actually calls is green everywhere else by construction. Needs the dev
# container (the scan runs against the toolchain that builds) and the network for the vulnerability
# database, which is why the staged mode below runs it only when a staged change moves a go.mod or go.sum
# — the one commit class that can change the answer from the tree's side; the toolchain side moves with
# the image, and --all always asks.
run_vulncheck_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/vulncheck.sh" ]]; then
        fail "the vulnerability check is missing or not executable: .dev/validate/vulncheck.sh. It is not optional — it is the only lane that reads a security advisory at all; restore it or chmod +x it"
    fi

    local UNIT_KEY_STRING=""
    if [[ "true" = "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
        UNIT_KEY_STRING="$(validation_cache_key "lane vulncheck" "day $(date -u +%Y-%m-%d)" \
            "$(validation_cache_object_list .dev/validate/vulncheck.sh .dev/validate/vulncheck.baseline .dev/validate/compatibility.baseline)" \
            "$(cat "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/module-digest.list")" \
            "tags $(validation_cache_tag_digest)")"
    fi

    run_cached_unit "vulncheck" "every module" "${UNIT_KEY_STRING}" \
        "melody govulncheck over every module against the vulncheck baseline" "${TAG_VALIDATE}" "vulncheck" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/vulncheck.sh"
}

validation_cache_published_lane_key() {
    local LANE_STRING="${1:?}"

    if [[ "true" != "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
        return 0
    fi

    validation_cache_key "lane ${LANE_STRING}" \
        "$(validation_cache_object_list ".dev/validate/${LANE_STRING}.sh" ".dev/validate/${LANE_STRING}.baseline")" \
        "$(cat "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/module-digest.list")" \
        "tags $(validation_cache_tag_digest)"
}

# every integration module against the framework version its own go.mod PINS, rather than against the
# local tree. Every Go lane above builds with go.work active, and a workspace substitutes the LOCAL module
# for the dependency a go.mod declares — so none of them has ever compiled an integration against the
# version it publishes itself as needing, which is exactly what `go get` resolves for a consumer. Green
# under the workspace is a DEVELOPMENT check; this is the PUBLISHING one, and they are two sections on
# purpose. The lane splits its failures by cause rather than counting them: a module naming something the
# pinned tag does not have is the normal state of an unreleased branch and dies when the release train
# raises the pin, while a name that exists at the tag and changed shape is a backwards-incompatible change
# no pin bump answers. Needs the dev container for the toolchain and the network on a cold module cache.
run_compatibility_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/compatibility.sh" ]]; then
        fail "the compatibility check is missing or not executable: .dev/validate/compatibility.sh. It is not optional — it is the only lane that builds a published module against the version it declares, so a skip here reports a pass for the one class of defect every other lane is structurally blind to; restore it or chmod +x it"
    fi

    run_cached_unit "compatibility" "every integration" "$(validation_cache_published_lane_key compatibility)" \
        "melody integration modules against the version each go.mod pins (GOWORK=off)" "${TAG_VALIDATE}" "compatibility" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/compatibility.sh"
}

# the exported surface of every published module against the surface of its last tag, through apidiff.
# The lane above asks the consumer's question — does what I publish compile against what I declare —
# and this one asks the publisher's: what moved in the API since the tag, a signature, a removed symbol,
# a constant's value, an interface a third party implements, and what was added. Every difference is
# filed with a class and a disposition in .dev/validate/apidiff.baseline, so the list a release manager
# decides on before a tag is measured rather than remembered. Needs the dev container for apidiff and
# the network on a cold module cache, because the old side is fetched from the proxy.
run_apidiff_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/apidiff.sh" ]]; then
        fail "the api surface check is missing or not executable: .dev/validate/apidiff.sh. It is not optional — it is the only lane that compares what a module exports with what its last tag exported, so a skip here reports a pass for exactly the class of change a release has to decide on; restore it or chmod +x it"
    fi

    run_cached_unit "apidiff" "every published module" "$(validation_cache_published_lane_key apidiff)" \
        "melody exported surface of every published module against its last tag (apidiff)" "${TAG_VALIDATE}" "apidiff" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/apidiff.sh"
}

# the two live e2e scripts. Every other lane compiles the harness and runs its unit tests; nothing until here
# actually drives a booted application over the wire, and that is the only place a whole class of defect shows
# up at all — a middleware ordering that only matters once a real request traverses the chain, a route the
# router registers but never answers, a session cookie that never reaches the wire. The scripts are host
# scripts: each execs into the dev container itself, so this lane brings the stack up and hands off. mailpit
# and prometheus join the live backends because the mail and metric sections scrape them directly, the
# load balancer because a stack check asserts what it routes, and the otel collector because the v3 example
# EXPORTS to it — a span exporter with nowhere to send fails the application's own close with a context
# deadline, so a collector that happened to be down failed this lane on ambient state rather than on code.
E2E_SERVICE_NAME_STRING_LIST=(
    "rabbitmq"
    "redis"
    "mysql"
    "postgres"
    "localstack"
    "mailpit"
    "prometheus"
    "otel-collector"
    # the v1 and v2 example applications: THREE HOSTS asks the load balancer for each major's vhost, so
    # all three supervised applications have to be up, not only the v3 one the dev service runs
    "dev-v1"
    "dev-v2"
    "load-balancer"
)

run_e2e_live_checks() {
    local E2E_SCRIPT_PATH_STRING
    for E2E_SCRIPT_PATH_STRING in \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/run.sh" \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/stack.sh"; do
        if [[ ! -x "${E2E_SCRIPT_PATH_STRING}" ]]; then
            fail "the live e2e script is missing or not executable: ${E2E_SCRIPT_PATH_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}. It is not optional — this is the only lane that drives a booted application over the wire, so a skip here reports a pass for the class of defect nothing else can see; restore it or chmod +x it"
        fi
    done

    local RUN_KEY_STRING=""
    local STACK_KEY_STRING=""
    if [[ "true" = "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
        local E2E_INPUT_STRING
        E2E_INPUT_STRING="$(validation_cache_e2e_input_list)"
        RUN_KEY_STRING="$(validation_cache_key "lane e2e" "unit run.sh" "${E2E_INPUT_STRING}")"
        STACK_KEY_STRING="$(validation_cache_key "lane e2e" "unit stack.sh" "${E2E_INPUT_STRING}")"
    fi

    if ! validation_cache_has "${RUN_KEY_STRING}" || ! validation_cache_has "${STACK_KEY_STRING}"; then
        docker_compose up -d --wait "${E2E_SERVICE_NAME_STRING_LIST[@]}"
    fi

    run_cached_unit "e2e" "run.sh" "${RUN_KEY_STRING}" \
        "melody e2e harness live run (.dev/e2e/run.sh)" "${TAG_VALIDATE}" "e2e" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/run.sh"

    run_cached_unit "e2e" "stack.sh" "${STACK_KEY_STRING}" \
        "melody e2e stack checks (.dev/e2e/stack.sh)" "${TAG_VALIDATE}" "e2e" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/stack.sh"
}

validation_cache_e2e_input_list() {
    local EXAMPLE_DIRECTORY_LIST=()

    local EXAMPLE_DIRECTORY_STRING
    while IFS= read -r EXAMPLE_DIRECTORY_STRING; do
        EXAMPLE_DIRECTORY_LIST+=("${EXAMPLE_DIRECTORY_STRING}")
    done < <(
        sed -nE 's#.*MELODY_DEV_EXAMPLE_DIR:[[:space:]]*(\$\{MELODY_DEV_EXAMPLE_DIR:-)?/app/([^}[:space:]]+).*#\2#p' "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/docker/docker-compose.yml"
        local MAJOR_STRING
        for MAJOR_STRING in $(sed -nE 's/^MELODY_E2E_MAJORS="\$\{MELODY_E2E_MAJORS-([0-9 ]*)\}"$/\1/p' "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/common.sh" | tr ' ' '\n'); do
            if [[ "1" = "${MAJOR_STRING}" ]]; then
                printf '.example\n'
            else
                printf 'v%s/.example\n' "${MAJOR_STRING}"
            fi
        done
    )

    if [[ 0 -eq ${#EXAMPLE_DIRECTORY_LIST[@]} ]]; then
        fail "the live e2e reads no example directory from the compose file or the harness defaults, so its unit key would name none of the applications it drives"
    fi

    validation_cache_closure_digest_list ".dev/e2e" "${EXAMPLE_DIRECTORY_LIST[@]}"
    validation_cache_object_list ".assets"
    printf 'backend %s\n' "${VALIDATION_CACHE_BACKEND_IMAGE_DIGEST_STRING}"

    local VARIABLE_NAME_STRING
    for VARIABLE_NAME_STRING in $(cat "${REPOSITORY_ROOT_DIRECTORY_STRING}"/.dev/e2e/*.sh | grep -oE '\$\{?[A-Z_][A-Z0-9_]*' | tr -d '${' | sort -u); do
        local VARIABLE_VALUE_STRING
        if VARIABLE_VALUE_STRING="$(printenv "${VARIABLE_NAME_STRING}")"; then
            printf 'environment %s set %s\n' "${VARIABLE_NAME_STRING}" "${VARIABLE_VALUE_STRING}"
        else
            printf 'environment %s unset\n' "${VARIABLE_NAME_STRING}"
        fi
    done
}

# the proof a FULL green run leaves behind, for the pre-push hook to verify in milliseconds instead of
# re-running the gate while git holds an open connection the server times out. The stamp names the tree
# hash of the exact worktree content this run validated — any byte changed afterwards computes a different
# hash and the stamp stops matching, so the hook can never pass on content the gate did not see. Only the
# full --all run writes it: --skip-live and the partial modes prove less than what a push claims. The hash
# is taken at the START of the run and checked again at the END: a run takes long enough to edit under,
# and an edit made mid-run was validated by no lane before it — stamping the end-state would certify
# content the gate never saw, so a run whose worktree moved finishes green and stamps nothing.
VALIDATION_STAMP_FILE_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.temp/validate-stamp"
VALIDATION_START_TREE_HASH_STRING=""
VALIDATION_TREE_MOVED_BOOLEAN="false"

record_validation_start_tree_hash() {
    local IGNORED_GO_FILE_LIST_STRING
    IGNORED_GO_FILE_LIST_STRING="$(ignored_go_files_in_packages)"
    if [[ "" != "${IGNORED_GO_FILE_LIST_STRING}" ]]; then
        fail "ignored go files sit inside the module trees, which the lanes would compile and the stamp would not see — remove them first: $(printf '%s' "${IGNORED_GO_FILE_LIST_STRING}" | tr '\n' ' ')"
    fi

    if ! VALIDATION_START_TREE_HASH_STRING="$(compute_worktree_tree_hash)"; then
        VALIDATION_START_TREE_HASH_STRING=""
        warning "could not compute the worktree hash at the start of the run — no validation stamp will be written"
    fi
}

e2e_override_variable_list() {
    local VARIABLE_NAME_STRING
    for VARIABLE_NAME_STRING in $(sed -nE 's/^[[:space:]]*(export[[:space:]]+)?([A-Z_][A-Z0-9_]*)="\$\{([A-Z_][A-Z0-9_]*)-.*/\2 \3/p' "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/common.sh" | awk '$1 == $2 { print $1 }'); do
        if printenv "${VARIABLE_NAME_STRING}" >/dev/null; then
            printf '%s\n' "${VARIABLE_NAME_STRING}"
        fi
    done
}

write_validation_stamp() {
    if [[ "" = "${VALIDATION_START_TREE_HASH_STRING}" ]]; then
        return 0
    fi

    local WORKTREE_TREE_HASH_STRING
    if ! WORKTREE_TREE_HASH_STRING="$(compute_worktree_tree_hash)"; then
        warning "could not compute the worktree hash — no validation stamp written, the pre-push hook will ask for a fresh full run"
        return 0
    fi

    local IGNORED_GO_FILE_LIST_STRING
    IGNORED_GO_FILE_LIST_STRING="$(ignored_go_files_in_packages)"
    if [[ "" != "${IGNORED_GO_FILE_LIST_STRING}" ]]; then
        warning "ignored go files appeared inside the module trees during the run — no validation stamp written: $(printf '%s' "${IGNORED_GO_FILE_LIST_STRING}" | tr '\n' ' ')"
        return 0
    fi

    if [[ "${VALIDATION_START_TREE_HASH_STRING}" != "${WORKTREE_TREE_HASH_STRING}" ]]; then
        warning "the worktree changed while the run validated it (${VALIDATION_START_TREE_HASH_STRING} at start, ${WORKTREE_TREE_HASH_STRING} now) — no validation stamp written, run the full gate again on the settled tree"
        return 0
    fi

    local E2E_OVERRIDE_LIST_STRING
    E2E_OVERRIDE_LIST_STRING="$(e2e_override_variable_list)"
    if [[ "" != "${E2E_OVERRIDE_LIST_STRING}" ]]; then
        warning "the live e2e ran with its defaults overridden ($(printf '%s' "${E2E_OVERRIDE_LIST_STRING}" | tr '\n' ' ')) — a narrowed or redirected e2e proves less than a push claims, so no validation stamp written"
        return 0
    fi

    if [[ "true" = "${VALIDATION_TREE_MOVED_BOOLEAN}" ]]; then
        warning "the worktree moved during the run and came back — a unit validated content the run did not start on, so no validation stamp written, run the full gate again on the settled tree"
        return 0
    fi

    mkdir -p "$(dirname "${VALIDATION_STAMP_FILE_PATH}")"
    printf '%s %s\n' "${WORKTREE_TREE_HASH_STRING}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${VALIDATION_STAMP_FILE_PATH}"
    info "validation stamp written for worktree tree ${WORKTREE_TREE_HASH_STRING}"
}

VALIDATION_CACHE_DIRECTORY_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.temp/validate-cache"
VALIDATION_CACHE_WORK_DIRECTORY_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.temp/validate-cache-work"
VALIDATION_CACHE_FORMAT_STRING="2"
VALIDATION_CACHE_RETENTION_DAYS_INTEGER="14"
VALIDATION_CACHE_ENABLED_BOOLEAN="false"
VALIDATION_CACHE_INFRASTRUCTURE_DIGEST_STRING=""
VALIDATION_CACHE_BACKEND_IMAGE_DIGEST_STRING=""
VALIDATION_UNIT_SUMMARY_LIST=()

VALIDATION_CACHE_INFRASTRUCTURE_PATH_LIST=(
    ".dev/docker"
    ".dev/generate.sh"
    ".dev/run-batch.sh"
    ".dev/utility.sh"
    ".dev/validate/all.sh"
    "go.work"
    "go.work.sum"
)

validation_cache_digest() {
    sha256sum | cut -d ' ' -f 1
}

validation_cache_object_list() {
    local OBJECT_PATH_STRING
    for OBJECT_PATH_STRING in "$@"; do
        printf '%s %s\n' "${OBJECT_PATH_STRING}" "$(git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" rev-parse --verify -q "${VALIDATION_START_TREE_HASH_STRING}:${OBJECT_PATH_STRING}" || printf 'absent')"
    done
}

validation_cache_embed_expression_list() {
    { git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" grep -I -E '^[[:space:]]*//go:embed[[:space:]]' "${VALIDATION_START_TREE_HASH_STRING}" -- '*.go' || true; } |
        awk -v tree="${VALIDATION_START_TREE_HASH_STRING}:" '
            {
                rest = substr($0, length(tree) + 1)
                separator = index(rest, ":")
                path = substr(rest, 1, separator - 1)
                directive = substr(rest, separator + 1)
                sub(/^[[:space:]]*\/\/go:embed[[:space:]]+/, "", directive)
                gsub(/"/, "", directive)
                directory = path
                if (sub(/\/[^\/]*$/, "", directory)) { prefix = directory "/" } else { prefix = "" }
                count = split(directive, pattern_list, /[[:space:]]+/)
                for (i = 1; i <= count; i++) {
                    pattern = pattern_list[i]
                    sub(/^all:/, "", pattern)
                    if ("" == pattern) { continue }
                    expression = prefix pattern
                    gsub(/[.+(){}|^$]/, "\\\\&", expression)
                    gsub(/\*/, "[^/]*", expression)
                    gsub(/\?/, "[^/]", expression)
                    print "^" expression "(/|$)"
                }
            }
        '
}

validation_cache_path_matches_embed() {
    awk 'NR == FNR { expression_list[++expression_count] = $0; next } { for (i = 1; i <= expression_count; i++) { if ($0 ~ expression_list[i]) { print; next } } }' "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/embed-expression.list" -
}

validation_cache_markdown_is_read_by_go() {
    if git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" grep -q -I -E '\.md["`]' "${VALIDATION_START_TREE_HASH_STRING}" -- '*.go'; then
        return 0
    fi

    [[ "" != "$(validation_cache_path_matches_embed < "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/markdown.list")" ]]
}

validation_cache_tag_digest() {
    git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" tag -l --format='%(refname) %(objectname)' | validation_cache_digest
}

prepare_validation_cache() {
    if [[ "" = "${VALIDATION_START_TREE_HASH_STRING}" ]]; then
        warning "no worktree hash — the unit cache is off for this run, every unit runs"
        return 0
    fi

    mkdir -p "${VALIDATION_CACHE_DIRECTORY_PATH}"
    find "${VALIDATION_CACHE_DIRECTORY_PATH}" -type f -mtime "+${VALIDATION_CACHE_RETENTION_DAYS_INTEGER}" -delete 2>/dev/null || true

    rm -rf "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}"
    mkdir -p "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}"

    local WORK_PATH="${VALIDATION_CACHE_WORK_DIRECTORY_PATH}"

    git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" ls-tree -r "${VALIDATION_START_TREE_HASH_STRING}" > "${WORK_PATH}/tree.list"
    awk -F '\t' '$2 ~ /\.md$/ { print $2 }' "${WORK_PATH}/tree.list" > "${WORK_PATH}/markdown.list"
    validation_cache_embed_expression_list > "${WORK_PATH}/embed-expression.list"

    (cd "${REPOSITORY_ROOT_DIRECTORY_STRING}" && git ls-files -o -i --exclude-standard | grep -vE '^(\.dev-data|\.temp)/|/\.temp/' || true) |
        validation_cache_path_matches_embed > "${WORK_PATH}/ignored-embedded.list"
    if [[ -s "${WORK_PATH}/ignored-embedded.list" ]]; then
        (cd "${REPOSITORY_ROOT_DIRECTORY_STRING}" && git hash-object --stdin-paths < "${WORK_PATH}/ignored-embedded.list") |
            paste - "${WORK_PATH}/ignored-embedded.list" |
            awk -F '\t' '{ print "ignored blob " $1 "\t" $2 }' >> "${WORK_PATH}/tree.list"
    fi

    local EXCLUDE_MARKDOWN_STRING="1"
    if validation_cache_markdown_is_read_by_go; then
        EXCLUDE_MARKDOWN_STRING="0"
        warning "a go file reads a markdown file, so the documents stay in the module digests and a document edit re-runs its module"
    fi

    awk -F '\t' '$2 ~ /(^|\/)go\.mod$/ { directory = $2; if (!sub(/\/go\.mod$/, "", directory)) { directory = "." } print length(directory) "\t" directory }' "${WORK_PATH}/tree.list" |
        sort -rn |
        cut -f 2 > "${WORK_PATH}/module.list"

    awk -F '\t' -v exclude_markdown="${EXCLUDE_MARKDOWN_STRING}" '
        NR == FNR { module_list[++module_count] = $0; next }
        {
            path = $2
            split($1, meta, " ")
            if ("1" == exclude_markdown && "ignored" != meta[1] && path ~ /\.md$/) { next }
            owner = "."
            for (i = 1; i <= module_count; i++) {
                if ("." != module_list[i] && 1 == index(path, module_list[i] "/")) { owner = module_list[i]; break }
            }
            if ("." == owner && path ~ /^\.(dev|github)\//) { owner = "-" }
            print owner "\t" meta[1] " " meta[3] " " path
        }
    ' "${WORK_PATH}/module.list" "${WORK_PATH}/tree.list" > "${WORK_PATH}/owned.list"

    : > "${WORK_PATH}/module-digest.list"
    : > "${WORK_PATH}/module-path.list"
    : > "${WORK_PATH}/module-requirement.list"

    local MODULE_DIRECTORY_STRING
    while IFS= read -r MODULE_DIRECTORY_STRING; do
        printf '%s %s\n' "${MODULE_DIRECTORY_STRING}" "$(awk -F '\t' -v owner="${MODULE_DIRECTORY_STRING}" '$1 == owner { print $2 }' "${WORK_PATH}/owned.list" | validation_cache_digest)" >> "${WORK_PATH}/module-digest.list"

        local GO_MOD_PATH_STRING="${MODULE_DIRECTORY_STRING}/go.mod"
        if [[ "." = "${MODULE_DIRECTORY_STRING}" ]]; then
            GO_MOD_PATH_STRING="go.mod"
        fi

        git -C "${REPOSITORY_ROOT_DIRECTORY_STRING}" show "${VALIDATION_START_TREE_HASH_STRING}:${GO_MOD_PATH_STRING}" |
            awk -v directory="${MODULE_DIRECTORY_STRING}" -v path_file="${WORK_PATH}/module-path.list" -v requirement_file="${WORK_PATH}/module-requirement.list" '
                /^module[ \t]/ { print $2 " " directory >> path_file; next }
                /^require[ \t]*\(/ { block = 1; next }
                /^\)/ { block = 0; next }
                /^require[ \t]+github\.com\/precision-soft\/melody/ { print directory " " $2 >> requirement_file; next }
                1 == block && $1 ~ /^github\.com\/precision-soft\/melody/ { print directory " " $1 >> requirement_file }
            '
    done < "${WORK_PATH}/module.list"

    local DEV_CONTAINER_IMAGE_STRING
    DEV_CONTAINER_IMAGE_STRING="$(docker inspect --format '{{.Image}}' "$(docker_compose_no_log ps -q "${SERVICE_NAME_STRING}")" 2>/dev/null || printf 'absent')"

    VALIDATION_CACHE_INFRASTRUCTURE_DIGEST_STRING="$({
        printf 'format %s\n' "${VALIDATION_CACHE_FORMAT_STRING}"
        printf 'markdown excluded %s\n' "${EXCLUDE_MARKDOWN_STRING}"
        printf 'image %s\n' "${DEV_CONTAINER_IMAGE_STRING}"
        validation_cache_object_list "${VALIDATION_CACHE_INFRASTRUCTURE_PATH_LIST[@]}"
        awk -F '\t' '$2 ~ /(^|\/)go\.(mod|sum)$/' "${WORK_PATH}/tree.list"
    } | validation_cache_digest)"

    ensure_docker_env_files >/dev/null
    local BACKEND_IMAGE_NAME_LIST_STRING
    if ! BACKEND_IMAGE_NAME_LIST_STRING="$(docker_compose_no_log --profile '*' config --images)" || [[ "" = "${BACKEND_IMAGE_NAME_LIST_STRING}" ]]; then
        fail "the compose file names no image, so the live and e2e unit keys could not name the backends they run against"
    fi

    VALIDATION_CACHE_BACKEND_IMAGE_DIGEST_STRING="$(sort -u <<<"${BACKEND_IMAGE_NAME_LIST_STRING}" |
        while IFS= read -r IMAGE_NAME_STRING; do
            printf '%s %s\n' "${IMAGE_NAME_STRING}" "$(docker image inspect --format '{{.Id}}' "${IMAGE_NAME_STRING}" 2>/dev/null || printf 'absent')"
        done | validation_cache_digest)"

    VALIDATION_CACHE_ENABLED_BOOLEAN="true"
}

validation_cache_module_closure() {
    awk -v start_list="$*" '
        FILENAME == ARGV[1] { directory_of[$1] = directory_of[$1] " " $2; next }
        { requirement_of[$1] = requirement_of[$1] " " $2 }
        END {
            tail = split(start_list, queue, " ")
            for (i = 1; i <= tail; i++) { seen[queue[i]] = 1 }
            head = 1
            while (head <= tail) {
                directory = queue[head++]
                requirement_count = split(requirement_of[directory], requirement_list, " ")
                for (i = 1; i <= requirement_count; i++) {
                    found_count = split(directory_of[requirement_list[i]], found_list, " ")
                    for (j = 1; j <= found_count; j++) {
                        if (!(found_list[j] in seen)) { seen[found_list[j]] = 1; queue[++tail] = found_list[j] }
                    }
                }
            }
            for (directory in seen) { print directory }
        }
    ' "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/module-path.list" "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/module-requirement.list" | sort
}

validation_cache_closure_digest_list() {
    awk 'NR == FNR { wanted[$0] = 1; next } $1 in wanted' <(validation_cache_module_closure "$@") "${VALIDATION_CACHE_WORK_DIRECTORY_PATH}/module-digest.list"
}

validation_cache_key() {
    if [[ "true" != "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
        return 0
    fi

    {
        printf 'infrastructure %s\n' "${VALIDATION_CACHE_INFRASTRUCTURE_DIGEST_STRING}"
        printf '%s\n' "$@"
    } | validation_cache_digest
}

validation_cache_module_key() {
    local LANE_STRING="${1:?}"
    local UNIT_STRING="${2:?}"
    shift 2

    if [[ "true" != "${VALIDATION_CACHE_ENABLED_BOOLEAN}" ]]; then
        return 0
    fi

    validation_cache_key "lane ${LANE_STRING}" "unit ${UNIT_STRING}" "$(validation_cache_closure_digest_list "$@")"
}

validation_cache_has() {
    local KEY_STRING="${1-}"

    [[ "true" = "${VALIDATION_CACHE_ENABLED_BOOLEAN}" && "true" != "${FRESH_BOOLEAN}" && "" != "${KEY_STRING}" && -f "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}" ]]
}

validation_cache_record() {
    local KEY_STRING="${1-}"
    local UNIT_STRING="${2:?}"

    if [[ "true" != "${VALIDATION_CACHE_ENABLED_BOOLEAN}" || "" = "${KEY_STRING}" ]]; then
        return 0
    fi

    local WORKTREE_TREE_HASH_STRING=""
    WORKTREE_TREE_HASH_STRING="$(compute_worktree_tree_hash || true)"
    if [[ "${VALIDATION_START_TREE_HASH_STRING}" != "${WORKTREE_TREE_HASH_STRING}" || "" != "$(ignored_go_files_in_packages)" ]]; then
        VALIDATION_CACHE_ENABLED_BOOLEAN="false"
        VALIDATION_TREE_MOVED_BOOLEAN="true"
        warning "the worktree changed while the run validated it — the unit cache is off for the rest of the run and no validation stamp will be written"
        return 0
    fi

    printf '%s %s\n' "${UNIT_STRING}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}.partial"
    mv -f "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}.partial" "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}"
}

run_cached_unit() {
    local LANE_STRING="${1:?}"
    local UNIT_STRING="${2:?}"
    local KEY_STRING="${3-}"
    shift 3

    if validation_cache_has "${KEY_STRING}"; then
        touch "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}"
        info "cached: ${1:?} (unchanged since a green run)"
        VALIDATION_UNIT_SUMMARY_LIST+=("cached	0	${LANE_STRING}	${UNIT_STRING}")
        return 0
    fi

    if [[ "" != "${KEY_STRING}" ]]; then
        rm -f "${VALIDATION_CACHE_DIRECTORY_PATH}/${KEY_STRING}"
    fi

    local EXIT_CODE_INTEGER="0"
    run_section "$@" || EXIT_CODE_INTEGER=$?

    if [[ 0 -ne ${EXIT_CODE_INTEGER} ]]; then
        VALIDATION_UNIT_SUMMARY_LIST+=("failed	${UTILITY_LAST_SECTION_DURATION_SECONDS_INTEGER}	${LANE_STRING}	${UNIT_STRING}")
        return ${EXIT_CODE_INTEGER}
    fi

    VALIDATION_UNIT_SUMMARY_LIST+=("ran	${UTILITY_LAST_SECTION_DURATION_SECONDS_INTEGER}	${LANE_STRING}	${UNIT_STRING}")
    validation_cache_record "${KEY_STRING}" "${LANE_STRING} ${UNIT_STRING}"
}

print_validation_unit_summary() {
    if [[ 0 -eq ${#VALIDATION_UNIT_SUMMARY_LIST[@]} ]]; then
        return 0
    fi

    println ""
    println "unit summary (status, seconds, lane, unit):"
    printf '%s\n' "${VALIDATION_UNIT_SUMMARY_LIST[@]}" | awk -F '\t' '
        { printf "  %-7s %6s  %-14s %s\n", $1, $2 "s", $3, $4; count[$1]++; seconds[$1] += $2 }
        END { printf "  ran %d (%ds), cached %d, failed %d\n", count["ran"], seconds["ran"], count["cached"], count["failed"] }
    '
    println "  wall time ${SECONDS}s"
}

get_versioned_module_directory_list() {
    local CANDIDATE_DIR_STRING
    for CANDIDATE_DIR_STRING in "${REPOSITORY_ROOT_DIRECTORY_STRING}"/v[0-9]*/; do
        CANDIDATE_DIR_STRING="${CANDIDATE_DIR_STRING%/}"
        if [[ -f "${CANDIDATE_DIR_STRING}/go.mod" ]]; then
            printf '%s\n' "${CANDIDATE_DIR_STRING}"
        fi
    done | sort -V
}

get_integration_module_directory_list() {
    {
        if [[ -d "${REPOSITORY_ROOT_DIRECTORY_STRING}/integrations" ]]; then
            find "${REPOSITORY_ROOT_DIRECTORY_STRING}/integrations" \
                -maxdepth 5 \
                -name go.mod \
                -print \
                2>/dev/null |
                while IFS= read -r GO_MOD_PATH_STRING; do
                    if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
                        continue
                    fi
                    dirname "${GO_MOD_PATH_STRING}"
                done
        fi

        local VERSIONED_DIR_STRING
        while IFS= read -r VERSIONED_DIR_STRING; do
            if [[ "" = "${VERSIONED_DIR_STRING}" ]]; then
                continue
            fi
            if [[ -d "${VERSIONED_DIR_STRING}/integrations" ]]; then
                find "${VERSIONED_DIR_STRING}/integrations" \
                    -maxdepth 5 \
                    -name go.mod \
                    -print \
                    2>/dev/null |
                    while IFS= read -r GO_MOD_PATH_STRING; do
                        if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
                            continue
                        fi
                        dirname "${GO_MOD_PATH_STRING}"
                    done
            fi
        done < <(get_versioned_module_directory_list)
    } | sort -u
}

has_staged_change_in_component() {
    local COMPONENT_DIRECTORY_STRING="${1:?}"

    local STAGED_PATH_LIST_STRING
    STAGED_PATH_LIST_STRING="$(git diff --cached --name-only 2>/dev/null || true)"
    if [[ "" = "${STAGED_PATH_LIST_STRING}" ]]; then
        return 1
    fi

    local COMPONENT_RELATIVE_PATH_STRING
    COMPONENT_RELATIVE_PATH_STRING="${COMPONENT_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
    if [[ "${COMPONENT_DIRECTORY_STRING}" = "${REPOSITORY_ROOT_DIRECTORY_STRING}" ]]; then
        COMPONENT_RELATIVE_PATH_STRING=""
    fi

    if [[ "" = "${COMPONENT_RELATIVE_PATH_STRING}" ]]; then
        if grep -E -q '\.go$|go\.(mod|sum)$' <<<"${STAGED_PATH_LIST_STRING}"; then
            return 0
        fi
        return 1
    fi

    if grep -F -q "${COMPONENT_RELATIVE_PATH_STRING}/" <<<"${STAGED_PATH_LIST_STRING}"; then
        return 0
    fi

    return 1
}

run_versioned_modules() {
    local VERSIONED_DIR_STRING
    while IFS= read -r VERSIONED_DIR_STRING; do
        if [[ "" = "${VERSIONED_DIR_STRING}" ]]; then
            continue
        fi

        local VERSION_STRING
        VERSION_STRING="$(basename "${VERSIONED_DIR_STRING}")"

        run_go_checks "${VERSIONED_DIR_STRING}" "melody framework ${VERSION_STRING} (${VERSION_STRING} module)"

        if [[ -f "${VERSIONED_DIR_STRING}/.example/go.mod" ]]; then
            run_go_checks "${VERSIONED_DIR_STRING}/.example" "melody example app ${VERSION_STRING} (${VERSION_STRING}/.example)"
        fi
    done < <(get_versioned_module_directory_list)
}

run_versioned_modules_staged() {
    local VERSIONED_DIR_STRING
    while IFS= read -r VERSIONED_DIR_STRING; do
        if [[ "" = "${VERSIONED_DIR_STRING}" ]]; then
            continue
        fi

        local VERSION_STRING
        VERSION_STRING="$(basename "${VERSIONED_DIR_STRING}")"

        if has_staged_change_in_component "${VERSIONED_DIR_STRING}"; then
            run_go_checks "${VERSIONED_DIR_STRING}" "melody framework ${VERSION_STRING} (${VERSION_STRING} module)"
        else
            info "skip ${VERSION_STRING} module (no staged changes)"
        fi

        if [[ -f "${VERSIONED_DIR_STRING}/.example/go.mod" ]]; then
            if has_staged_change_in_component "${VERSIONED_DIR_STRING}/.example"; then
                run_go_checks "${VERSIONED_DIR_STRING}/.example" "melody example app ${VERSION_STRING} (${VERSION_STRING}/.example)"
            else
                info "skip ${VERSION_STRING}/.example (no staged changes)"
            fi
        fi
    done < <(get_versioned_module_directory_list)
}

main() {
    local ROOT_DIRECTORY_STRING
    ROOT_DIRECTORY_STRING="${REPOSITORY_ROOT_DIRECTORY_STRING}"

    if [[ "live" = "${MODE_STRING}" ]]; then
        run_live_go_suites

        success "validation completed"
        return 0
    fi

    if [[ "e2e" = "${MODE_STRING}" ]]; then
        run_e2e_live_checks

        success "validation completed"
        return 0
    fi

    if [[ "vulncheck" = "${MODE_STRING}" ]]; then
        run_vulncheck_checks

        success "validation completed"
        return 0
    fi

    if [[ "compatibility" = "${MODE_STRING}" ]]; then
        run_compatibility_checks

        success "validation completed"
        return 0
    fi

    if [[ "apidiff" = "${MODE_STRING}" ]]; then
        run_apidiff_checks

        success "validation completed"
        return 0
    fi

    if [[ "all" = "${MODE_STRING}" ]]; then
        trap print_validation_unit_summary EXIT

        record_validation_start_tree_hash
        prepare_validation_cache

        run_go_checks "${ROOT_DIRECTORY_STRING}" "melody framework (root module)"

        if [[ -f "${ROOT_DIRECTORY_STRING}/.example/go.mod" ]]; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/.example" "melody example app (.example)"
        fi

        run_versioned_modules

        local INTEGRATION_MODULE_DIRECTORY_STRING
        while IFS= read -r INTEGRATION_MODULE_DIRECTORY_STRING; do
            if [[ "" = "${INTEGRATION_MODULE_DIRECTORY_STRING}" ]]; then
                continue
            fi
            run_go_checks "${INTEGRATION_MODULE_DIRECTORY_STRING}" "melody integration module: ${INTEGRATION_MODULE_DIRECTORY_STRING#${ROOT_DIRECTORY_STRING}/}"
        done < <(get_integration_module_directory_list)

        run_e2e_harness_checks

        run_documentation_checks

        run_parity_checks

        run_citation_checks

        run_changelog_checks

        run_vulncheck_checks

        run_compatibility_checks

        run_apidiff_checks

        run_race_go_suites

        if [[ "true" = "${SKIP_LIVE_BOOLEAN}" ]]; then
            info "skip live integration suites and live e2e run (--skip-live) — no validation stamp, the pre-push hook accepts only the full run"
        else
            run_live_go_suites
            run_e2e_live_checks
            write_validation_stamp
        fi

        success "validation completed"
        return 0
    fi

    section_start "staged validation" "${TAG_VALIDATE}" "staged"

    if has_staged_change_in_component "${ROOT_DIRECTORY_STRING}"; then
        run_go_checks "${ROOT_DIRECTORY_STRING}" "melody framework (root module)"
    else
        info "skip root module (no staged go/mod/sum changes)"
    fi

    if [[ -f "${ROOT_DIRECTORY_STRING}/.example/go.mod" ]]; then
        if has_staged_change_in_component "${ROOT_DIRECTORY_STRING}/.example"; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/.example" "melody example app (.example)"
        else
            info "skip .example (no staged changes)"
        fi
    fi

    run_versioned_modules_staged

    local INTEGRATION_MODULE_DIRECTORY_STRING
    while IFS= read -r INTEGRATION_MODULE_DIRECTORY_STRING; do
        if [[ "" = "${INTEGRATION_MODULE_DIRECTORY_STRING}" ]]; then
            continue
        fi
        if has_staged_change_in_component "${INTEGRATION_MODULE_DIRECTORY_STRING}"; then
            run_go_checks "${INTEGRATION_MODULE_DIRECTORY_STRING}" "melody integration module: ${INTEGRATION_MODULE_DIRECTORY_STRING#${ROOT_DIRECTORY_STRING}/}"
        else
            info "skip integration module: ${INTEGRATION_MODULE_DIRECTORY_STRING#${ROOT_DIRECTORY_STRING}/}"
        fi
    done < <(get_integration_module_directory_list)

    if has_staged_change_in_component "${ROOT_DIRECTORY_STRING}/.dev/e2e"; then
        run_e2e_harness_checks
    else
        info "skip .dev/e2e harness (no staged changes)"
    fi

    # ungated on purpose, unlike every lane above it. The check reads the whole tree rather than the staged
    # subset, and the drift it reports is precisely between two things that are rarely staged together: a
    # staged source file that adds an exported symbol makes an unstaged document wrong, so gating it on the
    # staged paths would skip it in the one commit that introduced the gap. It needs no container and costs
    # seconds, which is what makes running it every time affordable.
    run_documentation_checks

    # ungated for the same reason as the documentation check above it, in a sharper form: the drift it
    # reports lives between two majors that are never staged together. A fix staged on v1 is precisely what
    # makes the unstaged v2 wrong, so gating on the staged paths would skip the check in the one commit that
    # introduced the divergence. It reads the tree, needs no container and costs seconds.
    run_parity_checks

    # ungated for the same reason, in its own form: a citation goes stale when the CODE moves, and the
    # commit that moves it stages a .go file while the document that names it stays untouched. Gating on
    # the staged paths would skip the check in exactly the commit that broke the claim.
    run_citation_checks

    # ungated for the same reason, in the form closest to home: a changelog entry is written by the very
    # commit that stages the code it describes, and it is written into a file gating on staged paths would
    # then judge against the section it was just dropped into. The whole point is to catch the entry the
    # moment it is filed, not a cycle later.
    run_changelog_checks

    # gated, unlike the four documentary lanes above, because it is not free: it needs the container and
    # the network. The gate is on the one staged change class that can move the answer from the tree's
    # side — a go.mod or go.sum. The toolchain side moves with the image, and --all always asks.
    local STAGED_NAME_LIST_STRING
    STAGED_NAME_LIST_STRING="$(git diff --cached --name-only 2>/dev/null || true)"

    if grep -qE '(^|/)go\.(mod|sum)$' <<<"${STAGED_NAME_LIST_STRING}"; then
        run_vulncheck_checks
    else
        info "skip vulnerability check (no staged go.mod or go.sum change)"
    fi

    # gated like the vulnerability check above, and for the same reason: it needs the container and the
    # network. The trigger is wider, because the answer moves from three sides rather than one — an
    # integration's source, an integration's pin, and the PUBLIC API of a major. A signature change staged
    # only under a major is precisely the case this lane exists to catch, so gating on go.mod alone would
    # be the guard asking a different question than the one it was built for. Example applications and the
    # dev harness are left out: neither is a published module, and the harness already builds GOWORK=off.
    # The api surface lane shares the trigger, because its answer moves from the same three sides.
    local PUBLISHED_STAGED_NAME_LIST_STRING
    PUBLISHED_STAGED_NAME_LIST_STRING="$(grep -vE '(^|/)\.example/' <<<"${STAGED_NAME_LIST_STRING}" | grep -vE '^\.dev/' || true)"

    if grep -qE '\.go$|(^|/)go\.(mod|sum)$' <<<"${PUBLISHED_STAGED_NAME_LIST_STRING}"; then
        run_compatibility_checks
        run_apidiff_checks
    else
        info "skip compatibility and api surface checks (no staged go source, go.mod or go.sum outside the example applications and the dev harness)"
    fi

    section_end "staged validation" "success" "${TAG_VALIDATE}" "staged"
    success "validation completed"
}

main "$@"
