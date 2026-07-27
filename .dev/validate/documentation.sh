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
# Two halves have to be right for that to mean anything, and both were silently wrong before:
#
# The reading half must see every entry the documents actually write. They use `-` and `*` bullets
# interchangeably, and one whole document is written entirely in `*` — anchoring on `-` alone dropped it
# in full while the pair count stayed the same, so the check reported a healthy number having read
# nothing of it.
#
# The declaring half must see every declaration the code actually makes. Searching the tree per lookup
# meant a depth limit, which hid every subpackage; a top-level-only pattern, which never matched a
# method; and no notion of which package the entry belongs to, which resolved a name to whichever file
# happened to match first. It is now one index built per major in a single pass — functions, types,
# constants and variables, the members of grouped const/var blocks, and methods — and an entry that
# links to a file is looked up only inside the package that file lives in, so a name that two packages
# share cannot answer for the other.
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

# the symbol a list entry names, and the package it says the symbol lives in, separated by a tab.
#
# All three entry shapes the documents use are read — ``- `pkg.Ident(...)` ``, ``- [`(*Type).Ident(...)`](link)``
# and `- **pkg.Ident**` — under either bullet character, because the type lists are written in bold while
# the function lists are written in backticks, and one document is written entirely in `*`. Reading only
# one shape or only one bullet silently empties part of the inventory: a check that never learns a symbol
# exists can never report it missing.
#
# Anchoring to the whole token matters as much: matching a `New`/`Must` prefix anywhere in the line
# instead picks `MustFromResolver` out of `LoggerMustFromResolver` and then reports the generic container
# helper as missing from every document that mentions a logger.
#
# The package comes from the entry's own link, which every document writes relative to itself as
# `../../<major-relative path>`, so it needs no guessing. An entry without one yields an empty package and
# is looked up across the whole major, the way every entry used to be.
extract_documented_entry_list() {
    local DOCUMENT_PATH_STRING="${1:?}"

    awk '
        function symbolOf(token,   candidate) {
            candidate = token
            sub(/^\(\*?[A-Za-z][A-Za-z0-9_]*\)\./, "", candidate)
            sub(/^[a-z][a-z0-9]*\./, "", candidate)

            if (0 == match(candidate, /^[A-Z][A-Za-z0-9_]*/)) {
                return ""
            }

            return substr(candidate, 1, RLENGTH)
        }

        /^[-*] / {
            packageDirectory = ""
            if (0 != match($0, /\]\(\.\.\/\.\.\/[^)]+\)/)) {
                target = substr($0, RSTART + 8, RLENGTH - 9)
                if (target ~ /\.go$/) {
                    if (0 != match(target, /^.*\//)) {
                        packageDirectory = substr(target, 1, RLENGTH - 1)
                    }
                } else {
                    packageDirectory = target
                }
            }

            entry = $0
            sub(/^[-*] /, "", entry)

            # a linked entry names every symbol it covers inside the link text, and several list a whole
            # family on one line — the env key constants, the kernel parameter names. Reading only the first
            # leaves the rest outside the inventory, which is not merely incomplete: the missing ones are
            # then reported as undocumented for whichever major happens to list one of them first.
            if (1 == match(entry, /^\[/)) {
                linkTextEnd = index(entry, "](")
                if (0 == linkTextEnd) {
                    next
                }

                linkText = substr(entry, 2, linkTextEnd - 2)

                while (0 != match(linkText, /`[^`]+`/)) {
                    token = substr(linkText, RSTART + 1, RLENGTH - 2)
                    linkText = substr(linkText, RSTART + RLENGTH)

                    symbol = symbolOf(token)
                    if ("" != symbol) {
                        print symbol "\t" packageDirectory
                    }
                }

                next
            }

            if (1 == match(entry, /^`/)) {
                entry = substr(entry, 2)
            } else if (1 == match(entry, /^\*\*/)) {
                entry = substr(entry, 3)
            } else {
                next
            }

            symbol = symbolOf(entry)
            if ("" != symbol) {
                print symbol "\t" packageDirectory
            }
        }
    ' "${DOCUMENT_PATH_STRING}" 2>/dev/null || true
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
            extract_documented_entry_list "${DOCUMENT_PATH_STRING}" | cut -f1
        fi
    done | sort -u
}

# the source files that belong to this major and to nothing else. The majors are disjoint trees, so v1 is
# everything that is not under another major, an integration, or a dot directory — and that last exclusion
# is load-bearing rather than tidy: the repository carries a module cache under `.dev-data`, which holds
# released copies of melody itself, so a walk that descends into it finds every symbol declared in every
# major at once and can answer for a package that no longer exists.
list_major_source_file() {
    local MAJOR_DIRECTORY_STRING="${1:?}"

    if [[ "." = "${MAJOR_DIRECTORY_STRING}" ]]; then
        find . -type f -name "*.go" ! -name "*_test.go" \
            ! -path "./.*/*" ! -path "./v[0-9]*/*" ! -path "./integrations/*"

        return 0
    fi

    find "${MAJOR_DIRECTORY_STRING}" -type f -name "*.go" ! -name "*_test.go" ! -path "*/.*/*"
}

# every top-level declaration this major makes, as `symbol<tab>file`, in one pass over its sources.
#
# The member of a grouped `const (` / `var (` block is a declaration the eye reads as one and the older
# line-anchored pattern never saw; tracking the block is what tells such a member apart from a struct
# field, which is written at the same indent but only ever inside a `type` block the tracker is not in.
# Methods are indexed under their own name because that is how the documents list them, and a method is
# the single largest thing the previous pattern could not match at all.
build_major_declaration_index() {
    local MAJOR_DIRECTORY_STRING="${1:?}"

    list_major_source_file "${MAJOR_DIRECTORY_STRING}" | xargs awk '
        FNR == 1 { insideBlock = 0 }

        /^(const|var) \($/ { insideBlock = 1; next }

        1 == insideBlock && /^\)/ { insideBlock = 0; next }

        1 == insideBlock && 0 != match($0, /^    [A-Z][A-Za-z0-9_]*/) {
            print substr($0, 5, RLENGTH - 4) "\t" FILENAME
            next
        }

        0 != match($0, /^func \([a-zA-Z_][A-Za-z0-9_]* \*?[A-Za-z_][A-Za-z0-9_]*\) [A-Z][A-Za-z0-9_]*/) {
            declaration = substr($0, RSTART, RLENGTH)
            sub(/^func \([^)]*\) /, "", declaration)
            print declaration "\t" FILENAME
            next
        }

        0 != match($0, /^(func|type|const|var) [A-Z][A-Za-z0-9_]*/) {
            declaration = substr($0, RSTART, RLENGTH)
            sub(/^(func|type|const|var) /, "", declaration)
            print declaration "\t" FILENAME
            next
        }
    ' | sort -u
}

declare -A MAJOR_DECLARATION_STRING_MAP=()

load_major_declaration_index() {
    local MAJOR_DIRECTORY_STRING="${1:?}"

    local INDEX_LINE_STRING
    while IFS= read -r INDEX_LINE_STRING; do
        local INDEX_SYMBOL_STRING="${INDEX_LINE_STRING%%$'\t'*}"
        local INDEX_FILE_STRING="${INDEX_LINE_STRING#*$'\t'}"
        local INDEX_KEY_STRING="${MAJOR_DIRECTORY_STRING}"$'\t'"${INDEX_SYMBOL_STRING}"

        MAJOR_DECLARATION_STRING_MAP["${INDEX_KEY_STRING}"]="${MAJOR_DECLARATION_STRING_MAP[${INDEX_KEY_STRING}]:-}${INDEX_FILE_STRING}"$'\n'
    done < <(build_major_declaration_index "${MAJOR_DIRECTORY_STRING}")
}

# the file in this major that declares the symbol, if any, preferring one inside the package the entry
# links to. The scope is what keeps a shared name honest: `New` and `Warning` and `Item` are declared in
# more than one package, so a lookup that accepts any file at all reports the wrong major's document as
# incomplete and names a file the reader will not find the symbol described in.
find_declaring_file() {
    local MAJOR_DIRECTORY_STRING="${1:?}"
    local SYMBOL_STRING="${2:?}"
    local PACKAGE_DIRECTORY_STRING="${3:-}"

    local DECLARATION_STRING="${MAJOR_DECLARATION_STRING_MAP[${MAJOR_DIRECTORY_STRING}$'\t'${SYMBOL_STRING}]:-}"
    if [[ "" = "${DECLARATION_STRING}" ]]; then
        return 0
    fi

    if [[ "" = "${PACKAGE_DIRECTORY_STRING}" ]]; then
        printf '%s\n' "${DECLARATION_STRING}" | head -1

        return 0
    fi

    local PACKAGE_PREFIX_STRING="${MAJOR_DIRECTORY_STRING}/${PACKAGE_DIRECTORY_STRING}/"

    printf '%s\n' "${DECLARATION_STRING}" | grep -F "${PACKAGE_PREFIX_STRING}" | head -1 || true
}

GAP_COUNT_INTEGER=0
CHECKED_COUNT_INTEGER=0

declare -A MAJOR_INVENTORY_STRING_MAP=()
for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
    MAJOR_INVENTORY_STRING_MAP["${MAJOR_DIRECTORY_STRING}"]=$'\n'"$(list_major_documented_symbol_inventory "${MAJOR_DIRECTORY_STRING}")"$'\n'
    load_major_declaration_index "${MAJOR_DIRECTORY_STRING}"
done

DOCUMENT_NAME_STRING_LIST=()
while IFS= read -r DOCUMENT_PATH_STRING; do
    DOCUMENT_NAME_STRING_LIST+=("$(basename "${DOCUMENT_PATH_STRING}")")
done < <(find .documentation/package -name "*.md" | sort)

for DOCUMENT_NAME_STRING in "${DOCUMENT_NAME_STRING_LIST[@]}"; do
    ENTRY_UNION_STRING="$(
        for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
            DOCUMENT_PATH_STRING="${MAJOR_DIRECTORY_STRING}/.documentation/package/${DOCUMENT_NAME_STRING}"
            if [[ -f "${DOCUMENT_PATH_STRING}" ]]; then
                extract_documented_entry_list "${DOCUMENT_PATH_STRING}"
            fi
        done | sort -u
    )"

    while IFS=$'\t' read -r SYMBOL_STRING PACKAGE_DIRECTORY_STRING; do
        if [[ "" = "${SYMBOL_STRING}" ]]; then
            continue
        fi

        for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
            DOCUMENT_PATH_STRING="${MAJOR_DIRECTORY_STRING}/.documentation/package/${DOCUMENT_NAME_STRING}"
            if [[ ! -f "${DOCUMENT_PATH_STRING}" ]]; then
                continue
            fi

            CHECKED_COUNT_INTEGER=$((CHECKED_COUNT_INTEGER + 1))

            if [[ "${MAJOR_INVENTORY_STRING_MAP[${MAJOR_DIRECTORY_STRING}]}" == *$'\n'"${SYMBOL_STRING}"$'\n'* ]]; then
                continue
            fi

            DECLARING_FILE_STRING="$(find_declaring_file "${MAJOR_DIRECTORY_STRING}" "${SYMBOL_STRING}" "${PACKAGE_DIRECTORY_STRING}")"
            if [[ "" = "${DECLARING_FILE_STRING}" ]]; then
                continue
            fi

            println "  gap  ${DOCUMENT_NAME_STRING}: ${MAJOR_DIRECTORY_STRING} declares ${SYMBOL_STRING} in ${DECLARING_FILE_STRING} and does not document it"
            GAP_COUNT_INTEGER=$((GAP_COUNT_INTEGER + 1))
        done
    done <<< "${ENTRY_UNION_STRING}"
done

info "checked ${CHECKED_COUNT_INTEGER} symbol/major pairs across ${#DOCUMENT_NAME_STRING_LIST[@]} package documents"

if [[ 0 -lt ${GAP_COUNT_INTEGER} ]]; then
    fail "${GAP_COUNT_INTEGER} documentation gap(s): a major ships a symbol another major documents, and does not document it"
fi

success "package documentation agrees with the code of every major"
