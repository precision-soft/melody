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
if [[ "" = "${1-}" ]]; then
    :
elif [[ "-h" = "${1-}" ]]; then
    println "usage: all.sh [-h] [--all | --staged | --live | --e2e] [--skip-live]"
    println ""
    println "  -h           show this help and exit"
    println "  --all        validate all modules, the live integration suites and the live e2e run (default)"
    println "  --staged     validate only modules with staged changes"
    println "  --live       run only the live integration suites (mirrors the ci live job)"
    println "  --e2e        run only the live e2e harness and stack checks (mirrors the ci e2e job)"
    println "  --skip-live  leave the live suites and the e2e run out of --all, for a hand run with no backends"
    exit 0
elif [[ "--staged" = "${1-}" ]]; then
    MODE_STRING="staged"
elif [[ "--all" = "${1-}" ]]; then
    MODE_STRING="all"
elif [[ "--live" = "${1-}" ]]; then
    MODE_STRING="live"
elif [[ "--e2e" = "${1-}" ]]; then
    MODE_STRING="e2e"
elif [[ "--skip-live" = "${1-}" ]]; then
    MODE_STRING="all"
    SKIP_LIVE_BOOLEAN="true"
else
    fail "unknown flag: ${1}"
fi

if [[ "--skip-live" = "${2-}" ]]; then
    SKIP_LIVE_BOOLEAN="true"
fi

SERVICE_NAME_STRING="dev"

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

    run_section "${COMPONENT_TITLE_STRING}" "${TAG_VALIDATE}" "go" -- \
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
    docker_compose up -d --wait "${LIVE_SERVICE_NAME_STRING_LIST[@]}"

    local BATCH_COMMAND_LIST=()

    local LIVE_MODULE_DIRECTORY_STRING
    while IFS= read -r LIVE_MODULE_DIRECTORY_STRING; do
        if [[ "" = "${LIVE_MODULE_DIRECTORY_STRING}" ]]; then
            continue
        fi

        local LIVE_MODULE_RELATIVE_PATH_STRING="${LIVE_MODULE_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"

        BATCH_COMMAND_LIST+=("${LIVE_ENVIRONMENT_EXPORT_STRING} && cd ${CONTAINER_ROOT_PATH}/${LIVE_MODULE_RELATIVE_PATH_STRING} && go test -race -count=1 ./...")
    done < <(get_integration_module_directory_list)

    run_section "melody live integration suites (mirrors the ci live job)" "${TAG_VALIDATE}" "go" -- \
        run_batch_in_service_shell "${SERVICE_NAME_STRING}" "${BATCH_COMMAND_LIST[@]}"
}

# the race detector over the concurrency-carrying packages of the core majors and the cron runner, mirroring
# the ci race job. The build-tag matrix above runs these without it, so a lost mutex or an unsynchronized memo
# stays green there: these packages hold goroutines that outlive a call (the lazy service memo, the signal
# watcher, the smtp cancellation watcher, the http client's redirect policy reading the header map, the
# logger's shared writer, the message bus dispatch, the cron runner's parallel dispatch), and only the
# detector sees them race — several of those tests assert nothing at all, so without this lane they can only
# ever pass. validation carries the process-lifetime parse and constraint memos that concurrent requests share,
# and openapi the schema memo, so both belong here too. No service containers needed.
RACE_SUITE_SPECIFICATION_STRING_LIST=(
    ". ./container/... ./application/... ./config/... ./event/... ./httpclient/... ./logging/... ./cli/... ./validation/..."
    "v2 ./container/... ./application/... ./config/... ./event/... ./httpclient/... ./logging/... ./cli/... ./validation/..."
    "v3 ./container/... ./application/... ./config/... ./event/... ./httpclient/... ./logging/... ./cli/... ./mailer/... ./lock/... ./messagebus/... ./validation/... ./openapi/..."
    "integrations/cron ./..."
    "integrations/cron/v2 ./..."
    "integrations/cron/v3 ./..."
)

run_race_go_suites() {
    local BATCH_COMMAND_LIST=()

    local RACE_SUITE_SPECIFICATION_STRING
    for RACE_SUITE_SPECIFICATION_STRING in "${RACE_SUITE_SPECIFICATION_STRING_LIST[@]}"; do
        local RACE_MODULE_RELATIVE_PATH_STRING="${RACE_SUITE_SPECIFICATION_STRING%% *}"
        local RACE_PACKAGE_LIST_STRING="${RACE_SUITE_SPECIFICATION_STRING#* }"

        BATCH_COMMAND_LIST+=("cd ${CONTAINER_ROOT_PATH}/${RACE_MODULE_RELATIVE_PATH_STRING} && go test -race -count=1 ${RACE_PACKAGE_LIST_STRING}")
    done

    run_section "melody race detector on the concurrency-carrying core packages (mirrors the ci race job)" "${TAG_VALIDATE}" "go" -- \
        run_batch_in_service_shell "${SERVICE_NAME_STRING}" "${BATCH_COMMAND_LIST[@]}"
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
        return 0
    fi

    run_section "melody e2e harness module (.dev/e2e, GOWORK=off)" "${TAG_VALIDATE}" "go" -- \
        run_batch_in_service_shell "${SERVICE_NAME_STRING}" \
        "cd ${CONTAINER_ROOT_PATH}/.dev/e2e && GOWORK=off go vet ./..." \
        "cd ${CONTAINER_ROOT_PATH}/.dev/e2e && GOWORK=off go test -count=1 ./..."
}

# the package documents against the code of every major. The three majors are near-copies whose documents
# drifted independently, and nothing compared them: a symbol one major documents while another ships it
# undocumented is invisible to every compiler and every test. Reads the tree only, so it needs no container
# and costs seconds.
run_documentation_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/documentation.sh" ]]; then
        return 0
    fi

    run_section "melody package documentation against every major (mirrors the ci documentation job)" "${TAG_VALIDATE}" "docs" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/documentation.sh"
}

# the two live e2e scripts. Every other lane compiles the harness and runs its unit tests; nothing until here
# actually drives a booted application over the wire, and that is the only place a whole class of defect shows
# up at all — a middleware ordering that only matters once a real request traverses the chain, a route the
# router registers but never answers, a session cookie that never reaches the wire. The scripts are host
# scripts: each execs into the dev container itself, so this lane brings the stack up and hands off. mailpit
# and prometheus join the live backends because the mail and metric sections scrape them directly, and the
# load balancer because a stack check asserts what it routes.
E2E_SERVICE_NAME_STRING_LIST=(
    "rabbitmq"
    "redis"
    "mysql"
    "postgres"
    "localstack"
    "mailpit"
    "prometheus"
    "load-balancer"
)

run_e2e_live_checks() {
    if [[ ! -x "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/run.sh" ]]; then
        return 0
    fi

    docker_compose up -d --wait "${E2E_SERVICE_NAME_STRING_LIST[@]}"

    run_section "melody e2e harness live run (.dev/e2e/run.sh)" "${TAG_VALIDATE}" "e2e" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/run.sh"

    run_section "melody e2e stack checks (.dev/e2e/stack.sh)" "${TAG_VALIDATE}" "e2e" -- \
        "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/stack.sh"
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
        if printf '%s\n' "${STAGED_PATH_LIST_STRING}" | grep -E -q '\.go$|go\.(mod|sum)$'; then
            return 0
        fi
        return 1
    fi

    if printf '%s\n' "${STAGED_PATH_LIST_STRING}" | grep -F -q "${COMPONENT_RELATIVE_PATH_STRING}/"; then
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

    if [[ "all" = "${MODE_STRING}" ]]; then
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

        run_race_go_suites

        if [[ "true" = "${SKIP_LIVE_BOOLEAN}" ]]; then
            info "skip live integration suites and live e2e run (--skip-live)"
        else
            run_live_go_suites
            run_e2e_live_checks
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

    section_end "staged validation" "success" "${TAG_VALIDATE}" "staged"
    success "validation completed"
}

main "$@"
