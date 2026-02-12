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

MODE_STRING="tidy"
if [[ "${1-}" = "--download" ]]; then
    MODE_STRING="download"
elif [[ "${1-}" = "--tidy" ]]; then
    MODE_STRING="tidy"
elif [[ "${1-}" = "--download-and-tidy" ]]; then
    MODE_STRING="download-and-tidy"
fi

SERVICE_NAME_STRING="dev"

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

# ── container path helpers ───────────────────────────────────────────────────

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

# ── module discovery ─────────────────────────────────────────────────────────

get_module_directory_list() {
    {
        printf '%s\n' "${REPOSITORY_ROOT_DIRECTORY_STRING}"
        if [[ -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.example/go.mod" ]]; then
            printf '%s\n' "${REPOSITORY_ROOT_DIRECTORY_STRING}/.example"
        fi
        find "${REPOSITORY_ROOT_DIRECTORY_STRING}/integrations" -maxdepth 4 -name go.mod -print 2>/dev/null | while IFS= read -r GO_MOD_PATH_STRING; do
            if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
                continue
            fi
            dirname "${GO_MOD_PATH_STRING}"
        done
    } | sort -u
}

# ── mod operations via container ─────────────────────────────────────────────

run_mod_for_directory() {
    local MODULE_DIRECTORY_STRING="${1:?}"
    local MODULE_LABEL_STRING="${2:?}"

    local CONTAINER_DIRECTORY_STRING
    CONTAINER_DIRECTORY_STRING="$(container_path_for "${MODULE_DIRECTORY_STRING}")"

    section_start "${MODULE_LABEL_STRING}" "${TAG_VALIDATE}" "go" "mod"

    if [[ "download" = "${MODE_STRING}" || "download-and-tidy" = "${MODE_STRING}" ]]; then
        run_section "go mod download" "${TAG_VALIDATE}" "go" "mod" "download" -- \
            run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go mod download"
    fi

    if [[ "tidy" = "${MODE_STRING}" || "download-and-tidy" = "${MODE_STRING}" ]]; then
        run_section "go mod tidy" "${TAG_VALIDATE}" "go" "mod" "tidy" -- \
            run_in_service_shell "${SERVICE_NAME_STRING}" "cd ${CONTAINER_DIRECTORY_STRING} && go mod tidy"
    fi

    section_end "${MODULE_LABEL_STRING}" "success" "${TAG_VALIDATE}" "go" "mod"
}

# ── main ─────────────────────────────────────────────────────────────────────

main() {
    local ROOT_DIRECTORY_STRING
    ROOT_DIRECTORY_STRING="${REPOSITORY_ROOT_DIRECTORY_STRING}"

    local MODULE_DIRECTORY_STRING
    while IFS= read -r MODULE_DIRECTORY_STRING; do
        if [[ "" = "${MODULE_DIRECTORY_STRING}" ]]; then
            continue
        fi

        local LABEL_STRING
        if [[ "${MODULE_DIRECTORY_STRING}" = "${ROOT_DIRECTORY_STRING}" ]]; then
            LABEL_STRING="melody framework (root module)"
        else
            LABEL_STRING="go module: ${MODULE_DIRECTORY_STRING#${ROOT_DIRECTORY_STRING}/}"
        fi

        run_mod_for_directory "${MODULE_DIRECTORY_STRING}" "${LABEL_STRING}"
    done < <(get_module_directory_list)

    success "go mod ${MODE_STRING} completed"
}

main "$@"
