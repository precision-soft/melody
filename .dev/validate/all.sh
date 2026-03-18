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
if [[ "" = "${1-}" ]]; then
    :
elif [[ "-h" = "${1-}" ]]; then
    println "usage: all.sh [-h] [--all | --staged]"
    println ""
    println "  -h         show this help and exit"
    println "  --all      validate all modules (default)"
    println "  --staged   validate only modules with staged changes"
    exit 0
elif [[ "--staged" = "${1-}" ]]; then
    MODE_STRING="staged"
elif [[ "--all" = "${1-}" ]]; then
    MODE_STRING="all"
else
    fail "unknown flag: ${1}"
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

    section_end "staged validation" "success" "${TAG_VALIDATE}" "staged"
    success "validation completed"
}

main "$@"
