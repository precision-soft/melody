#!/usr/bin/env bash

# Runs govulncheck over every Go module in the tree and gates on the REACHABLE findings.
#
#   .dev/validate/vulncheck.sh [--report]
#
# The band exists because no other lane asks the question at all: vet, the tests and the race detector
# judge the code this repository writes, and the parity and citation bands judge what it documents — none
# of them reads a security advisory. A vulnerable function this code actually calls is green everywhere
# else by construction.
#
# The gate is on reachability, which is the analysis govulncheck exists to do: an advisory against a
# module this tree merely requires, in code no call path here reaches, is reported by the tool as
# informational and deliberately not gated — the dependency pinning policy of the integration modules
# pins the OLDEST version that compiles, so ungated informational findings are expected and bumping every
# pin they name would defeat that policy for nothing the binary can execute.
#
# Findings that ARE reachable land in `vulncheck.baseline`, one line per module and advisory, with a
# mandatory reason. The baseline is bidirectional, like the citation band's: a reachable finding with no
# line fails the check, and a line whose finding no longer occurs fails it just as loudly — the advisory
# it excuses is gone, so the line has to go. That second half is what makes a toolchain bump visible: the
# standard-library class dies wholesale at the next image rebuild, and this band then demands the rows be
# deleted rather than letting them rot into a suppression list.
#
# Unlike its neighbours this band needs the development container: the Go toolchain lives there, and the
# scan runs against the toolchain that builds — which is also why a standard-library advisory is a row
# here until the IMAGE moves, not until any go.mod does. govulncheck is installed into the container on
# first use and needs the network for the module download and for the vulnerability database.
#
# `--report` prints the current reachable findings as baseline-shaped rows (reason left for the reader to
# write at the site), for regenerating the baseline after a toolchain or dependency move.

set -euo pipefail
IFS=$'\n\t'

REPOSITORY_ROOT_DIRECTORY_STRING="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ "" = "${REPOSITORY_ROOT_DIRECTORY_STRING}" ]]; then
    SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    DEV_DIRECTORY_STRING="$(cd -P "${SCRIPT_DIRECTORY_STRING}/.." && pwd)"
    REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "${DEV_DIRECTORY_STRING}/.." && pwd)"
fi

. "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/utility.sh"

cd "${REPOSITORY_ROOT_DIRECTORY_STRING}"

REPORT_BOOLEAN="false"
if [[ "--report" = "${1-}" ]]; then
    REPORT_BOOLEAN="true"
elif [[ "" != "${1-}" ]]; then
    fail "unknown flag: ${1}"
fi

SERVICE_NAME_STRING="dev"
CONTAINER_ROOT_PATH="/app"
BASELINE_FILE_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/vulncheck.baseline"

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

# the same module set the --all lane of all.sh validates, from the same discovery: the root module, the
# versioned majors, every example application, every integration module, and the e2e harness — which is
# outside go.work on purpose, so it scans with GOWORK=off exactly as it builds.
get_scanned_module_relative_path_list() {
    {
        if [[ -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/go.mod" ]]; then
            printf '.\n'
        fi
        if [[ -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.example/go.mod" ]]; then
            printf '.example\n'
        fi

        local VERSIONED_DIR_STRING
        for VERSIONED_DIR_STRING in "${REPOSITORY_ROOT_DIRECTORY_STRING}"/v[0-9]*/; do
            VERSIONED_DIR_STRING="${VERSIONED_DIR_STRING%/}"
            if [[ -f "${VERSIONED_DIR_STRING}/go.mod" ]]; then
                printf '%s\n' "${VERSIONED_DIR_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
            fi
            if [[ -f "${VERSIONED_DIR_STRING}/.example/go.mod" ]]; then
                printf '%s\n' "${VERSIONED_DIR_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}/.example"
            fi
        done

        local INTEGRATION_ROOT_STRING
        for INTEGRATION_ROOT_STRING in "${REPOSITORY_ROOT_DIRECTORY_STRING}/integrations" "${REPOSITORY_ROOT_DIRECTORY_STRING}"/v[0-9]*/integrations; do
            if [[ -d "${INTEGRATION_ROOT_STRING}" ]]; then
                find "${INTEGRATION_ROOT_STRING}" -maxdepth 5 -name go.mod -print 2>/dev/null |
                    while IFS= read -r GO_MOD_PATH_STRING; do
                        if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
                            continue
                        fi
                        local MODULE_DIR_STRING
                        MODULE_DIR_STRING="$(dirname "${GO_MOD_PATH_STRING}")"
                        printf '%s\n' "${MODULE_DIR_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
                    done
            fi
        done

        if [[ -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/e2e/go.mod" ]]; then
            printf '.dev/e2e\n'
        fi
    } | sort -u
}

MODULE_RELATIVE_PATH_LIST=()
while IFS= read -r MODULE_RELATIVE_PATH_STRING; do
    if [[ "" = "${MODULE_RELATIVE_PATH_STRING}" ]]; then
        continue
    fi
    MODULE_RELATIVE_PATH_LIST+=("${MODULE_RELATIVE_PATH_STRING}")
done < <(get_scanned_module_relative_path_list)

if [[ 0 -eq "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "no Go module found to scan — an empty module list here would report a pass over nothing"
fi

# one container invocation for the whole scan: the loop runs inside, and every line of the protocol names
# the module it is about, so a module that produced no line at all is detected on the host rather than
# silently counted as clean. govulncheck answers 0 for a clean module and 3 for one with findings; any
# other exit is the tool failing, which is a failure of the band, never a pass.
SCAN_OUTPUT_STRING="$(
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" sh -s -- "${MODULE_RELATIVE_PATH_LIST[@]}" <<'CONTAINER_SCRIPT'
set -u
if ! command -v govulncheck >/dev/null 2>&1; then
    if ! go install golang.org/x/vuln/cmd/govulncheck@latest >/dev/null 2>&1; then
        printf 'PROTOCOL\tINSTALL-FAILED\n'
        exit 0
    fi
fi
if ! command -v govulncheck >/dev/null 2>&1; then
    printf 'PROTOCOL\tINSTALL-FAILED\n'
    exit 0
fi
for MODULE_PATH in "$@"; do
    [ -n "${MODULE_PATH}" ] || continue
    if [ ".dev/e2e" = "${MODULE_PATH}" ]; then
        OUTPUT="$(cd "/app/${MODULE_PATH}" && GOWORK=off govulncheck ./... 2>&1)"
    else
        OUTPUT="$(cd "/app/${MODULE_PATH}" && govulncheck ./... 2>&1)"
    fi
    EXIT_CODE=$?
    if [ 0 -ne "${EXIT_CODE}" ] && [ 3 -ne "${EXIT_CODE}" ]; then
        printf '%s\tERROR\t%s\n' "${MODULE_PATH}" "${EXIT_CODE}"
        printf '%s\n' "${OUTPUT}" | tail -5 | awk -v module="${MODULE_PATH}" '{ print module "\tDETAIL\t" $0 }' | tr -d '\r'
        continue
    fi
    printf '%s\n' "${OUTPUT}" | grep -oE 'GO-[0-9]{4}-[0-9]+' | sort -u | while IFS= read -r FINDING_ID; do
        printf '%s\tFINDING\t%s\n' "${MODULE_PATH}" "${FINDING_ID}"
    done
    printf '%s\tSCANNED\n' "${MODULE_PATH}"
done
CONTAINER_SCRIPT
)"

if printf '%s\n' "${SCAN_OUTPUT_STRING}" | grep -Fxq $'PROTOCOL\tINSTALL-FAILED'; then
    fail "govulncheck is not installed in the ${SERVICE_NAME_STRING} container and installing it failed — the band cannot run, and reporting success here would mean it silently contributed nothing"
fi

SCANNED_COUNT_NUMBER="$(printf '%s\n' "${SCAN_OUTPUT_STRING}" | grep -c $'\tSCANNED$' || true)"
ERROR_LINE_LIST_STRING="$(printf '%s\n' "${SCAN_OUTPUT_STRING}" | grep $'\tERROR\t' || true)"
FINDING_LINE_LIST_STRING="$(printf '%s\n' "${SCAN_OUTPUT_STRING}" | grep $'\tFINDING\t' || true)"

if [[ "" != "${ERROR_LINE_LIST_STRING}" ]]; then
    printf '%s\n' "${SCAN_OUTPUT_STRING}" | grep $'\tDETAIL\t' || true
    fail "govulncheck failed on: $(printf '%s\n' "${ERROR_LINE_LIST_STRING}" | cut -f1 | sort -u | tr '\n' ' ')"
fi

if [[ "${SCANNED_COUNT_NUMBER}" -ne "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "scanned ${SCANNED_COUNT_NUMBER} of ${#MODULE_RELATIVE_PATH_LIST[@]} modules — a module that vanished from the scan is not a clean module"
fi

if [[ "true" = "${REPORT_BOOLEAN}" ]]; then
    if [[ "" = "${FINDING_LINE_LIST_STRING}" ]]; then
        info "no reachable finding in any of the ${SCANNED_COUNT_NUMBER} modules"
        exit 0
    fi
    printf '%s\n' "${FINDING_LINE_LIST_STRING}" | while IFS=$'\t' read -r MODULE_PATH_STRING _ FINDING_ID_STRING; do
        printf '%s ~ %s ~ \n' "${MODULE_PATH_STRING}" "${FINDING_ID_STRING}"
    done
    exit 0
fi

# the baseline rows: module ~ advisory ~ reason, reason mandatory, no wildcards
BASELINE_KEY_LIST=()
if [[ -f "${BASELINE_FILE_PATH}" ]]; then
    while IFS= read -r BASELINE_LINE_STRING; do
        case "${BASELINE_LINE_STRING}" in
            ''|'#'*) continue ;;
        esac
        BASELINE_MODULE_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f1 | sed 's/^ *//; s/ *$//')"
        BASELINE_FINDING_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f2 | sed 's/^ *//; s/ *$//')"
        BASELINE_REASON_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f3- | sed 's/^ *//; s/ *$//')"
        if [[ "" = "${BASELINE_MODULE_STRING}" || "" = "${BASELINE_FINDING_STRING}" || "" = "${BASELINE_REASON_STRING}" ]]; then
            fail "malformed baseline line (module ~ advisory ~ reason, all mandatory): ${BASELINE_LINE_STRING}"
        fi
        case "${BASELINE_MODULE_STRING}${BASELINE_FINDING_STRING}" in
            *'*'*) fail "wildcard refused in baseline line: ${BASELINE_LINE_STRING}" ;;
        esac
        BASELINE_KEY_LIST+=("${BASELINE_MODULE_STRING}"$'\t'"${BASELINE_FINDING_STRING}")
    done < "${BASELINE_FILE_PATH}"
fi

FOUND_KEY_LIST_STRING="$(printf '%s\n' "${FINDING_LINE_LIST_STRING}" | awk -F'\t' 'NF >= 3 { print $1 "\t" $3 }' | sort -u)"

NEW_FINDING_LIST_STRING=""
while IFS= read -r FOUND_KEY_STRING; do
    if [[ "" = "${FOUND_KEY_STRING}" ]]; then
        continue
    fi
    FOUND_IN_BASELINE_BOOLEAN="false"
    for BASELINE_KEY_STRING in ${BASELINE_KEY_LIST[@]+"${BASELINE_KEY_LIST[@]}"}; do
        if [[ "${FOUND_KEY_STRING}" = "${BASELINE_KEY_STRING}" ]]; then
            FOUND_IN_BASELINE_BOOLEAN="true"
            break
        fi
    done
    if [[ "false" = "${FOUND_IN_BASELINE_BOOLEAN}" ]]; then
        NEW_FINDING_LIST_STRING="${NEW_FINDING_LIST_STRING}${FOUND_KEY_STRING}"$'\n'
    fi
done <<<"${FOUND_KEY_LIST_STRING}"

STALE_ROW_LIST_STRING=""
for BASELINE_KEY_STRING in ${BASELINE_KEY_LIST[@]+"${BASELINE_KEY_LIST[@]}"}; do
    if ! printf '%s\n' "${FOUND_KEY_LIST_STRING}" | grep -Fxq "${BASELINE_KEY_STRING}"; then
        STALE_ROW_LIST_STRING="${STALE_ROW_LIST_STRING}${BASELINE_KEY_STRING}"$'\n'
    fi
done

FOUND_COUNT_NUMBER=0
if [[ "" != "${FOUND_KEY_LIST_STRING}" ]]; then
    FOUND_COUNT_NUMBER="$(printf '%s\n' "${FOUND_KEY_LIST_STRING}" | grep -c . || true)"
fi

info "${SCANNED_COUNT_NUMBER} module(s) scanned: ${FOUND_COUNT_NUMBER} reachable finding(s), ${#BASELINE_KEY_LIST[@]} baseline row(s)"

FAILED_BOOLEAN="false"

if [[ "" != "${NEW_FINDING_LIST_STRING}" ]]; then
    FAILED_BOOLEAN="true"
    while IFS=$'\t' read -r MODULE_PATH_STRING FINDING_ID_STRING; do
        if [[ "" = "${MODULE_PATH_STRING}" ]]; then
            continue
        fi
        println "new reachable finding: ${FINDING_ID_STRING} in ${MODULE_PATH_STRING} (https://pkg.go.dev/vuln/${FINDING_ID_STRING})"
    done <<<"${NEW_FINDING_LIST_STRING}"
fi

if [[ "" != "${STALE_ROW_LIST_STRING}" ]]; then
    FAILED_BOOLEAN="true"
    while IFS=$'\t' read -r MODULE_PATH_STRING FINDING_ID_STRING; do
        if [[ "" = "${MODULE_PATH_STRING}" ]]; then
            continue
        fi
        println "stale baseline row: ${FINDING_ID_STRING} in ${MODULE_PATH_STRING} no longer occurs — the row has to go"
    done <<<"${STALE_ROW_LIST_STRING}"
fi

if [[ "true" = "${FAILED_BOOLEAN}" ]]; then
    fail "the reachable findings and the baseline disagree — repair the finding or file it with a reason, and delete every stale row"
fi

success "every reachable finding is filed in the baseline with a reason, and every baseline row still occurs"
