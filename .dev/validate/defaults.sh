#!/usr/bin/env bash
# Checks v4/.documentation/DEFAULTS.md, the inventory of the fourth major's defaults, against the code.
#
#   .dev/validate/defaults.sh [--draft]
#
# The tool under .dev/validate/typecheck/defaults reads every v4-line module, the framework and every
# integrations/**/v4, through go/packages and go/types, and enumerates what no reader can be trusted to keep
# whole by hand: every bound.Rule and bound.BackoffRule literal, every boot-warning code, every fs.FileMode
# constant and constant mode handed to the os file calls, every declared env key, and every door candidate of a
# configuration struct: every bool, every field typed by an enumeration of the line's own, every *Disabled field
# and every bound limit. It then reads the document and refuses:
#
#   - an enumerated default with no row, and a row whose key the code no longer declares;
#   - a row without its door, a `none` door without a reason, a limit refusing unlimited without a reason or
#     without a value: door, a clamping limit whose reason is a bare class reference;
#   - an anchor, a key or a door, that names no declaration, and an env: door naming no declared key;
#   - an env: door on a protection row whose reason does not say the key hardens it: a protection is turned
#     off in Go only;
#   - a door candidate no row names and no line of .dev/validate/defaults.not-doors writes off with its reason,
#     and a not-doors line naming no candidate or a field a row names;
#   - a protects-against cell still a draft, a limit whose default cell states no figure, and a value: door
#     naming a function that takes nothing, which answers a value and sets none;
#   - a repeated key and a malformed row.
#
# Every failure is printed `kind ~ key ~ message`; a line of .dev/validate/defaults.baseline with the same
# kind and key holds it, and a line that holds nothing fails the band. --draft prints the enumerated items and
# door fields no row names, with their bounds and a suggested door, for the hand that writes the rows.
#
# The tool loads under the repository's workspace, as the interface lister does, so an integration ahead of
# its pins still type-checks; a module that does not type-check fails the run. Needs the development
# container, where the toolchain lives.

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

DRAFT_ARGUMENT_STRING=""
if [[ "--draft" = "${1-}" ]]; then
    DRAFT_ARGUMENT_STRING="-draft"
elif [[ "" != "${1-}" ]]; then
    fail "unknown flag: ${1} (the band takes --draft or nothing)"
fi

SERVICE_NAME_STRING="dev"

require_docker
require_docker_daemon
ensure_service_running "${SERVICE_NAME_STRING}"

if [[ ! -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/v4/.documentation/DEFAULTS.md" ]]; then
    fail "the inventory v4/.documentation/DEFAULTS.md does not exist — the band checks the code against it"
fi
if [[ ! -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/defaults.not-doors" ]]; then
    fail "the not-doors file .dev/validate/defaults.not-doors does not exist — it writes off, with a reason, each door candidate that turns nothing off"
fi
if [[ ! -f "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/validate/defaults.baseline" ]]; then
    fail "the baseline .dev/validate/defaults.baseline does not exist — an empty file is the baseline of a tree with nothing held"
fi

MODULE_LIST=("v4")
while IFS= read -r GO_MOD_PATH_STRING; do
    if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
        continue
    fi
    MODULE_LIST+=("$(dirname "${GO_MOD_PATH_STRING}")")
done < <(find integrations -path '*/.example' -prune -o -path '*/v4/go.mod' -print | sort)

if [[ 2 -gt "${#MODULE_LIST[@]}" ]]; then
    fail "no integration v4 module was found — a band over the framework alone would vouch for modules it never read"
fi

info "defaults inventory: ${#MODULE_LIST[@]} v4-line module(s)"

EXIT_CODE_INTEGER="0"
docker_compose_no_log exec -T "${SERVICE_NAME_STRING}" sh -s -- "${DRAFT_ARGUMENT_STRING}" "${MODULE_LIST[@]}" <<'CONTAINER_SCRIPT' || EXIT_CODE_INTEGER=$?
set -u
DRAFT_ARGUMENT="${1}"
shift
TOOL_BINARY="/tmp/melody-defaults"
BUILD_OUTPUT="$(cd /app/.dev/validate/typecheck && GOWORK=off go build -o "${TOOL_BINARY}" ./defaults 2>&1)"
if [ 0 -ne $? ]; then
    printf '%s\n' "${BUILD_OUTPUT}" >&2
    printf 'the defaults reader under .dev/validate/typecheck/defaults did not build\n' >&2
    exit 2
fi
cd /app
if [ -n "${DRAFT_ARGUMENT}" ]; then
    exec "${TOOL_BINARY}" "${DRAFT_ARGUMENT}" "$@"
fi
exec "${TOOL_BINARY}" "$@"
CONTAINER_SCRIPT

if [[ -n "${DRAFT_ARGUMENT_STRING}" ]]; then
    exit "${EXIT_CODE_INTEGER}"
fi

if [[ 0 -ne "${EXIT_CODE_INTEGER}" ]]; then
    fail "v4/.documentation/DEFAULTS.md and the code disagree (above) — write the row, repair the anchor, or hold the failure in .dev/validate/defaults.baseline with its reason"
fi

success "defaults inventory: every default has its row, every row its anchor"
