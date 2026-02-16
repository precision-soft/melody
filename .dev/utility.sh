#!/usr/bin/env bash

if [[ "1" = "${MELODY_UTILITY_SOURCED:-0}" ]]; then
    return 0
fi

MELODY_UTILITY_SOURCED="1"
readonly MELODY_UTILITY_SOURCED

resolve_path() {
    local INPUT_PATH_STRING="${1:?}"

    if command -v readlink >/dev/null 2>&1; then
        if readlink -f / >/dev/null 2>&1; then
            readlink -f "${INPUT_PATH_STRING}"
            return 0
        fi
    fi

    local CURRENT_PATH_STRING="${INPUT_PATH_STRING}"
    while [[ -L "${CURRENT_PATH_STRING}" ]]; do
        local CURRENT_DIRECTORY_STRING
        CURRENT_DIRECTORY_STRING="$(cd -P "$(dirname "${CURRENT_PATH_STRING}")" && pwd)"

        CURRENT_PATH_STRING="$(readlink "${CURRENT_PATH_STRING}")"
        if [[ "/" = "${CURRENT_PATH_STRING:0:1}" ]]; then
            :
        else
            CURRENT_PATH_STRING="${CURRENT_DIRECTORY_STRING}/${CURRENT_PATH_STRING}"
        fi
    done

    printf '%s/%s\n' "$(cd -P "$(dirname "${CURRENT_PATH_STRING}")" && pwd)" "$(basename "${CURRENT_PATH_STRING}")"
}

UTILITY_DIRECTORY_STRING="$(cd -P "$(dirname "$(resolve_path "${BASH_SOURCE[0]}")")" && pwd)"
ROOT_DIRECTORY_STRING="$(cd -P "${UTILITY_DIRECTORY_STRING}/.." && pwd)"
ROOT_DIR="${ROOT_DIRECTORY_STRING}"
export ROOT_DIR

readonly ROOT_DIR

MELODY_DOCKER_DIRECTORY="${ROOT_DIR}/.dev/docker"
MELODY_DOCKER_COMPOSE_FILE_PATH="${MELODY_DOCKER_DIRECTORY}/docker-compose.yml"
MELODY_DOCKER_ENV_FILE_PATH="${MELODY_DOCKER_DIRECTORY}/.env"
MELODY_DOCKER_ENV_LOCAL_FILE_PATH="${MELODY_DOCKER_DIRECTORY}/.env.local"

TAG_DOCKER="docker"
TAG_GIT="git"
TAG_VALIDATE="validate"

readonly TAG_DOCKER
readonly TAG_GIT
readonly TAG_VALIDATE

if [[ "${TERM-}" == *color* || -t 1 ]]; then
    COLOR_RESET='\e[0m'

    COLOR_DIM='\e[0;90m'
    COLOR_RED='\e[1;31m'
    COLOR_GREEN='\e[1;32m'
    COLOR_YELLOW='\e[1;33m'
    COLOR_BLUE='\e[1;34m'
    COLOR_CYAN='\e[1;36m'
    COLOR_WHITE='\e[0;37m'
else
    COLOR_RESET='' COLOR_DIM='' COLOR_RED='' COLOR_GREEN='' COLOR_YELLOW='' COLOR_BLUE='' COLOR_CYAN='' COLOR_WHITE=''
fi

UTILITY_LAST_LINE_WAS_BLANK="false"

utility_to_lower() {
    local INPUT_STRING="${1:-}"
    printf '%s' "${INPUT_STRING}" | LC_ALL=C tr '[:upper:]' '[:lower:]'
}

utility_to_upper() {
    local INPUT_STRING="${1:-}"
    printf '%s' "${INPUT_STRING}" | LC_ALL=C tr '[:lower:]' '[:upper:]'
}

utility_mark_non_blank_line_printed() {
    UTILITY_LAST_LINE_WAS_BLANK="false"
}

println() {
    local TEXT_STRING="${1:-}"
    printf %b "${TEXT_STRING}\n"
    utility_mark_non_blank_line_printed
}

print_level() {
    local LEVEL_INTEGER="${1:-1}"
    local HASH_COUNT_INTEGER=$((LEVEL_INTEGER * 2 - 1))
    local INDEX_INTEGER
    for ((INDEX_INTEGER = 1; INDEX_INTEGER <= HASH_COUNT_INTEGER; INDEX_INTEGER++)); do
        printf '#'
    done
}

print_bracket_line() {
    local COLOR_STRING="${1:-}"
    shift

    printf '%b' "${COLOR_STRING}"

    local SEGMENT_STRING
    for SEGMENT_STRING in "$@"; do
        if [[ "" = "${SEGMENT_STRING}" ]]; then
            continue
        fi
        printf '[ %s ]' "$(utility_to_lower "${SEGMENT_STRING}")"
    done

    printf '%b\n' "${COLOR_RESET}"
    utility_mark_non_blank_line_printed
}

print_bracket_line_raw_last() {
    local COLOR_STRING="${1:-}"
    shift

    printf %b "${COLOR_STRING}"

    local SEGMENT_COUNT_INTEGER="$#"
    local CURRENT_INDEX_INTEGER="0"
    local SEGMENT_STRING
    for SEGMENT_STRING in "$@"; do
        if [[ "" = "${SEGMENT_STRING}" ]]; then
            continue
        fi

        CURRENT_INDEX_INTEGER="$((CURRENT_INDEX_INTEGER + 1))"
        if [[ ${CURRENT_INDEX_INTEGER} -lt ${SEGMENT_COUNT_INTEGER} ]]; then
            printf "[ %s ]" "$(utility_to_lower "${SEGMENT_STRING}")"
            continue
        fi

        printf "[ %s ]" "${SEGMENT_STRING}"
    done

    printf "%b\n" "${COLOR_RESET}"
    utility_mark_non_blank_line_printed
}

print_command() {
    local COMMAND_STRING="${1:-}"
    print_bracket_line "${COLOR_CYAN}" "command" "${COMMAND_STRING}"
}

info() {
    local MESSAGE_STRING="${1:-}"
    print_bracket_line "${COLOR_WHITE}" "info" "${MESSAGE_STRING}"
}

success() {
    local MESSAGE_STRING="${1:-}"
    print_bracket_line "${COLOR_GREEN}" "success" "${MESSAGE_STRING}"
}

warning() {
    local MESSAGE_STRING="${1:-}"
    print_bracket_line "${COLOR_YELLOW}" "warning" "${MESSAGE_STRING}"
}

error() {
    local MESSAGE_STRING="${1:-}"
    print_bracket_line "${COLOR_RED}" "failure" "${MESSAGE_STRING}" >&2
}

fail() {
    error "${1:-}"
    exit 1
}

if [[ "" = "${UTILITY_SECTION_LEVEL_INTEGER:-}" ]]; then
    UTILITY_SECTION_LEVEL_INTEGER="1"
fi

if ! [[ "${UTILITY_SECTION_LEVEL_INTEGER}" =~ ^[0-9]+$ ]]; then
    UTILITY_SECTION_LEVEL_INTEGER="1"
fi

if [[ 1 -gt ${UTILITY_SECTION_LEVEL_INTEGER} ]]; then
    UTILITY_SECTION_LEVEL_INTEGER="1"
fi

export UTILITY_SECTION_LEVEL_INTEGER

utility_print_section_line() {
    local LEVEL_INTEGER="${1:?}"
    local TITLE_STRING="${2:?}"
    local ACTION_STRING="${3:?}"
    shift 3
    local PART_LIST=("$@")

    printf '%b[%s]%b' "${COLOR_BLUE}" "$(print_level "${LEVEL_INTEGER}")" "${COLOR_DIM}"

    local PART_STRING
    for PART_STRING in "${PART_LIST[@]}"; do
        if [[ "" = "${PART_STRING}" ]]; then
            continue
        fi
        printf '[ %s ]' "$(utility_to_lower "${PART_STRING}")"
    done

    printf '[ %b%s%b ]' "${COLOR_BLUE}" "$(utility_to_upper "${TITLE_STRING}")" "${COLOR_DIM}"
    printf '[ %s ]%b\n' "$(utility_to_lower "${ACTION_STRING}")" "${COLOR_RESET}"
    utility_mark_non_blank_line_printed
}

section_start() {
    if [[ 1 -gt $# ]]; then
        fail "section_start requires: title [parts...]"
    fi

    local TITLE_STRING="${1}"
    shift

    local LEVEL_INTEGER="${UTILITY_SECTION_LEVEL_INTEGER}"

    local PART_LIST=("$@")
    utility_print_section_line "${LEVEL_INTEGER}" "${TITLE_STRING}" "start" "${PART_LIST[@]}"

    UTILITY_SECTION_LEVEL_INTEGER="$((LEVEL_INTEGER + 1))"
    export UTILITY_SECTION_LEVEL_INTEGER
}

section_end() {
    if [[ 1 -gt $# ]]; then
        fail "section_end requires: title [status] [parts...]"
    fi

    local TITLE_STRING="${1}"
    shift

    local LEVEL_INTEGER="${UTILITY_SECTION_LEVEL_INTEGER}"
    if [[ 1 -lt ${LEVEL_INTEGER} ]]; then
        LEVEL_INTEGER="$((LEVEL_INTEGER - 1))"
    fi

    if [[ 1 -gt ${LEVEL_INTEGER} ]]; then
        LEVEL_INTEGER="1"
    fi

    local STATUS_STRING="success"
    if [[ 1 -le $# ]]; then
        local FIRST_ARGUMENT_STRING="${1}"
        if [[ "success" = "${FIRST_ARGUMENT_STRING}" || "failure" = "${FIRST_ARGUMENT_STRING}" || "failed" = "${FIRST_ARGUMENT_STRING}" ]]; then
            STATUS_STRING="${FIRST_ARGUMENT_STRING}"
            shift
        fi
    fi

    if [[ "failed" = "${STATUS_STRING}" ]]; then
        STATUS_STRING="failure"
    fi

    local PART_LIST=("$@")

    printf '%b[%s]%b' "${COLOR_BLUE}" "$(print_level "${LEVEL_INTEGER}")" "${COLOR_DIM}"

    local PART_STRING
    for PART_STRING in "${PART_LIST[@]}"; do
        if [[ "" = "${PART_STRING}" ]]; then
            continue
        fi
        printf '[ %s ]' "$(utility_to_lower "${PART_STRING}")"
    done

    printf '[ %b%s%b ]' "${COLOR_BLUE}" "$(utility_to_upper "${TITLE_STRING}")" "${COLOR_DIM}"
    printf '[ end ]'

    if [[ "failure" = "${STATUS_STRING}" ]]; then
        printf '[ %bfailure%b ]%b\n' "${COLOR_RED}" "${COLOR_DIM}" "${COLOR_RESET}"
    else
        printf '[ %bsuccess%b ]%b\n' "${COLOR_GREEN}" "${COLOR_DIM}" "${COLOR_RESET}"
    fi

    utility_mark_non_blank_line_printed

    UTILITY_SECTION_LEVEL_INTEGER="${LEVEL_INTEGER}"
    export UTILITY_SECTION_LEVEL_INTEGER
}

run_section() {
    if [[ 3 -gt $# ]]; then
        fail "run_section requires: title [parts...] -- command..."
    fi

    local TITLE_STRING="${1:?}"
    shift

    local PART_LIST=()
    local COMMAND_PART_LIST=()
    local FOUND_DELIMITER_STRING="false"

    local ARGUMENT_STRING
    for ARGUMENT_STRING in "$@"; do
        if [[ "false" = "${FOUND_DELIMITER_STRING}" ]]; then
            if [[ "--" = "${ARGUMENT_STRING}" ]]; then
                FOUND_DELIMITER_STRING="true"
                continue
            fi

            PART_LIST+=("${ARGUMENT_STRING}")
            continue
        fi

        COMMAND_PART_LIST+=("${ARGUMENT_STRING}")
    done

    if [[ "true" = "${FOUND_DELIMITER_STRING}" ]]; then
        :
    else
        fail "run_section requires delimiter: --"
    fi

    if [[ 0 -lt ${#COMMAND_PART_LIST[@]} ]]; then
        :
    else
        fail "run_section requires a command after --"
    fi

    section_start "${TITLE_STRING}" "${PART_LIST[@]}"

    local EXIT_CODE_INTEGER="0"
    "${COMMAND_PART_LIST[@]}" || EXIT_CODE_INTEGER=$?

    if [[ 0 -eq ${EXIT_CODE_INTEGER} ]]; then
        section_end "${TITLE_STRING}" "success" "${PART_LIST[@]}"
        return 0
    fi

    section_end "${TITLE_STRING}" "failure" "${PART_LIST[@]}"
    return ${EXIT_CODE_INTEGER}
}

require_command() {
    local COMMAND_NAME="${1:?}"

    if ! command -v "${COMMAND_NAME}" >/dev/null 2>&1; then
        fail "${COMMAND_NAME} is not available"
    fi

    return 0
}

require_docker() {
    require_command docker
}

require_docker_daemon() {
    if ! docker info >/dev/null 2>&1; then
        fail "docker daemon is not reachable"
    fi

    return 0
}

ensure_docker_env_files() {
    if [[ -f "${MELODY_DOCKER_ENV_FILE_PATH}" ]]; then
        :
    else
        fail "missing ${MELODY_DOCKER_ENV_FILE_PATH}"
    fi

    if [[ -f "${MELODY_DOCKER_ENV_LOCAL_FILE_PATH}" ]]; then
        :
    else
        info "creating .env.local"
        touch "${MELODY_DOCKER_ENV_LOCAL_FILE_PATH}"
    fi
}

docker_print_command() {
    local COMMAND_PART_LIST=("$@")
    local COMMAND_STRING=""
    local COMMAND_PART_STRING

    for COMMAND_PART_STRING in "${COMMAND_PART_LIST[@]}"; do
        if [[ "" = "${COMMAND_STRING}" ]]; then
            COMMAND_STRING="${COMMAND_PART_STRING}"
        else
            COMMAND_STRING="${COMMAND_STRING} ${COMMAND_PART_STRING}"
        fi
    done

    print_bracket_line_raw_last "${COLOR_GREEN}" "${TAG_DOCKER}" "command" "${COMMAND_STRING}"
}

docker_compose_no_log() {
    ensure_docker_env_files

    (
        cd "${MELODY_DOCKER_DIRECTORY}" &&
            USER_ID="$(id -u)" GROUP_ID="$(id -g)" docker compose \
                -f "${MELODY_DOCKER_COMPOSE_FILE_PATH}" \
                --env-file "${MELODY_DOCKER_ENV_FILE_PATH}" \
                --env-file "${MELODY_DOCKER_ENV_LOCAL_FILE_PATH}" \
                "$@"
    )
}

docker_compose() {
    ensure_docker_env_files

    USER_ID="$(id -u)"
    GROUP_ID="$(id -g)"
    export USER_ID GROUP_ID

    local COMMAND_PART_LIST=(
        docker
        compose
        -f "${MELODY_DOCKER_COMPOSE_FILE_PATH}"
        --env-file "${MELODY_DOCKER_ENV_FILE_PATH}"
        --env-file "${MELODY_DOCKER_ENV_LOCAL_FILE_PATH}"
    )

    local ARGUMENT_STRING
    for ARGUMENT_STRING in "$@"; do
        COMMAND_PART_LIST+=("${ARGUMENT_STRING}")
    done

    docker_print_command "${COMMAND_PART_LIST[@]}"

    docker_compose_no_log "$@"
}

docker_compose_service_exists() {
    local SERVICE_NAME_STRING="${1:?}"

    if ! command -v docker >/dev/null 2>&1; then
        return 1
    fi

    if ! docker info >/dev/null 2>&1; then
        return 1
    fi

    if docker_compose_no_log config --services 2>/dev/null | grep -Fxq "${SERVICE_NAME_STRING}"; then
        return 0
    fi

    return 1
}

ensure_service_running() {
    local SERVICE_NAME_STRING="${1:?}"

    if [[ "" = "$(docker_compose_no_log ps -q "${SERVICE_NAME_STRING}" 2>/dev/null || true)" ]]; then
        info "service ${SERVICE_NAME_STRING} is not running [starting]"
        docker_compose up -d "${SERVICE_NAME_STRING}"
    fi
}

run_in_service_shell() {
    local SERVICE_NAME_STRING="${1:?}"
    local COMMAND_STRING="${2:?}"

    ensure_service_running "${SERVICE_NAME_STRING}"

    docker_compose exec -T "${SERVICE_NAME_STRING}" bash -c "${COMMAND_STRING}" </dev/null
}

run_in_service_shell_interactive() {
    local SERVICE_NAME_STRING="${1:?}"
    local COMMAND_STRING="${2:?}"

    ensure_service_running "${SERVICE_NAME_STRING}"

    docker_compose exec "${SERVICE_NAME_STRING}" bash -c "${COMMAND_STRING}"
}

staged_files() {
    local TITLE_STRING="${1:-staged files}"

    section_start "${TITLE_STRING}" "${TAG_GIT}"

    local STAGED_PATH_LIST_STRING
    STAGED_PATH_LIST_STRING="$(git diff --cached --name-only | awk 'NF')"

    if [[ "" = "${STAGED_PATH_LIST_STRING}" ]]; then
        warning "no staged files"
        section_end "${TITLE_STRING}" "success" "${TAG_GIT}"
        return 0
    fi

    local STAGED_PATH_STRING
    while IFS= read -r STAGED_PATH_STRING; do
        if [[ "" = "${STAGED_PATH_STRING}" ]]; then
            continue
        fi
        println "${COLOR_BLUE}  - ${STAGED_PATH_STRING}${COLOR_RESET}"
    done <<<"${STAGED_PATH_LIST_STRING}"

    section_end "${TITLE_STRING}" "success" "${TAG_GIT}"
}
