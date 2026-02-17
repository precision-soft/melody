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
if [[ "${1-}" = "--staged" ]]; then
    MODE_STRING="staged"
elif [[ "${1-}" = "--all" ]]; then
    MODE_STRING="all"
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

    section_start "${COMPONENT_TITLE_STRING}" "${TAG_VALIDATE}" "go"

    local TAGS_STRING
    for TAGS_STRING in "${GO_TAG_LIST_STRING_LIST[@]}"; do
        if [[ "" = "${TAGS_STRING}" ]]; then
            run_section "go vet" "${TAG_VALIDATE}" "go" "vet" -- \
                run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go vet ./..."

            run_section "go test" "${TAG_VALIDATE}" "go" "test" -- \
                run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go test ./..."
        else
            run_section "go vet" "${TAG_VALIDATE}" "go" "vet" "tags" "${TAGS_STRING}" -- \
                run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go vet -tags '${TAGS_STRING}' ./..."

            run_section "go test" "${TAG_VALIDATE}" "go" "test" "tags" "${TAGS_STRING}" -- \
                run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go test -tags '${TAGS_STRING}' ./..."
        fi
    done

    section_end "${COMPONENT_TITLE_STRING}" "success" "${TAG_VALIDATE}" "go"
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

        if [[ -d "${REPOSITORY_ROOT_DIRECTORY_STRING}/v2/integrations" ]]; then
            find "${REPOSITORY_ROOT_DIRECTORY_STRING}/v2/integrations" \
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

main() {
    local ROOT_DIRECTORY_STRING
    ROOT_DIRECTORY_STRING="${REPOSITORY_ROOT_DIRECTORY_STRING}"

    if [[ "all" = "${MODE_STRING}" ]]; then
        run_go_checks "${ROOT_DIRECTORY_STRING}" "melody framework (root module)"

        if [[ -f "${ROOT_DIRECTORY_STRING}/.example/go.mod" ]]; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/.example" "melody example app (.example)"
        fi

        if [[ -f "${ROOT_DIRECTORY_STRING}/v2/go.mod" ]]; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/v2" "melody framework v2 (v2 module)"
        fi

        if [[ -f "${ROOT_DIRECTORY_STRING}/v2/.example/go.mod" ]]; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/v2/.example" "melody example app v2 (v2/.example)"
        fi

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

    if [[ -f "${ROOT_DIRECTORY_STRING}/v2/go.mod" ]]; then
        if has_staged_change_in_component "${ROOT_DIRECTORY_STRING}/v2"; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/v2" "melody framework v2 (v2 module)"
        else
            info "skip v2 module (no staged changes)"
        fi
    fi

    if [[ -f "${ROOT_DIRECTORY_STRING}/v2/.example/go.mod" ]]; then
        if has_staged_change_in_component "${ROOT_DIRECTORY_STRING}/v2/.example"; then
            run_go_checks "${ROOT_DIRECTORY_STRING}/v2/.example" "melody example app v2 (v2/.example)"
        else
            info "skip v2/.example (no staged changes)"
        fi
    fi

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
