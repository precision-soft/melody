#!/bin/bash

if [[ "${TERM-}" == *color* || -t 1 ]]; then
    COLOR_RESET='\e[0;0m'
    COLOR_RED='\e[0;31m'
    COLOR_GREEN='\e[0;32m'
    COLOR_YELLOW='\e[0;33m'
    COLOR_BLUE='\e[0;34m'
    COLOR_WHITE='\e[1;37m'
else
    COLOR_RESET=''
    COLOR_RED=''
    COLOR_GREEN=''
    COLOR_YELLOW=''
    COLOR_BLUE=''
    COLOR_WHITE=''
fi

DOCKER_PATH=".dev/docker/"

println() {
    local TEXT="${1:-}"
    printf %b "${TEXT}\n"
}

print_command() {
    println "${COLOR_YELLOW}[${COLOR_GREEN} $1 ${COLOR_YELLOW}]${COLOR_RESET}"
}

info() {
    println "${COLOR_BLUE}info:${COLOR_RESET} ${1:-}"
}

success() {
    println "${COLOR_GREEN}success:${COLOR_RESET} ${1:-}"
}

error() {
    println "${COLOR_RED}( ${1:-} )${COLOR_RESET}"
}

warning() {
    println "${COLOR_YELLOW}( ${1:-} )${COLOR_RESET}"
}

section() {
    println "${COLOR_YELLOW}[[${COLOR_GREEN} ${1:-} ${COLOR_YELLOW}]]${COLOR_RESET}"
}

section_start() {
    local SECTION_TITLE="${1:-}"
    local PRINT_BLANK="${2:-true}"
    section "${COLOR_WHITE}START >->->->-> ${COLOR_GREEN}${SECTION_TITLE}"
    if [[ "${PRINT_BLANK}" != "false" ]]; then
        println ""
    fi
}

section_end() {
    local SECTION_TITLE="${1:-}"
    local PRINT_BLANK="${2:-true}"
    if [[ "${PRINT_BLANK}" != "false" ]]; then
        println ""
    fi
    section "${COLOR_WHITE}END <-<-<-<-< ${COLOR_GREEN}${SECTION_TITLE}"
}

docker_compose_no_log() {
    (
        cd "${DOCKER_PATH}" &&
        USER_ID="$(id -u)" GROUP_ID="$(id -g)" docker compose \
            -f docker-compose.yml \
            --env-file .env --env-file .env.local \
            "$@"
    )
}

docker_compose() {
    print_command "(cd ${DOCKER_PATH} && USER_ID=$(id -u) GROUP_ID=$(id -g) docker compose --env-file .env --env-file .env.local $*)"
    docker_compose_no_log "$@"
}

run_in_container() {
    bash "${PWD}/dc" exec -T "$@"
}

error_container() {
    echo "the '$1' container is not running"
}

check_container() {
    local CONTAINER_NAME="$1"

    if ! command -v docker >/dev/null 2>&1; then
        warning "docker is not installed or not accessible"
        return 1
    fi

    if command -v docker-compose >/dev/null 2>&1 || docker compose version >/dev/null 2>&1; then
        if [[ -z $(docker_compose_no_log ps -q "${CONTAINER_NAME}") ]]; then
            warning "container '${CONTAINER_NAME}' is not running"
            return 1
        fi
    else
        if [[ -z $(docker ps -q --filter "name=${CONTAINER_NAME}" --filter "status=running") ]]; then
            warning "container '${CONTAINER_NAME}' is not running"
            return 1
        fi
    fi

    return 0
}

staged_files() {
    local TITLE="${1:-staged files}"
    section_start "${TITLE}"
    git diff --cached --name-only
    section_end "${TITLE}"
}
