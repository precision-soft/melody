#!/usr/bin/env bash

# Checks the per-major package documents against the code of every major.
#
#   .dev/validate/documentation.sh
#
# The three majors are deliberate near-copies, and their documents drifted independently: a symbol
# documented for one major stayed undocumented for another that had carried it all along, because
# nothing compared them. Reading the documents against each other is not enough either — most of the
# divergence between them is legitimate, since v3 has whole packages the others do not.
#
# So the check is narrower than "the documents match": for every exported symbol any major's document
# lists, every OTHER major whose code declares that symbol must document it too. A symbol that only one
# major implements is documented only there and never reported, and a difference in prose is never
# reported at all. What is reported is exactly the case where a reader of one major cannot find an API
# that major ships.
#
# Needs no container: it reads the tree.

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

MAJOR_DIRECTORY_STRING_LIST=(
    "."
    "v2"
    "v3"
)

# the symbol a list entry names, with a receiver or package qualifier, the argument list and any type
# parameter stripped. All three entry shapes the documents use are read — ``- `pkg.Ident(...)` ``,
# ``- [`(*Type).Ident(...)`](link)`` and `- **pkg.Ident**` — because the type lists are written in bold
# while the function lists are written in backticks, and reading only one shape silently empties half
# the inventory: a check that never learns a symbol exists can never report it missing.
#
# Anchoring to the whole token matters as much: matching a `New`/`Must` prefix anywhere in the line
# instead picks `MustFromResolver` out of `LoggerMustFromResolver` and then reports the generic
# container helper as missing from every document that mentions a logger.
extract_documented_symbol_list() {
    local DOCUMENT_PATH_STRING="${1:?}"

    grep -ohP '^- (\[`|`|\*\*)(\(\*?[A-Za-z][A-Za-z0-9_]*\)\.|[a-z][a-z0-9]*\.)?\K[A-Z][A-Za-z0-9_]*' \
        "${DOCUMENT_PATH_STRING}" 2>/dev/null || true
}

# every symbol this major lists in any of its package documents. Membership is tested against this
# inventory rather than against a plain text search of the one document: a bare `grep` for a short name
# such as `Item` matches the `*cache.Item` inside a constructor's return type, so deleting the entry
# that documents the type would leave the check green — which is exactly what it did before this was
# an inventory. Spanning every document of the major is deliberate: a symbol whose home is another
# package's document is documented, just not here.
list_major_documented_symbol_inventory() {
    local MAJOR_DIRECTORY_STRING="${1:?}"

    local DOCUMENT_PATH_STRING
    for DOCUMENT_PATH_STRING in "${MAJOR_DIRECTORY_STRING}"/.documentation/package/*.md; do
        if [[ -f "${DOCUMENT_PATH_STRING}" ]]; then
            extract_documented_symbol_list "${DOCUMENT_PATH_STRING}"
        fi
    done | sort -u
}

# the file in this major's own tree that declares the symbol at top level, if any. The majors are
# disjoint trees, so v1 is everything that is not under another major, an integration, or the harness.
find_declaring_file() {
    local MAJOR_DIRECTORY_STRING="${1:?}"
    local SYMBOL_STRING="${2:?}"

    local SEARCH_ROOT_STRING="${MAJOR_DIRECTORY_STRING}"
    if [[ "." = "${MAJOR_DIRECTORY_STRING}" ]]; then
        SEARCH_ROOT_STRING="."
    fi

    find "${SEARCH_ROOT_STRING}" -maxdepth 2 -name "*.go" ! -name "*_test.go" 2>/dev/null |
        grep -vE "^\./(v[0-9]+|integrations|\.dev|\.example)/" |
        { xargs grep -lE "^(func|type|const|var) ${SYMBOL_STRING}\b" 2>/dev/null || true; } |
        head -1
}

GAP_COUNT_INTEGER=0
CHECKED_COUNT_INTEGER=0

declare -A MAJOR_INVENTORY_STRING_MAP=()
for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
    MAJOR_INVENTORY_STRING_MAP["${MAJOR_DIRECTORY_STRING}"]=$'\n'"$(list_major_documented_symbol_inventory "${MAJOR_DIRECTORY_STRING}")"$'\n'
done

DOCUMENT_NAME_STRING_LIST=()
while IFS= read -r DOCUMENT_PATH_STRING; do
    DOCUMENT_NAME_STRING_LIST+=("$(basename "${DOCUMENT_PATH_STRING}")")
done < <(find .documentation/package -name "*.md" | sort)

for DOCUMENT_NAME_STRING in "${DOCUMENT_NAME_STRING_LIST[@]}"; do
    SYMBOL_UNION_STRING="$(
        for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
            DOCUMENT_PATH_STRING="${MAJOR_DIRECTORY_STRING}/.documentation/package/${DOCUMENT_NAME_STRING}"
            if [[ -f "${DOCUMENT_PATH_STRING}" ]]; then
                extract_documented_symbol_list "${DOCUMENT_PATH_STRING}"
            fi
        done | sort -u
    )"

    for SYMBOL_STRING in ${SYMBOL_UNION_STRING}; do
        for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
            DOCUMENT_PATH_STRING="${MAJOR_DIRECTORY_STRING}/.documentation/package/${DOCUMENT_NAME_STRING}"
            if [[ ! -f "${DOCUMENT_PATH_STRING}" ]]; then
                continue
            fi

            CHECKED_COUNT_INTEGER=$((CHECKED_COUNT_INTEGER + 1))

            if [[ "${MAJOR_INVENTORY_STRING_MAP[${MAJOR_DIRECTORY_STRING}]}" == *$'\n'"${SYMBOL_STRING}"$'\n'* ]]; then
                continue
            fi

            DECLARING_FILE_STRING="$(find_declaring_file "${MAJOR_DIRECTORY_STRING}" "${SYMBOL_STRING}")"
            if [[ "" = "${DECLARING_FILE_STRING}" ]]; then
                continue
            fi

            println "  gap  ${DOCUMENT_NAME_STRING}: ${MAJOR_DIRECTORY_STRING} declares ${SYMBOL_STRING} in ${DECLARING_FILE_STRING} and does not document it"
            GAP_COUNT_INTEGER=$((GAP_COUNT_INTEGER + 1))
        done
    done
done

info "checked ${CHECKED_COUNT_INTEGER} symbol/major pairs across ${#DOCUMENT_NAME_STRING_LIST[@]} package documents"

if [[ 0 -lt ${GAP_COUNT_INTEGER} ]]; then
    fail "${GAP_COUNT_INTEGER} documentation gap(s): a major ships a symbol another major documents, and does not document it"
fi

success "package documentation agrees with the code of every major"
