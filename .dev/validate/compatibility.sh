#!/usr/bin/env bash

# Builds every integration module against the framework version its own go.mod PINS, rather than against
# the local tree, and gates on the result.
#
#   .dev/validate/compatibility.sh [--report]
#
# The band exists because no other lane asks the question, and the reason is structural: this repository
# carries a go.work, and every other Go lane in the gate builds with the workspace ACTIVE. A workspace
# substitutes the LOCAL module for the dependency a go.mod declares, so `go vet ./...` inside an
# integration compiles it against the framework as it is in this worktree — never against the version the
# module publishes itself as needing. That is not what a consumer gets: `go get` reads the go.mod. So a
# change to a major that breaks the modules this repository itself publishes is green in every lane here,
# and stays green until someone installs the integration from a tag.
#
# Green under the workspace is a DEVELOPMENT check. This lane is the PUBLISHING one, and the two are
# deliberately separate sections.
#
# What the band reports is split in two, because counting the failures and reading them are different
# things and only the second is actionable:
#
#   ahead     every diagnostic is an ABSENCE — the integration names something the pinned version does
#             not have. That is the normal, expected state of an unreleased branch: the framework grew a
#             symbol, the integration started using it, and the pin still names an older tag. The
#             dependency policy pins the OLDEST version that compiles, so the release train raises the
#             pin and the row dies. It is not a defect and nothing here has to be repaired.
#
#   contract  at least one diagnostic is a SHAPE mismatch — the name EXISTS at the pinned version and the
#             integration no longer satisfies it. That is a backwards-incompatible change on the
#             framework's published surface, and no pin bump makes it go away for a third party who
#             implements the same interface. Every such row needs a decision, so they print under their
#             own heading rather than in the same list as the routine ones.
#
# The two absence forms the type checker emits are both counted as absence, because both mean the name is
# not there: `undefined: pkg.Name` for a package-level identifier, and
# `x.Sel undefined (type T has no field or method Sel)` for a member. Everything else — `wrong type for
# method`, `missing method`, an argument-count or assignability error — is a shape mismatch. An error form
# this reader does not recognise is classified as `contract`, never as `ahead`: an unknown diagnostic has
# to be looked at, and defaulting it to the routine class is exactly how a band stops asking its question.
#
# A symbol reads as the compiler named it, which for an absent package-level identifier is the alias the
# importing FILE chose — `securitycontract.EpochRevocableTokenStore` where the framework calls the package
# `security/contract`. Rewriting it to a canonical path would mean resolving an alias against a version
# that does not have the symbol, so the row keeps what was measured and the two spellings can sit in the
# same row.
#
# The build runs with `-gcflags=all=-e`, which removes the compiler's ten-error limit. Without it the
# diagnostic list is truncated with `too many errors`, and the truncation is not hypothetical: measured on
# this tree, integrations/bunorm/migrate/v3 hid two of its five distinct symbols behind the cut. A
# contract mismatch sitting behind ten absences would be classified as `ahead` by a reader that never saw
# it, which is the single failure this band exists to prevent.
#
# `go build`, not `go vet` or `go test`: the question is the consumer's. A `go get` of an integration
# compiles its packages, never its test files, so a break confined to a _test.go is not a compatibility
# fact about the published module and filing a baseline row for it would be filing noise.
#
# A module whose go.mod carries a `replace` for a melody module is skipped, with its reason printed and
# counted — a replace answers the dependency locally, so there is no pinned version left to ask about.
# The skips are counted rather than dropped because built + skipped has to equal discovered: a module
# that quietly vanished from the run is not a module that passed.
#
# The v1 and v2 integrations pin RELEASED versions of frozen majors and are expected to build. They are
# the band's positive control, not filler: a run in which they fail is a finding about the freeze.
#
# Like its neighbours the band needs the development container, because the Go toolchain lives there, and
# it needs the network on a cold module cache — it downloads the pinned version of each dependency, which
# is the whole point of asking the question outside the workspace.
#
# `--report` prints the current failures as baseline-shaped rows, with the reason left for the reader to
# write at the site, for regenerating the file after the release train raises the pins.

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
BASELINE_FILE_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/compatibility.baseline"

# the baseline rows: module ~ class ~ symbols ~ reason, all four mandatory, no wildcards. The symbols are
# the exact set the run measures, not a prefix of it: a row that named a subset would grow silently to
# cover the next symbol the branch starts using, which is the failure a wildcard would cause spelled
# differently. The declared pin is deliberately NOT a field — the run reads it from the go.mod and prints
# it, so the file cannot carry a version that has gone stale against the module it describes.
BASELINE_MODULE_LIST=()
BASELINE_CLASS_LIST=()
BASELINE_SYMBOL_LIST=()
if [[ -f "${BASELINE_FILE_PATH}" ]]; then
    while IFS= read -r BASELINE_LINE_STRING; do
        case "${BASELINE_LINE_STRING}" in
            ''|'#'*) continue ;;
        esac
        BASELINE_MODULE_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f1 | sed 's/^ *//; s/ *$//')"
        BASELINE_CLASS_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f2 | sed 's/^ *//; s/ *$//')"
        BASELINE_SYMBOL_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f3 | sed 's/^ *//; s/ *$//')"
        BASELINE_REASON_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f4- | sed 's/^ *//; s/ *$//')"
        if [[ "" = "${BASELINE_MODULE_STRING}" || "" = "${BASELINE_CLASS_STRING}" || "" = "${BASELINE_SYMBOL_STRING}" || "" = "${BASELINE_REASON_STRING}" ]]; then
            fail "malformed baseline line (module ~ class ~ symbols ~ reason, all mandatory): ${BASELINE_LINE_STRING}"
        fi
        if [[ "ahead" != "${BASELINE_CLASS_STRING}" && "contract" != "${BASELINE_CLASS_STRING}" ]]; then
            fail "unknown class '${BASELINE_CLASS_STRING}' in baseline line (expected ahead or contract): ${BASELINE_LINE_STRING}"
        fi
        case "${BASELINE_MODULE_STRING}${BASELINE_SYMBOL_STRING}" in
            *'*'*) fail "wildcard refused in baseline line: ${BASELINE_LINE_STRING}" ;;
        esac
        for EXISTING_INDEX_NUMBER in "${!BASELINE_MODULE_LIST[@]}"; do
            if [[ "${BASELINE_MODULE_LIST[${EXISTING_INDEX_NUMBER}]}" = "${BASELINE_MODULE_STRING}" ]]; then
                fail "duplicate baseline row for ${BASELINE_MODULE_STRING} — one module has one failure and one decision, and a second row would be read by nothing: ${BASELINE_LINE_STRING}"
            fi
        done
        BASELINE_MODULE_LIST+=("${BASELINE_MODULE_STRING}")
        BASELINE_CLASS_LIST+=("${BASELINE_CLASS_STRING}")
        BASELINE_SYMBOL_LIST+=("${BASELINE_SYMBOL_STRING}")
    done < "${BASELINE_FILE_PATH}"
fi

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

# every integration module of every major, discovered rather than listed, so a module added later joins
# this lane without anyone remembering to register it. The example applications are deliberately out of
# scope: they are consumers of the integrations rather than published modules themselves, and the pins
# they would report are the integration pins, not the framework contract this band is about.
get_integration_module_relative_path_list() {
    {
        local INTEGRATION_ROOT_STRING
        for INTEGRATION_ROOT_STRING in "${REPOSITORY_ROOT_DIRECTORY_STRING}/integrations" "${REPOSITORY_ROOT_DIRECTORY_STRING}"/v[0-9]*/integrations; do
            if [[ -d "${INTEGRATION_ROOT_STRING}" ]]; then
                find "${INTEGRATION_ROOT_STRING}" -maxdepth 5 -name go.mod -print 2>/dev/null |
                    while IFS= read -r GO_MOD_PATH_STRING; do
                        if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
                            continue
                        fi
                        local MODULE_DIRECTORY_STRING
                        MODULE_DIRECTORY_STRING="$(dirname "${GO_MOD_PATH_STRING}")"
                        printf '%s\n' "${MODULE_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
                    done
            fi
        done
    } | sort -u
}

MODULE_RELATIVE_PATH_LIST=()
while IFS= read -r MODULE_RELATIVE_PATH_STRING; do
    if [[ "" = "${MODULE_RELATIVE_PATH_STRING}" ]]; then
        continue
    fi
    MODULE_RELATIVE_PATH_LIST+=("${MODULE_RELATIVE_PATH_STRING}")
done < <(get_integration_module_relative_path_list)

if [[ 0 -eq "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "no integration module found to build — an empty module list here would report a pass over nothing"
fi

# one container invocation for the whole run: the loop runs inside, and every line of the protocol names
# the module it is about, so a module that produced no terminal line at all is detected on the host rather
# than silently counted as clean. A failure with no compiler diagnostic in it is the toolchain or the
# network failing, never a compatibility finding, and is reported as an error of the band itself.
BUILD_OUTPUT_STRING="$(
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" sh -s -- "${MODULE_RELATIVE_PATH_LIST[@]}" <<'CONTAINER_SCRIPT'
set -u
for MODULE_PATH in "$@"; do
    [ -n "${MODULE_PATH}" ] || continue

    # the module's own declaration, read through the toolchain rather than out of the file's text. A go.mod
    # states one requirement on a single `require` line and several inside a block, and a `replace` can be
    # named in a comment — so a reader that greps the file answers a question about spelling. `go mod edit
    # -json` is the same parser the toolchain uses, needs no network, and names the replaced module rather
    # than merely noticing the word.
    DECLARATION="$(cd "/app/${MODULE_PATH}" && GOWORK=off go mod edit -json 2>&1)"
    if [ 0 -ne $? ]; then
        printf '%s\tUNREADABLE\t%s\n' "${MODULE_PATH}" "$(printf '%s' "${DECLARATION}" | tr '\t\n' '  ')"
        continue
    fi

    DECLARED="$(printf '%s\n' "${DECLARATION}" | awk '
        function value(line) {
            sub(/^[^:]*:[ ]*/, "", line)
            gsub(/[",]/, "", line)
            return line
        }
        /^\t"Module": \{/  { section = "module"; next }
        /^\t"Require": \[/ { section = "require"; next }
        /^\t"Replace": \[/ { section = "replace"; next }
        /^\t"[A-Za-z]+":/  { section = "other"; entry = ""; next }
        section == "require" && /^\t\t\t"Path": /    { path = value($0); next }
        section == "require" && /^\t\t\t"Version": / { print "PIN\t" path "@" value($0); next }
        section == "replace" && /^\t\t\t"Old": \{/   { entry = "old"; next }
        section == "replace" && /^\t\t\t"New": \{/   { entry = "new"; next }
        section == "replace" && entry == "old" && /^\t\t\t\t"Path": / { print "REPLACE\t" value($0); next }
    ')"

    MELODY_REPLACE="$(printf '%s\n' "${DECLARED}" | awk -F'\t' '$1 == "REPLACE" && $2 ~ /^github\.com\/precision-soft\/melody/ { print $2 }' | tr '\n' ' ')"
    if [ -n "${MELODY_REPLACE}" ]; then
        printf '%s\tSKIPPED\ta replace directive answers %slocally, so no pinned version is left to ask about\n' "${MODULE_PATH}" "${MELODY_REPLACE}"
        continue
    fi

    MELODY_PIN_COUNT="$(printf '%s\n' "${DECLARED}" | awk -F'\t' '$1 == "PIN" && $2 ~ /^github\.com\/precision-soft\/melody/' | grep -c . || true)"
    if [ 0 -eq "${MELODY_PIN_COUNT}" ]; then
        printf '%s\tSKIPPED\tthe module declares no melody requirement, so it pins no framework version to build against\n' "${MODULE_PATH}"
        continue
    fi

    printf '%s\n' "${DECLARED}" | awk -F'\t' '$1 == "PIN" && $2 ~ /^github\.com\/precision-soft\/melody/ { print $2 }' |
        while IFS= read -r PIN_LINE; do
            [ -n "${PIN_LINE}" ] || continue
            printf '%s\tPIN\t%s\n' "${MODULE_PATH}" "${PIN_LINE}"
        done

    OUTPUT="$(cd "/app/${MODULE_PATH}" && GOWORK=off go build -gcflags=all=-e ./... 2>&1)"
    EXIT_CODE=$?

    if [ 0 -eq "${EXIT_CODE}" ]; then
        printf '%s\tBUILT\n' "${MODULE_PATH}"
        continue
    fi

    printf '%s\n' "${OUTPUT}" | tr '\t' ' ' | awk -v module="${MODULE_PATH}" 'NF { print module "\tDIAGNOSTIC\t" $0 }' | tr -d '\r'
    printf '%s\tFAILED\t%s\n' "${MODULE_PATH}" "${EXIT_CODE}"
done
CONTAINER_SCRIPT
)"

UNREADABLE_LINE_LIST_STRING="$(printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep $'\tUNREADABLE\t' || true)"
if [[ "" != "${UNREADABLE_LINE_LIST_STRING}" ]]; then
    printf '%s\n' "${UNREADABLE_LINE_LIST_STRING}" | cut -f1,3- | sed 's/^/    /'
    fail "the toolchain could not read the module declaration of the module(s) above — the band cannot tell what version they pin, and a module whose pin it cannot read is not a module that passed"
fi

TERMINAL_LINE_COUNT_NUMBER="$(printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep -cE $'\t(BUILT|SKIPPED|FAILED)(\t|$)' || true)"
if [[ "${TERMINAL_LINE_COUNT_NUMBER}" -ne "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "${TERMINAL_LINE_COUNT_NUMBER} of ${#MODULE_RELATIVE_PATH_LIST[@]} discovered modules reported a result — a module that vanished from the run is not a module that built"
fi

BUILT_COUNT_NUMBER="$(printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep -c $'\tBUILT$' || true)"
SKIPPED_LINE_LIST_STRING="$(printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep $'\tSKIPPED\t' || true)"
SKIPPED_COUNT_NUMBER="$(printf '%s\n' "${SKIPPED_LINE_LIST_STRING}" | grep -c . || true)"
FAILED_MODULE_LIST_STRING="$(printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep $'\tFAILED\t' | cut -f1 | sort -u || true)"

# a module that failed without a single compiler diagnostic did not fail on compatibility: the download
# could not resolve, the toolchain could not start, the module cache is unwritable. Reporting that as a
# finding would file a baseline row for an outage.
while IFS= read -r FAILED_MODULE_STRING; do
    if [[ "" = "${FAILED_MODULE_STRING}" ]]; then
        continue
    fi
    if ! printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep -F "${FAILED_MODULE_STRING}"$'\tDIAGNOSTIC\t' | grep -qE '\.go:[0-9]+:[0-9]+: '; then
        printf '%s\n' "${BUILD_OUTPUT_STRING}" | grep -F "${FAILED_MODULE_STRING}"$'\tDIAGNOSTIC\t' | cut -f3- | sed 's/^/    /' || true
        fail "the build of ${FAILED_MODULE_STRING} failed with no compiler diagnostic in it — that is the toolchain or the network failing, not a compatibility finding; the band cannot answer its question and reporting success here would mean it silently contributed nothing"
    fi
done <<<"${FAILED_MODULE_LIST_STRING}"

# the classifier. One line per (module, class, symbol), from the diagnostics alone: the `have`/`want`
# continuation lines the compiler prints under a method mismatch carry no site of their own and are detail
# for the reader, not facts to key on.
MEASURED_FACT_LIST_STRING="$(
    printf '%s\n' "${BUILD_OUTPUT_STRING}" |
        grep $'\tDIAGNOSTIC\t' |
        awk -F'\t' '
        function shorten(text,    result) {
            result = text
            gsub(/"/, "", result)
            sub(/^\*/, "", result)
            sub(/^github\.com\/precision-soft\/melody\//, "", result)
            sub(/^v[0-9]+\//, "", result)
            sub(/^github\.com\/precision-soft\/melody$/, "melody", result)
            return result
        }
        {
            module = $1
            message = $3
            if (message !~ /\.go:[0-9]+:[0-9]+: /) {
                next
            }
            sub(/^.*\.go:[0-9]+:[0-9]+: /, "", message)

            interface_name = ""
            if (match(message, /does not implement [^ ]+ \(/)) {
                interface_name = substr(message, RSTART + 19, RLENGTH - 21)
                interface_name = shorten(interface_name)
            }

            if (match(message, /wrong type for method [A-Za-z0-9_]+/)) {
                member = substr(message, RSTART + 22, RLENGTH - 22)
                print module "\tcontract\t" (interface_name == "" ? "" : interface_name ".") member
                next
            }
            if (match(message, /missing method [A-Za-z0-9_]+/)) {
                member = substr(message, RSTART + 15, RLENGTH - 15)
                print module "\tcontract\t" (interface_name == "" ? "" : interface_name ".") member
                next
            }
            if (message ~ /^undefined: /) {
                print module "\tahead\t" shorten(substr(message, 12))
                next
            }
            if (match(message, / undefined \(type .* has no field or method [A-Za-z0-9_]+\)/)) {
                detail = substr(message, RSTART, RLENGTH)
                type_start = index(detail, "(type ") + 6
                type_end = index(detail, " has no field or method ")
                owner = shorten(substr(detail, type_start, type_end - type_start))
                member = substr(detail, type_end + 24)
                sub(/\)$/, "", member)
                print module "\tahead\t" owner "." member
                next
            }
            gsub(/  +/, " ", message)
            print module "\tcontract\t" message
        }' |
        sort -u
)"

CLASSIFIED_MODULE_COUNT_NUMBER="$(printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | awk -F'\t' 'NF >= 3 { print $1 }' | sort -u | grep -c . || true)"

# a module's class is the strongest class among its diagnostics: one shape mismatch in a list of absences
# is still a shape mismatch, and it is the one that needs a decision.
get_module_class() {
    local MODULE_PATH_STRING="${1:?}"

    if printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | grep -Fq "${MODULE_PATH_STRING}"$'\tcontract\t'; then
        printf 'contract'
        return 0
    fi

    printf 'ahead'
}

get_module_symbol_list() {
    local MODULE_PATH_STRING="${1:?}"

    printf '%s\n' "${MEASURED_FACT_LIST_STRING}" |
        awk -F'\t' -v module="${MODULE_PATH_STRING}" 'NF >= 3 && $1 == module { print $3 }' |
        sort -u |
        paste -sd',' - |
        sed 's/,/, /g'
}

get_module_pin_list() {
    local MODULE_PATH_STRING="${1:?}"

    printf '%s\n' "${BUILD_OUTPUT_STRING}" |
        awk -F'\t' -v module="${MODULE_PATH_STRING}" 'NF >= 3 && $1 == module && $2 == "PIN" { print $3 }' |
        sort -u |
        paste -sd',' - |
        sed 's/,/, /g'
}

if [[ "true" = "${REPORT_BOOLEAN}" ]]; then
    if [[ "" = "${FAILED_MODULE_LIST_STRING}" ]]; then
        info "every one of the ${BUILT_COUNT_NUMBER} built module(s) compiles against its declared pin — no baseline row is needed"
        exit 0
    fi
    while IFS= read -r FAILED_MODULE_STRING; do
        if [[ "" = "${FAILED_MODULE_STRING}" ]]; then
            continue
        fi
        printf '%s ~ %s ~ %s ~ \n' \
            "${FAILED_MODULE_STRING}" \
            "$(get_module_class "${FAILED_MODULE_STRING}")" \
            "$(get_module_symbol_list "${FAILED_MODULE_STRING}")"
    done <<<"${FAILED_MODULE_LIST_STRING}"
    exit 0
fi

FAILED_MODULE_COUNT_NUMBER="$(printf '%s\n' "${FAILED_MODULE_LIST_STRING}" | grep -c . || true)"

info "${#MODULE_RELATIVE_PATH_LIST[@]} integration module(s) discovered: ${BUILT_COUNT_NUMBER} built against their declared pin, ${SKIPPED_COUNT_NUMBER} skipped, ${FAILED_MODULE_COUNT_NUMBER} failed, ${#BASELINE_MODULE_LIST[@]} baseline row(s)"

while IFS= read -r SKIPPED_LINE_STRING; do
    if [[ "" = "${SKIPPED_LINE_STRING}" ]]; then
        continue
    fi
    info "skipped $(printf '%s' "${SKIPPED_LINE_STRING}" | cut -f1): $(printf '%s' "${SKIPPED_LINE_STRING}" | cut -f3-)"
done <<<"${SKIPPED_LINE_LIST_STRING}"

if [[ $((BUILT_COUNT_NUMBER + SKIPPED_COUNT_NUMBER + FAILED_MODULE_COUNT_NUMBER)) -ne "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "built ${BUILT_COUNT_NUMBER} + skipped ${SKIPPED_COUNT_NUMBER} + failed ${FAILED_MODULE_COUNT_NUMBER} does not equal the ${#MODULE_RELATIVE_PATH_LIST[@]} modules discovered — the run did not account for every module"
fi

if [[ "${CLASSIFIED_MODULE_COUNT_NUMBER}" -ne "${FAILED_MODULE_COUNT_NUMBER}" ]]; then
    fail "${CLASSIFIED_MODULE_COUNT_NUMBER} of the ${FAILED_MODULE_COUNT_NUMBER} failed module(s) produced a classified diagnostic — a failure the reader could not classify is not a failure it may ignore"
fi

FAILED_BOOLEAN="false"
CONTRACT_ROW_LIST_STRING=""
AHEAD_ROW_LIST_STRING=""

while IFS= read -r FAILED_MODULE_STRING; do
    if [[ "" = "${FAILED_MODULE_STRING}" ]]; then
        continue
    fi

    MEASURED_CLASS_STRING="$(get_module_class "${FAILED_MODULE_STRING}")"
    MEASURED_SYMBOL_STRING="$(get_module_symbol_list "${FAILED_MODULE_STRING}")"

    BASELINE_INDEX_NUMBER=-1
    for INDEX_NUMBER in "${!BASELINE_MODULE_LIST[@]}"; do
        if [[ "${BASELINE_MODULE_LIST[${INDEX_NUMBER}]}" = "${FAILED_MODULE_STRING}" ]]; then
            BASELINE_INDEX_NUMBER="${INDEX_NUMBER}"
            break
        fi
    done

    if [[ -1 -eq "${BASELINE_INDEX_NUMBER}" ]]; then
        FAILED_BOOLEAN="true"
        println "unfiled: ${FAILED_MODULE_STRING} does not build against its declared pin ($(get_module_pin_list "${FAILED_MODULE_STRING}")) — class ${MEASURED_CLASS_STRING}, ${MEASURED_SYMBOL_STRING}"
        continue
    fi

    if [[ "${BASELINE_CLASS_LIST[${BASELINE_INDEX_NUMBER}]}" != "${MEASURED_CLASS_STRING}" ]]; then
        FAILED_BOOLEAN="true"
        println "class changed: ${FAILED_MODULE_STRING} is filed as ${BASELINE_CLASS_LIST[${BASELINE_INDEX_NUMBER}]} and measures ${MEASURED_CLASS_STRING} — a routine pin lag that became a contract break is exactly what this band exists to surface, and the row has to be re-decided"
        continue
    fi

    if [[ "${BASELINE_SYMBOL_LIST[${BASELINE_INDEX_NUMBER}]}" != "${MEASURED_SYMBOL_STRING}" ]]; then
        FAILED_BOOLEAN="true"
        println "symbols changed: ${FAILED_MODULE_STRING} is filed for '${BASELINE_SYMBOL_LIST[${BASELINE_INDEX_NUMBER}]}' and measures '${MEASURED_SYMBOL_STRING}' — the row describes a different failure than the one that occurs, so it has to be rewritten"
        continue
    fi

    if [[ "contract" = "${MEASURED_CLASS_STRING}" ]]; then
        CONTRACT_ROW_LIST_STRING="${CONTRACT_ROW_LIST_STRING}${FAILED_MODULE_STRING} ($(get_module_pin_list "${FAILED_MODULE_STRING}")): ${MEASURED_SYMBOL_STRING}"$'\n'
    else
        AHEAD_ROW_LIST_STRING="${AHEAD_ROW_LIST_STRING}${FAILED_MODULE_STRING} ($(get_module_pin_list "${FAILED_MODULE_STRING}")): ${MEASURED_SYMBOL_STRING}"$'\n'
    fi
done <<<"${FAILED_MODULE_LIST_STRING}"

for INDEX_NUMBER in "${!BASELINE_MODULE_LIST[@]}"; do
    if printf '%s\n' "${FAILED_MODULE_LIST_STRING}" | grep -Fxq "${BASELINE_MODULE_LIST[${INDEX_NUMBER}]}"; then
        continue
    fi

    FAILED_BOOLEAN="true"

    DISCOVERED_BOOLEAN="false"
    for MODULE_RELATIVE_PATH_STRING in "${MODULE_RELATIVE_PATH_LIST[@]}"; do
        if [[ "${MODULE_RELATIVE_PATH_STRING}" = "${BASELINE_MODULE_LIST[${INDEX_NUMBER}]}" ]]; then
            DISCOVERED_BOOLEAN="true"
            break
        fi
    done

    if [[ "false" = "${DISCOVERED_BOOLEAN}" ]]; then
        println "stale baseline row: ${BASELINE_MODULE_LIST[${INDEX_NUMBER}]} is no longer an integration module in this tree — the row names something the run cannot measure, so it has to go"
        continue
    fi

    println "stale baseline row: ${BASELINE_MODULE_LIST[${INDEX_NUMBER}]} now builds against its declared pin — the row has to go"
done

if [[ "true" = "${FAILED_BOOLEAN}" ]]; then
    fail "the modules that do not build against their declared pin and the baseline disagree — repair the break or file it with its class and a reason, and delete every stale row"
fi

# the two classes print apart, and this is the whole point of the band rather than a presentation choice:
# a run that lists nine failures together says 'nine integrations are broken' and sends the release train
# hunting phantoms, while the same nine read by cause say 'one needs a decision, the rest are a release
# step'.
if [[ "" != "${CONTRACT_ROW_LIST_STRING}" ]]; then
    println ""
    println "NON-BACKWARDS-COMPATIBLE SURFACE — the pinned version has these names and the integration no longer satisfies them."
    println "Raising the pin does not answer this: a third party implementing the same interface stops compiling too."
    while IFS= read -r CONTRACT_ROW_STRING; do
        if [[ "" = "${CONTRACT_ROW_STRING}" ]]; then
            continue
        fi
        println "    ${CONTRACT_ROW_STRING}"
    done <<<"${CONTRACT_ROW_LIST_STRING}"
    println ""
fi

if [[ "" != "${AHEAD_ROW_LIST_STRING}" ]]; then
    println "source ahead of the pinned tag — the expected state of an unreleased branch. The release train"
    println "raises these pins, the modules build, and this band then DEMANDS the rows be deleted."
    while IFS= read -r AHEAD_ROW_STRING; do
        if [[ "" = "${AHEAD_ROW_STRING}" ]]; then
            continue
        fi
        println "    ${AHEAD_ROW_STRING}"
    done <<<"${AHEAD_ROW_LIST_STRING}"
fi

success "every integration module either builds against the version its go.mod pins or is filed with its class and a reason, and every baseline row still occurs"
