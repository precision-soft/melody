#!/usr/bin/env bash

# Compares the EXPORTED surface of every published module with the surface of the last tag that module
# carries, and gates on every difference.
#
#   .dev/validate/apidiff.sh [--report]
#   .dev/validate/apidiff.sh --compare OLD NEW
#
# The band exists because no other lane asks the question. The compatibility lane next to it builds each
# integration against the framework version its go.mod pins, which is the CONSUMER's question: does what I
# publish still compile against what I declare. This lane asks the PUBLISHER's: what did I move in the API
# since the last tag — a signature changed, a symbol removed, a constant's value changed, an interface
# grown a method a third party implements — and what did I add. The first list is what a release manager
# has to decide on before a tag, one row at a time; the second is what a minor release is allowed to carry
# and a patch release is not. Neither list can be read off a changelog: the changelog carries the markers
# somebody remembered to write, and measured on this tree the marked set was a third of the measured one,
# with neither set inside the other.
#
# The measurement is apidiff's, in module mode. For each module the OLD side is the module at its last tag,
# fetched from the module proxy into the module cache, which is the published truth rather than a local
# checkout; the NEW side is this worktree. The tag is read from git at run time and printed beside the
# module, never stored: a version written into a file is a version that goes stale on its own.
#
# The NEW side loads under the workspace deliberately. Eight integrations are ahead of the framework
# version they pin, so outside the workspace they do not type-check at all, and a surface cannot be read
# off a module that does not compile. Under the workspace the surface is the one the module will publish
# once the release train raises its pins, which is the surface this lane is about; whether the pins are
# right is the compatibility lane's question, and the two stay separate. One consequence has to be
# handled rather than ignored: apidiff matches packages by their path relative to the module they belong
# to, and under the repository's full workspace the pattern `<module>/...` also loads every workspace
# module whose path sits UNDER the compared one — the nested example applications, and for the v1 modules
# the v2 and v3 modules beneath them — whose packages then land on the same relative names as the compared
# module's own and silently replace them. So the new side is loaded in a workspace built for that one
# module, holding it and the closure of the melody modules it requires and nothing else; a package of
# another workspace module that still reaches the report is an error of the run, never dropped.
#
# apidiff prints two sections, `Incompatible changes:` and `Compatible changes:`, and every row in BOTH
# starts with `- `, so the class is read from the heading a row sits under, never from its prefix. A row
# is `- <object>: <change>`; the object is `./<package>.<Name>` for a package under the module root, bare
# `<Name>` for the root package, and `package <path>` for a whole package. Anything the reader does not
# recognise is reported as an error of the run rather than skipped: an unknown line is a row the band did
# not read, and a band that skips what it does not understand has stopped asking its question. The one
# thing the tool prints that is not a row is a note about an object it reported twice, `! second,
# different message ...` with indented detail; those are forwarded verbatim as notes.
#
# Every difference is filed in `apidiff.baseline`, as module, class, object, change, disposition and
# reason, and the file is bidirectional like the compatibility, vulncheck and citation baselines: a
# measured difference with no row fails the check, a row that no longer measures fails it just as loudly,
# and a row whose class or change text differs from what the run measures fails too — the row describes a
# different fact than the one that occurs. The disposition is the decision the row carries: `keep` means
# the change ships on this major (backwards compatible, a security repair, a defect repair whose remedy
# cannot be compatible), `cut` means it leaves this major before the tag, and `unclassified` means
# measured and not yet decided. A decided row has to say why; an undecided one is allowed to say nothing,
# because "measured, undecided" is exactly what it claims and a generic reason would be a reason nobody
# can review. The undecided rows are counted in the green report: that number is the remaining work.
#
# The v1 and v2 modules are frozen and receive patch-level defect repairs only, so on them every row is a
# finding about the freeze rather than routine; they are also the band's positive control, since a run in
# which a frozen module measures nothing says the comparison ran.
#
# `--report` prints the current differences as baseline-shaped rows with the disposition left
# `unclassified` and the reason empty, for seeding the file after a release moves the tags.
#
# `--compare OLD NEW` compares one explicit pair, without the baseline, and prints the classified rows.
# OLD and NEW are each a module directory in this tree or a `path@version`. This is the second axis the
# major split needs — a copied major against the one it was copied from — and it comes with a measured
# limit: apidiff matches packages by their path relative to the module root, so two majors DO line up,
# but every signature that names a type of the module itself spells the module path into the text, and
# a renamed module makes every such signature read as changed. The reader rewrites the new module path to
# the old one in the change text before printing, which removes the rename and leaves what actually
# moved. A row that still reads `changed from X to X` after that is real: apidiff prints identical text
# when the named type behind the text changed identity, such as a type that became an alias of a type
# in another package.
#
# Like its neighbours the band needs the development container, because the Go toolchain and apidiff live
# there, and it needs the network on a cold module cache — it fetches the tagged version of each module,
# which is the whole point of comparing against what was published. apidiff is installed into the
# container on first use.

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

MODE_STRING="gate"
COMPARE_OLD_STRING=""
COMPARE_NEW_STRING=""
if [[ "--report" = "${1-}" ]]; then
    MODE_STRING="report"
    if [[ "" != "${2-}" ]]; then
        fail "--report takes no argument"
    fi
elif [[ "--compare" = "${1-}" ]]; then
    MODE_STRING="compare"
    COMPARE_OLD_STRING="${2-}"
    COMPARE_NEW_STRING="${3-}"
    if [[ "" = "${COMPARE_OLD_STRING}" || "" = "${COMPARE_NEW_STRING}" ]]; then
        fail "--compare needs OLD and NEW, each a module directory in this tree or a path@version"
    fi
    if [[ "" != "${4-}" ]]; then
        fail "--compare takes exactly two arguments"
    fi
elif [[ "" != "${1-}" ]]; then
    fail "unknown flag: ${1}"
fi

SERVICE_NAME_STRING="dev"
CONTAINER_ROOT_PATH="/app"
BASELINE_FILE_PATH="${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/apidiff.baseline"

# the baseline rows: module ~ class ~ object ~ change ~ disposition ~ reason. The first five are mandatory;
# the reason is mandatory on a decided row and allowed empty on an unclassified one. Class and change are
# measured facts and are compared for equality, so a row keyed on (module, object) whose signature moves
# again has to be rewritten — that is the row doing its job. Every field is compared for exact equality,
# so nothing in this file can act as a pattern: a `*` in an object is a pointer receiver in apidiff's
# own spelling, `(*Transport).Close`, never a wildcard. The module field alone refuses glob characters.
BASELINE_KEY_LIST=()
BASELINE_MODULE_LIST=()
BASELINE_CLASS_LIST=()
BASELINE_CHANGE_LIST=()
BASELINE_DISPOSITION_LIST=()
if [[ "gate" = "${MODE_STRING}" && -f "${BASELINE_FILE_PATH}" ]]; then
    while IFS= read -r BASELINE_LINE_STRING; do
        case "${BASELINE_LINE_STRING}" in
            ''|'#'*) continue ;;
        esac
        BASELINE_MODULE_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f1 | sed 's/^ *//; s/ *$//')"
        BASELINE_CLASS_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f2 | sed 's/^ *//; s/ *$//')"
        BASELINE_OBJECT_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f3 | sed 's/^ *//; s/ *$//')"
        BASELINE_CHANGE_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f4 | sed 's/^ *//; s/ *$//')"
        BASELINE_DISPOSITION_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f5 | sed 's/^ *//; s/ *$//')"
        BASELINE_REASON_STRING="$(printf '%s' "${BASELINE_LINE_STRING}" | cut -d'~' -f6- | sed 's/^ *//; s/ *$//')"
        FIELD_COUNT_NUMBER="$(printf '%s' "${BASELINE_LINE_STRING}" | awk -F'~' '{ print NF }')"
        if [[ "${FIELD_COUNT_NUMBER}" -lt 6 ]]; then
            fail "malformed baseline line (module ~ class ~ object ~ change ~ disposition ~ reason, six fields): ${BASELINE_LINE_STRING}"
        fi
        if [[ "" = "${BASELINE_MODULE_STRING}" || "" = "${BASELINE_CLASS_STRING}" || "" = "${BASELINE_OBJECT_STRING}" || "" = "${BASELINE_CHANGE_STRING}" || "" = "${BASELINE_DISPOSITION_STRING}" ]]; then
            fail "malformed baseline line (module, class, object, change and disposition are mandatory): ${BASELINE_LINE_STRING}"
        fi
        if [[ "incompatible" != "${BASELINE_CLASS_STRING}" && "compatible" != "${BASELINE_CLASS_STRING}" ]]; then
            fail "unknown class '${BASELINE_CLASS_STRING}' in baseline line (expected incompatible or compatible): ${BASELINE_LINE_STRING}"
        fi
        if [[ "unclassified" != "${BASELINE_DISPOSITION_STRING}" && "keep" != "${BASELINE_DISPOSITION_STRING}" && "cut" != "${BASELINE_DISPOSITION_STRING}" ]]; then
            fail "unknown disposition '${BASELINE_DISPOSITION_STRING}' in baseline line (expected unclassified, keep or cut): ${BASELINE_LINE_STRING}"
        fi
        if [[ "unclassified" != "${BASELINE_DISPOSITION_STRING}" && "" = "${BASELINE_REASON_STRING}" ]]; then
            fail "a decided row has to say why — '${BASELINE_DISPOSITION_STRING}' with an empty reason is a decision nobody can review: ${BASELINE_LINE_STRING}"
        fi
        case "${BASELINE_MODULE_STRING}" in
            *'*'*|*'?'*) fail "wildcard refused in baseline line: ${BASELINE_LINE_STRING}" ;;
        esac
        BASELINE_KEY_STRING="${BASELINE_MODULE_STRING}"$'\t'"${BASELINE_OBJECT_STRING}"
        for EXISTING_INDEX_NUMBER in "${!BASELINE_KEY_LIST[@]}"; do
            if [[ "${BASELINE_KEY_LIST[${EXISTING_INDEX_NUMBER}]}" = "${BASELINE_KEY_STRING}" ]]; then
                fail "duplicate baseline row for ${BASELINE_OBJECT_STRING} in ${BASELINE_MODULE_STRING} — one object has one change and one decision, and a second row would be read by nothing: ${BASELINE_LINE_STRING}"
            fi
        done
        BASELINE_KEY_LIST+=("${BASELINE_KEY_STRING}")
        BASELINE_MODULE_LIST+=("${BASELINE_MODULE_STRING}")
        BASELINE_CLASS_LIST+=("${BASELINE_CLASS_STRING}")
        BASELINE_CHANGE_LIST+=("${BASELINE_CHANGE_STRING}")
        BASELINE_DISPOSITION_LIST+=("${BASELINE_DISPOSITION_STRING}")
    done < "${BASELINE_FILE_PATH}"
fi

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

# every published module of every major, discovered rather than listed: the root module, the versioned
# majors, and every integration module. The example applications are out by rule, not by accident — they
# are consumers, carry no tag, and publish no surface — and so is the e2e harness.
get_published_module_relative_path_list() {
    {
        if [[ -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/go.mod" ]]; then
            printf '.\n'
        fi

        local VERSIONED_DIRECTORY_STRING
        for VERSIONED_DIRECTORY_STRING in "${REPOSITORY_ROOT_DIRECTORY_STRING}"/v[0-9]*/; do
            VERSIONED_DIRECTORY_STRING="${VERSIONED_DIRECTORY_STRING%/}"
            if [[ -f "${VERSIONED_DIRECTORY_STRING}/go.mod" ]]; then
                printf '%s\n' "${VERSIONED_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
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
                        case "${GO_MOD_PATH_STRING}" in
                            */.example/*) continue ;;
                        esac
                        local MODULE_DIRECTORY_STRING
                        MODULE_DIRECTORY_STRING="$(dirname "${GO_MOD_PATH_STRING}")"
                        printf '%s\n' "${MODULE_DIRECTORY_STRING#${REPOSITORY_ROOT_DIRECTORY_STRING}/}"
                    done
            fi
        done
    } | sort -u
}

# the module path of a directory, read through the toolchain rather than out of the file's text: the
# module directive has two spellings and a reader that greps for one answers a question about spelling.
read_module_path_list() {
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" sh -s -- "$@" <<'CONTAINER_SCRIPT'
set -u
for MODULE_PATH in "$@"; do
    [ -n "${MODULE_PATH}" ] || continue
    DECLARATION="$(cd "/app/${MODULE_PATH}" && GOWORK=off go mod edit -json 2>&1)"
    if [ 0 -ne $? ]; then
        printf '%s\tUNREADABLE\t%s\n' "${MODULE_PATH}" "$(printf '%s' "${DECLARATION}" | tr '\t\n' '  ')"
        continue
    fi
    printf '%s\tMODULE\t%s\n' "${MODULE_PATH}" "$(printf '%s\n' "${DECLARATION}" | awk '
        /^\t"Module": \{/ { section = "module"; next }
        /^\t"[A-Za-z]+":/ { section = "other"; next }
        section == "module" && /^\t\t"Path": / { line = $0; sub(/^[^:]*:[ ]*/, "", line); gsub(/[",]/, "", line); print line; exit }
    ')"
done
CONTAINER_SCRIPT
}

# the last tag of a module, from the module path alone: the repository tags a module under the directory
# convention Go prescribes — `<subdirectory>/vN.x.y`, with the major suffix folded into the version — so
# the prefix is the module's sub-path without its major suffix and the version pattern is its major.
get_latest_tag() {
    local MODULE_PATH_STRING="${1:?}"

    local SUB_PATH_STRING="${MODULE_PATH_STRING#github.com/precision-soft/melody}"
    SUB_PATH_STRING="${SUB_PATH_STRING#/}"
    local MAJOR_NUMBER=1
    if [[ "${SUB_PATH_STRING}" =~ (^|/)v([0-9]+)$ ]]; then
        MAJOR_NUMBER="${BASH_REMATCH[2]}"
        SUB_PATH_STRING="${SUB_PATH_STRING%v${MAJOR_NUMBER}}"
        SUB_PATH_STRING="${SUB_PATH_STRING%/}"
    fi
    local PREFIX_STRING=""
    if [[ "" != "${SUB_PATH_STRING}" ]]; then
        PREFIX_STRING="${SUB_PATH_STRING}/"
    fi

    git tag -l "${PREFIX_STRING}v${MAJOR_NUMBER}.*" | sort -V | awk 'END { if (NR) print }'
}

# one container invocation for the whole run. Each line of the input names a module directory, its module
# path and its OLD side — `path@version` to fetch, or `dir:<relative path>` for a directory of this tree.
# Every line of the protocol names the module it is about, so a module that produced no terminal line is
# detected on the host rather than silently counted as clean. The reader drops packages that belong to
# ANOTHER workspace module beneath the compared one, and says how many.
run_comparisons() {
    docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" sh -s -- "$@" <<'CONTAINER_SCRIPT'
set -u
if ! command -v apidiff >/dev/null 2>&1; then
    if ! go install golang.org/x/exp/cmd/apidiff@latest >/dev/null 2>&1; then
        printf 'PROTOCOL\tINSTALL-FAILED\n'
        exit 0
    fi
fi
if ! command -v apidiff >/dev/null 2>&1; then
    printf 'PROTOCOL\tINSTALL-FAILED\n'
    exit 0
fi

# every workspace module, path and directory, for building the per-module workspaces below
WORKSPACE_MODULE_TABLE="$(cd /app && go list -m -json 2>/dev/null | awk -F'"' '
    /^\t"Path": / { path = $4 }
    /^\t"Dir": /  { print path "\t" $4 }
')"
WORKSPACE_GO_VERSION="$(cd /app && go work edit -json 2>/dev/null | awk -F'"' '/^\t"Go": / { print $4; exit }')"

module_path_of_directory() {
    (cd "${1}" && GOWORK=off go mod edit -json 2>/dev/null) | awk '
        /^\t"Module": \{/ { section = "module"; next }
        /^\t"[A-Za-z]+":/ { section = "other"; next }
        section == "module" && /^\t\t"Path": / { line = $0; sub(/^[^:]*:[ ]*/, "", line); gsub(/[",]/, "", line); print line; exit }
    '
}

melody_requirements_of_directory() {
    (cd "${1}" && GOWORK=off go mod edit -json 2>/dev/null) | awk '
        /^\t"Require": \[/ { section = "require"; next }
        /^\t"[A-Za-z]+":/  { section = "other"; next }
        section == "require" && /^\t\t\t"Path": / { line = $0; sub(/^[^:]*:[ ]*/, "", line); gsub(/[",]/, "", line); if (index(line, "github.com/precision-soft/melody") == 1) print line }
    '
}

# a workspace holding exactly the module and the closure of the melody modules it requires, and nothing
# else. apidiff matches packages by their path relative to the module they belong to, so under the full
# workspace a package of a module BENEATH the compared one — `.example/cache` beneath the root module,
# `v3/kernel` beneath it too — lands on the same relative name as the compared module's own package and
# silently replaces it: measured, the root module then read as 244 symbols removed and 131 added, every
# one of them another module's. A workspace that contains only what the module requires cannot fold a
# stranger in, and a stranger that still appears is reported as an error rather than dropped.
build_workspace_for() {
    WORKSPACE_DIRECTORY="${1}"
    MODULE_DIRECTORY="${2}"
    rm -rf "${WORKSPACE_DIRECTORY}"
    mkdir -p "${WORKSPACE_DIRECTORY}"
    MEMBER_LIST="${MODULE_DIRECTORY}"
    PENDING_LIST="$(melody_requirements_of_directory "${MODULE_DIRECTORY}")"
    SEEN_LIST=""
    while [ -n "${PENDING_LIST}" ]; do
        REQUIRED_PATH="$(printf '%s\n' "${PENDING_LIST}" | sed -n 1p)"
        PENDING_LIST="$(printf '%s\n' "${PENDING_LIST}" | sed 1d)"
        [ -n "${REQUIRED_PATH}" ] || continue
        case "${SEEN_LIST}" in *"|${REQUIRED_PATH}|"*) continue ;; esac
        SEEN_LIST="${SEEN_LIST}|${REQUIRED_PATH}|"
        REQUIRED_DIRECTORY="$(printf '%s\n' "${WORKSPACE_MODULE_TABLE}" | awk -F'\t' -v path="${REQUIRED_PATH}" '$1 == path { print $2; exit }')"
        [ -n "${REQUIRED_DIRECTORY}" ] || continue
        MEMBER_LIST="${MEMBER_LIST}
${REQUIRED_DIRECTORY}"
        PENDING_LIST="${PENDING_LIST}
$(melody_requirements_of_directory "${REQUIRED_DIRECTORY}")"
    done
    {
        printf 'go %s\n\nuse (\n' "${WORKSPACE_GO_VERSION}"
        printf '%s\n' "${MEMBER_LIST}" | awk 'NF && !seen[$0]++ { print "\t" $0 }'
        printf ')\n'
    } >"${WORKSPACE_DIRECTORY}/go.work"
    if [ -f /app/go.work.sum ]; then
        cp /app/go.work.sum "${WORKSPACE_DIRECTORY}/go.work.sum"
    fi
}

for INPUT_LINE in "$@"; do
    [ -n "${INPUT_LINE}" ] || continue
    MODULE_PATH="$(printf '%s' "${INPUT_LINE}" | cut -f1)"
    NEW_MODULE_PATH="$(printf '%s' "${INPUT_LINE}" | cut -f2)"
    OLD_SPEC="$(printf '%s' "${INPUT_LINE}" | cut -f3)"

    case "${OLD_SPEC}" in
        dir:*)
            OLD_DIRECTORY="/app/${OLD_SPEC#dir:}"
            OLD_MODULE_PATH="$(module_path_of_directory "${OLD_DIRECTORY}")"
            if [ -z "${OLD_MODULE_PATH}" ]; then
                printf '%s\tERROR\tthe old side %s declares no readable module path\n' "${MODULE_PATH}" "${OLD_DIRECTORY}"
                continue
            fi
            build_workspace_for /tmp/apidiff-old-workspace "${OLD_DIRECTORY}"
            OLD_GOWORK="/tmp/apidiff-old-workspace/go.work"
            ;;
        *)
            # a module fetched from the proxy is read in module mode whatever it carries: the root module's
            # zip ships the repository's go.work, whose `use` entries name directories the zip does not
            # contain, and the toolchain would otherwise pick that file up and refuse the whole load
            OLD_GOWORK="off"
            OLD_MODULE_PATH="${OLD_SPEC%@*}"
            DOWNLOAD="$(cd "/app/${MODULE_PATH}" && GOWORK=off go mod download -json "${OLD_SPEC}" 2>&1)"
            OLD_DIRECTORY="$(printf '%s\n' "${DOWNLOAD}" | awk -F'"' '/^\t"Dir": / { print $4; exit }')"
            if [ -z "${OLD_DIRECTORY}" ] || [ ! -d "${OLD_DIRECTORY}" ]; then
                printf '%s\tERROR\tcould not fetch %s: %s\n' "${MODULE_PATH}" "${OLD_SPEC}" "$(printf '%s' "${DOWNLOAD}" | tr '\t\n' '  ' | tail -c 600)"
                continue
            fi
            ;;
    esac

    EXPORT_FILE="/tmp/apidiff-old.export"
    rm -f "${EXPORT_FILE}"
    EXPORT_OUTPUT="$(cd "${OLD_DIRECTORY}" && GOWORK="${OLD_GOWORK}" apidiff -m -w "${EXPORT_FILE}" "${OLD_MODULE_PATH}" 2>&1)"
    if [ 0 -ne $? ] || [ ! -s "${EXPORT_FILE}" ]; then
        printf '%s\tERROR\tcould not read the old surface of %s in %s: %s\n' "${MODULE_PATH}" "${OLD_MODULE_PATH}" "${OLD_DIRECTORY}" "$(printf '%s' "${EXPORT_OUTPUT}" | grep -v '^Ignoring internal package' | tr '\t\n' '  ' | tail -c 600)"
        continue
    fi

    build_workspace_for /tmp/apidiff-new-workspace "/app/${MODULE_PATH}"

    # apidiff iterates a type's method set as a map (compatibility.go, `range oldMethodSet`), so when one
    # method still takes an old named type and a sibling method now takes a new one, which method is
    # compared first decides which new type the old one is taken to correspond to — and the two answers
    # print different rows: the honest one names the method whose parameter changed, the other declares
    # the old type itself changed and hides the method. Measured on this tree the wrong reading came up in
    # about three runs of five (20 of 30, then 17 of 30). It has a signature nothing else produces: an
    # object reported as `changed from T to T` under its OWN name, which can only come from a contested
    # correspondence. A run carrying that row is retried; the retry stops at the first clean run, so the
    # expected cost is two or three runs of about a second, and the cap of forty only bounds a tail with a
    # probability near one in a thousand million. A module that never yields an uncontested run fails
    # naming the objects, rather than filing a row that a later run would call stale.
    REPORT_FILE="/tmp/apidiff-report.txt"
    ERROR_FILE="/tmp/apidiff-error.txt"
    ATTEMPT_NUMBER=0
    CONTESTED_LIST=""
    while [ "${ATTEMPT_NUMBER}" -lt 40 ]; do
        ATTEMPT_NUMBER=$((ATTEMPT_NUMBER + 1))
        (cd "/app/${MODULE_PATH}" && GOWORK=/tmp/apidiff-new-workspace/go.work apidiff -m "${EXPORT_FILE}" "${NEW_MODULE_PATH}" >"${REPORT_FILE}" 2>"${ERROR_FILE}")
        EXIT_CODE=$?
        if [ 0 -ne "${EXIT_CODE}" ]; then
            break
        fi
        CONTESTED_LIST="$(awk '
            /^- / {
                line = substr($0, 3)
                separator = index(line, ": ")
                if (0 == separator) next
                object = substr(line, 1, separator - 1)
                change = substr(line, separator + 2)
                name = object
                sub(/^.*[.\/]/, "", name)
                if (change == "changed from " name " to " name) print object
            }' "${REPORT_FILE}")"
        if [ -z "${CONTESTED_LIST}" ]; then
            break
        fi
    done
    if [ 0 -ne "${EXIT_CODE}" ]; then
        printf '%s\tERROR\tapidiff exited %s comparing %s: %s\n' "${MODULE_PATH}" "${EXIT_CODE}" "${NEW_MODULE_PATH}" "$(grep -v '^Ignoring internal package' "${ERROR_FILE}" | tr '\t\n' '  ' | tail -c 600)"
        continue
    fi
    if [ -n "${CONTESTED_LIST}" ]; then
        printf '%s\tERROR\tapidiff resolved a contested type correspondence the wrong way in every one of %s runs, reporting %s as changed from its own name to its own name — the reading is not deterministic for this module and cannot be filed\n' "${MODULE_PATH}" "${ATTEMPT_NUMBER}" "$(printf '%s' "${CONTESTED_LIST}" | tr '\n' ' ')"
        continue
    fi

    printf '%s\tOLD\t%s\n' "${MODULE_PATH}" "${OLD_SPEC}"
    printf '%s\tATTEMPTS\t%s\n' "${MODULE_PATH}" "${ATTEMPT_NUMBER}"

    tr '\t' ' ' <"${REPORT_FILE}" | tr -d '\r' | awk -v module="${MODULE_PATH}" -v newpath="${NEW_MODULE_PATH}" -v oldpath="${OLD_MODULE_PATH}" -v workspace="$(printf '%s\n' "${WORKSPACE_MODULE_TABLE}" | cut -f1)" '
        BEGIN {
            count = split(workspace, entry, "\n")
            for (i = 1; i <= count; i++) {
                if ("" == entry[i]) {
                    continue
                }
                if (entry[i] != newpath && index(entry[i], newpath "/") == 1) {
                    foreign[entry[i]] = 1
                }
                if (entry[i] != oldpath && index(entry[i], oldpath "/") == 1) {
                    foreign[entry[i]] = 1
                }
            }
            section = ""
        }
        function package_of(object, base,    rest, parts, count, last, dot) {
            if (object ~ /^package /) {
                return substr(object, 9)
            }
            if (object ~ /^\.\//) {
                rest = substr(object, 3)
                count = split(rest, parts, "/")
                last = parts[count]
                dot = index(last, ".")
                if (0 == dot) {
                    return base "/" rest
                }
                return base "/" substr(rest, 1, length(rest) - length(last) + dot - 1)
            }
            return base
        }
        function replace_all(text, from, to,    result, position) {
            result = ""
            while (0 != (position = index(text, from))) {
                result = result substr(text, 1, position - 1) to
                text = substr(text, position + length(from))
            }
            return result text
        }
        function is_foreign(package_path,    key) {
            for (key in foreign) {
                if (package_path == key || index(package_path, key "/") == 1) {
                    return 1
                }
            }
            return 0
        }
        /^Incompatible changes:$/ { section = "incompatible"; next }
        /^Compatible changes:$/ { section = "compatible"; next }
        /^- / {
            line = substr($0, 3)
            separator = index(line, ": ")
            if (0 == separator || "" == section) {
                print module "\tUNPARSED\t" $0
                next
            }
            object = substr(line, 1, separator - 1)
            change = substr(line, separator + 2)
            package_path = package_of(object, newpath)
            if (object ~ /^package / && index(package_path, oldpath "/") == 1 && index(package_path, newpath "/") != 1) {
                base = oldpath
            } else {
                base = newpath
            }
            if (is_foreign(package_path)) {
                print module "\tFOREIGN\t" package_path
                next
            }
            if (object ~ /^package /) {
                if (package_path == base) {
                    object = "package ."
                } else {
                    object = "package ./" substr(package_path, length(base) + 2)
                }
            }
            if (newpath != oldpath) {
                change = replace_all(change, newpath, oldpath)
            }
            print module "\tCHANGE\t" section "\t" object "\t" change
            next
        }
        /^! / || /^  / { print module "\tNOTE\t" $0; next }
        NF { print module "\tUNPARSED\t" $0 }
    '
    printf '%s\tCOMPARED\n' "${MODULE_PATH}"
done
CONTAINER_SCRIPT
}

# the facts of a run, one per line: module ~ class ~ object ~ change, tab separated, from the protocol
read_measured_fact_list() {
    printf '%s\n' "${1}" | awk -F'\t' '$2 == "CHANGE" && NF >= 5 { print $1 "\t" $3 "\t" $4 "\t" $5 }' | sort -u
}

verify_protocol() {
    local OUTPUT_STRING="${1}"
    local EXPECTED_COUNT_NUMBER="${2}"

    if printf '%s\n' "${OUTPUT_STRING}" | grep -Fxq $'PROTOCOL\tINSTALL-FAILED'; then
        fail "apidiff is not installed in the ${SERVICE_NAME_STRING} container and installing it failed — the band cannot run, and reporting success here would mean it silently contributed nothing"
    fi

    local ERROR_LINE_LIST_STRING
    ERROR_LINE_LIST_STRING="$(printf '%s\n' "${OUTPUT_STRING}" | grep $'\tERROR\t' || true)"
    if [[ "" != "${ERROR_LINE_LIST_STRING}" ]]; then
        printf '%s\n' "${ERROR_LINE_LIST_STRING}" | cut -f1,3- | sed 's/^/    /'
        fail "the comparison of the module(s) above did not run — that is the proxy, the toolchain or apidiff failing, never a surface finding; the band cannot answer its question and a pass here would mean it silently contributed nothing"
    fi

    local UNPARSED_LINE_LIST_STRING
    UNPARSED_LINE_LIST_STRING="$(printf '%s\n' "${OUTPUT_STRING}" | grep $'\tUNPARSED\t' || true)"
    if [[ "" != "${UNPARSED_LINE_LIST_STRING}" ]]; then
        printf '%s\n' "${UNPARSED_LINE_LIST_STRING}" | cut -f1,3- | sed 's/^/    /'
        fail "apidiff printed the line(s) above in a form this reader does not recognise — a row the band did not read is a row it cannot file, so the reader has to learn the form before the run can pass"
    fi

    local COMPARED_COUNT_NUMBER
    COMPARED_COUNT_NUMBER="$(printf '%s\n' "${OUTPUT_STRING}" | grep -c $'\tCOMPARED$' || true)"
    if [[ "${COMPARED_COUNT_NUMBER}" -ne "${EXPECTED_COUNT_NUMBER}" ]]; then
        fail "${COMPARED_COUNT_NUMBER} of ${EXPECTED_COUNT_NUMBER} module(s) reported a comparison — a module that vanished from the run is not a module whose surface held"
    fi

    local FOREIGN_LINE_LIST_STRING
    FOREIGN_LINE_LIST_STRING="$(printf '%s\n' "${OUTPUT_STRING}" | grep $'\tFOREIGN\t' || true)"
    if [[ "" != "${FOREIGN_LINE_LIST_STRING}" ]]; then
        printf '%s\n' "${FOREIGN_LINE_LIST_STRING}" | cut -f1,3- | sed 's/^/    /'
        fail "the package(s) above belong to another workspace module beneath the compared one and still reached the report — the per-module workspace did not isolate the module, and a surface read over a stranger's packages is not the module's surface"
    fi

    printf '%s\n' "${OUTPUT_STRING}" | awk -F'\t' '$2 == "ATTEMPTS" && $3 > 1 { print $1 "\t" $3 }' |
        while IFS=$'\t' read -r RETRIED_MODULE_STRING ATTEMPT_COUNT_STRING; do
            println "    ${RETRIED_MODULE_STRING}: apidiff needed ${ATTEMPT_COUNT_STRING} runs to resolve a contested type correspondence by name" >&2
        done

    while IFS= read -r NOTE_LINE_STRING; do
        if [[ "" = "${NOTE_LINE_STRING}" ]]; then
            continue
        fi
        println "    apidiff note on $(printf '%s' "${NOTE_LINE_STRING}" | cut -f1): $(printf '%s' "${NOTE_LINE_STRING}" | cut -f3-)" >&2
    done <<<"$(printf '%s\n' "${OUTPUT_STRING}" | grep $'\tNOTE\t' || true)"
}

get_old_spec_of_module() {
    local MODULE_PATH_STRING="${1:?}"
    printf '%s\n' "${COMPARISON_OUTPUT_STRING}" | awk -F'\t' -v module="${MODULE_PATH_STRING}" '$1 == module && $2 == "OLD" { print $3; exit }'
}

# ---------------------------------------------------------------------------------------------------------
# --compare OLD NEW: one explicit pair, no baseline
# ---------------------------------------------------------------------------------------------------------

resolve_compare_side() {
    local SIDE_STRING="${1:?}"
    if [[ "${SIDE_STRING}" = *@* ]]; then
        printf '%s' "${SIDE_STRING}"
        return 0
    fi
    local RELATIVE_PATH_STRING="${SIDE_STRING#./}"
    RELATIVE_PATH_STRING="${RELATIVE_PATH_STRING%/}"
    if [[ ! -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/${RELATIVE_PATH_STRING}/go.mod" ]]; then
        fail "${SIDE_STRING} is neither a path@version nor a module directory of this tree"
    fi
    printf 'dir:%s' "${RELATIVE_PATH_STRING}"
}

if [[ "compare" = "${MODE_STRING}" ]]; then
    OLD_SPEC_STRING="$(resolve_compare_side "${COMPARE_OLD_STRING}")"
    NEW_SPEC_STRING="$(resolve_compare_side "${COMPARE_NEW_STRING}")"
    if [[ "${NEW_SPEC_STRING}" != dir:* ]]; then
        fail "NEW has to be a module directory of this tree — the new side is what the worktree publishes"
    fi
    NEW_RELATIVE_PATH_STRING="${NEW_SPEC_STRING#dir:}"

    DECLARATION_OUTPUT_STRING="$(read_module_path_list "${NEW_RELATIVE_PATH_STRING}")"
    NEW_MODULE_PATH_STRING="$(printf '%s\n' "${DECLARATION_OUTPUT_STRING}" | awk -F'\t' '$2 == "MODULE" { print $3; exit }')"
    if [[ "" = "${NEW_MODULE_PATH_STRING}" ]]; then
        printf '%s\n' "${DECLARATION_OUTPUT_STRING}" | sed 's/^/    /'
        fail "the toolchain could not read the module declaration of ${NEW_RELATIVE_PATH_STRING}"
    fi

    COMPARISON_OUTPUT_STRING="$(run_comparisons "${NEW_RELATIVE_PATH_STRING}"$'\t'"${NEW_MODULE_PATH_STRING}"$'\t'"${OLD_SPEC_STRING}")"
    verify_protocol "${COMPARISON_OUTPUT_STRING}" 1

    MEASURED_FACT_LIST_STRING="$(read_measured_fact_list "${COMPARISON_OUTPUT_STRING}")"
    INCOMPATIBLE_COUNT_NUMBER="$(printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | awk -F'\t' '$2 == "incompatible"' | grep -c . || true)"
    COMPATIBLE_COUNT_NUMBER="$(printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | awk -F'\t' '$2 == "compatible"' | grep -c . || true)"
    info "${NEW_RELATIVE_PATH_STRING} against ${COMPARE_OLD_STRING}: ${INCOMPATIBLE_COUNT_NUMBER} incompatible, ${COMPATIBLE_COUNT_NUMBER} compatible"
    printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | awk -F'\t' 'NF >= 4 { print $1 " ~ " $2 " ~ " $3 " ~ " $4 }'
    exit 0
fi

# ---------------------------------------------------------------------------------------------------------
# the gate and --report: every published module against its last tag
# ---------------------------------------------------------------------------------------------------------

MODULE_RELATIVE_PATH_LIST=()
while IFS= read -r MODULE_RELATIVE_PATH_STRING; do
    if [[ "" = "${MODULE_RELATIVE_PATH_STRING}" ]]; then
        continue
    fi
    MODULE_RELATIVE_PATH_LIST+=("${MODULE_RELATIVE_PATH_STRING}")
done < <(get_published_module_relative_path_list)

if [[ 0 -eq "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "no published module found to compare — an empty module list here would report a pass over nothing"
fi

DECLARATION_OUTPUT_STRING="$(read_module_path_list "${MODULE_RELATIVE_PATH_LIST[@]}")"

UNREADABLE_LINE_LIST_STRING="$(printf '%s\n' "${DECLARATION_OUTPUT_STRING}" | grep $'\tUNREADABLE\t' || true)"
if [[ "" != "${UNREADABLE_LINE_LIST_STRING}" ]]; then
    printf '%s\n' "${UNREADABLE_LINE_LIST_STRING}" | cut -f1,3- | sed 's/^/    /'
    fail "the toolchain could not read the module declaration of the module(s) above — the band cannot tell what they publish, and a module whose path it cannot read is not a module that passed"
fi

# the comparison input: module directory, module path, old side — one line per module that has a tag. A
# module with no tag has no published surface to compare against, so it is skipped with its reason and
# counted; compared + skipped has to equal discovered.
COMPARISON_INPUT_LIST=()
SKIPPED_LINE_LIST=()
for MODULE_RELATIVE_PATH_STRING in "${MODULE_RELATIVE_PATH_LIST[@]}"; do
    MODULE_PATH_STRING="$(printf '%s\n' "${DECLARATION_OUTPUT_STRING}" | awk -F'\t' -v module="${MODULE_RELATIVE_PATH_STRING}" '$1 == module && $2 == "MODULE" { print $3; exit }')"
    if [[ "" = "${MODULE_PATH_STRING}" ]]; then
        fail "${MODULE_RELATIVE_PATH_STRING} produced no module path — the declaration reader did not account for every module"
    fi
    LATEST_TAG_STRING="$(get_latest_tag "${MODULE_PATH_STRING}")"
    if [[ "" = "${LATEST_TAG_STRING}" ]]; then
        SKIPPED_LINE_LIST+=("${MODULE_RELATIVE_PATH_STRING}: ${MODULE_PATH_STRING} carries no tag, so it has no published surface to compare against")
        continue
    fi
    VERSION_STRING="${LATEST_TAG_STRING##*/}"
    COMPARISON_INPUT_LIST+=("${MODULE_RELATIVE_PATH_STRING}"$'\t'"${MODULE_PATH_STRING}"$'\t'"${MODULE_PATH_STRING}@${VERSION_STRING}")
done

if [[ 0 -eq "${#COMPARISON_INPUT_LIST[@]}" ]]; then
    fail "none of the ${#MODULE_RELATIVE_PATH_LIST[@]} discovered module(s) carries a tag — nothing to compare, and a pass over nothing is not a pass"
fi

COMPARISON_OUTPUT_STRING="$(run_comparisons "${COMPARISON_INPUT_LIST[@]}")"
verify_protocol "${COMPARISON_OUTPUT_STRING}" "${#COMPARISON_INPUT_LIST[@]}"

MEASURED_FACT_LIST_STRING="$(read_measured_fact_list "${COMPARISON_OUTPUT_STRING}")"

TILDE_FACT_LIST_STRING="$(printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | grep '~' || true)"
if [[ "" != "${TILDE_FACT_LIST_STRING}" ]]; then
    printf '%s\n' "${TILDE_FACT_LIST_STRING}" | sed 's/^/    /'
    fail "the fact(s) above carry a '~', which is the baseline's field separator — they cannot be filed as they are, and the reader has to learn an escape before the run can pass"
fi

if [[ "report" = "${MODE_STRING}" ]]; then
    if [[ "" = "${MEASURED_FACT_LIST_STRING}" ]]; then
        info "no published module differs from its last tag — no baseline row is needed"
        exit 0
    fi
    printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | awk -F'\t' 'NF >= 4 { print $1 " ~ " $2 " ~ " $3 " ~ " $4 " ~ unclassified ~ " }'
    exit 0
fi

MEASURED_COUNT_NUMBER="$(printf '%s\n' "${MEASURED_FACT_LIST_STRING}" | grep -c . || true)"

info "${#MODULE_RELATIVE_PATH_LIST[@]} published module(s) discovered: ${#COMPARISON_INPUT_LIST[@]} compared with their last tag, ${#SKIPPED_LINE_LIST[@]} skipped, ${MEASURED_COUNT_NUMBER} difference(s) measured, ${#BASELINE_KEY_LIST[@]} baseline row(s)"

for SKIPPED_LINE_STRING in "${SKIPPED_LINE_LIST[@]}"; do
    info "skipped ${SKIPPED_LINE_STRING}"
done

if [[ $((${#COMPARISON_INPUT_LIST[@]} + ${#SKIPPED_LINE_LIST[@]})) -ne "${#MODULE_RELATIVE_PATH_LIST[@]}" ]]; then
    fail "compared ${#COMPARISON_INPUT_LIST[@]} + skipped ${#SKIPPED_LINE_LIST[@]} does not equal the ${#MODULE_RELATIVE_PATH_LIST[@]} modules discovered — the run did not account for every module"
fi

FAILED_BOOLEAN="false"
MATCHED_INDEX_LIST_STRING=""
DECIDED_ROW_LIST_STRING=""

while IFS=$'\t' read -r FACT_MODULE_STRING FACT_CLASS_STRING FACT_OBJECT_STRING FACT_CHANGE_STRING; do
    if [[ "" = "${FACT_MODULE_STRING}" ]]; then
        continue
    fi

    FACT_KEY_STRING="${FACT_MODULE_STRING}"$'\t'"${FACT_OBJECT_STRING}"
    BASELINE_INDEX_NUMBER=-1
    for INDEX_NUMBER in "${!BASELINE_KEY_LIST[@]}"; do
        if [[ "${BASELINE_KEY_LIST[${INDEX_NUMBER}]}" = "${FACT_KEY_STRING}" ]]; then
            BASELINE_INDEX_NUMBER="${INDEX_NUMBER}"
            break
        fi
    done

    if [[ -1 -eq "${BASELINE_INDEX_NUMBER}" ]]; then
        FAILED_BOOLEAN="true"
        println "unfiled: ${FACT_MODULE_STRING} ($(get_old_spec_of_module "${FACT_MODULE_STRING}")) ${FACT_CLASS_STRING} ${FACT_OBJECT_STRING}: ${FACT_CHANGE_STRING}"
        continue
    fi

    MATCHED_INDEX_LIST_STRING="${MATCHED_INDEX_LIST_STRING}${BASELINE_INDEX_NUMBER}"$'\n'

    if [[ "${BASELINE_CLASS_LIST[${BASELINE_INDEX_NUMBER}]}" != "${FACT_CLASS_STRING}" ]]; then
        FAILED_BOOLEAN="true"
        println "class changed: ${FACT_MODULE_STRING} ${FACT_OBJECT_STRING} is filed as ${BASELINE_CLASS_LIST[${BASELINE_INDEX_NUMBER}]} and measures ${FACT_CLASS_STRING} — an addition that became a break, or a break that became an addition, is a different decision, and the row has to be re-decided"
        continue
    fi

    if [[ "${BASELINE_CHANGE_LIST[${BASELINE_INDEX_NUMBER}]}" != "${FACT_CHANGE_STRING}" ]]; then
        FAILED_BOOLEAN="true"
        println "change text changed: ${FACT_MODULE_STRING} ${FACT_OBJECT_STRING} is filed as '${BASELINE_CHANGE_LIST[${BASELINE_INDEX_NUMBER}]}' and measures '${FACT_CHANGE_STRING}' — the row describes a different change than the one that occurs, so it has to be rewritten"
        continue
    fi

    DECIDED_ROW_LIST_STRING="${DECIDED_ROW_LIST_STRING}${FACT_MODULE_STRING}"$'\t'"${FACT_CLASS_STRING}"$'\t'"${BASELINE_DISPOSITION_LIST[${BASELINE_INDEX_NUMBER}]}"$'\t'"${FACT_OBJECT_STRING}"$'\t'"${FACT_CHANGE_STRING}"$'\n'
done <<<"${MEASURED_FACT_LIST_STRING}"

for INDEX_NUMBER in "${!BASELINE_KEY_LIST[@]}"; do
    if printf '%s' "${MATCHED_INDEX_LIST_STRING}" | grep -qx "${INDEX_NUMBER}"; then
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

    STALE_OBJECT_STRING="$(printf '%s' "${BASELINE_KEY_LIST[${INDEX_NUMBER}]}" | cut -f2)"
    if [[ "false" = "${DISCOVERED_BOOLEAN}" ]]; then
        println "stale baseline row: ${BASELINE_MODULE_LIST[${INDEX_NUMBER}]} is no longer a published module in this tree — the row names something the run cannot measure, so it has to go"
        continue
    fi

    println "stale baseline row: ${BASELINE_MODULE_LIST[${INDEX_NUMBER}]} ${STALE_OBJECT_STRING} no longer differs from the last tag — the change it files is gone, or a release moved the tag past it, so the row has to go"
done

if [[ "true" = "${FAILED_BOOLEAN}" ]]; then
    fail "the differences from the last tags and the baseline disagree — file each new difference with its class and a disposition, re-decide every row whose class or change moved, and delete every stale row"
fi

# the green report reads by class and disposition, per module, with the tag beside the module: the
# undecided count is the work left before a tag, and a row on a frozen major is a finding about the freeze.
print_rows_of() {
    local CLASS_STRING="${1:?}"
    local DISPOSITION_STRING="${2:?}"
    local HEADING_STRING="${3:?}"

    local ROW_LIST_STRING
    ROW_LIST_STRING="$(printf '%s' "${DECIDED_ROW_LIST_STRING}" | awk -F'\t' -v class="${CLASS_STRING}" -v disposition="${DISPOSITION_STRING}" '$2 == class && $3 == disposition { print $1 "\t" $4 "\t" $5 }')"
    if [[ "" = "${ROW_LIST_STRING}" ]]; then
        return 0
    fi

    println ""
    println "${HEADING_STRING} — $(printf '%s\n' "${ROW_LIST_STRING}" | grep -c .) row(s)"
    printf '%s\n' "${ROW_LIST_STRING}" | cut -f1 | sort | uniq -c | awk '{ print $2 "\t" $1 }' |
        while IFS=$'\t' read -r MODULE_NAME_STRING MODULE_COUNT_STRING; do
            println "    ${MODULE_NAME_STRING} ($(get_old_spec_of_module "${MODULE_NAME_STRING}")): ${MODULE_COUNT_STRING}"
        done
}

print_rows_of "incompatible" "unclassified" "INCOMPATIBLE, UNDECIDED — each of these needs a keep or a cut before the next tag"
print_rows_of "incompatible" "cut" "incompatible, decided to leave this major before the tag"
print_rows_of "incompatible" "keep" "incompatible, decided to ship on this major as a documented break"
print_rows_of "compatible" "unclassified" "compatible additions, undecided"
print_rows_of "compatible" "cut" "compatible additions, decided to leave this major"
print_rows_of "compatible" "keep" "compatible additions, decided to ship"

UNDECIDED_COUNT_NUMBER="$(printf '%s' "${DECIDED_ROW_LIST_STRING}" | awk -F'\t' '$3 == "unclassified"' | grep -c . || true)"
println ""
success "every difference between a published module and its last tag is filed with its class and a disposition, and every baseline row still occurs — ${UNDECIDED_COUNT_NUMBER} of ${MEASURED_COUNT_NUMBER} still undecided"
