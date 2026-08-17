#!/usr/bin/env bash

# Checks the per-major package documents against the code of every major.
#
#   .dev/validate/documentation.sh
#   .dev/validate/documentation.sh --undocumented [<document or package filter>]
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
# The check above can only ever compare one major against another, so a door no major documents is
# invisible to it by construction, and so is the reverse question the citation band asks — that one reads
# what the documents cite and asks whether the code has it. Neither can say what the code has and no
# document names. `--undocumented` asks exactly that, and reports rather than gates: which exported symbol
# a major declares is enrolled in no entry list of any major.
#
# It cannot be a gate, and the shape of the number says why. Of some 1270 exported symbols a published
# major declares, around 900 are enrolled nowhere, and most of that is correct — the documents list types,
# constructors and the doors an integrator comes through, not every method of every type. A band that
# demanded an entry for each of them would need its exceptions counted in the hundreds, which is the same
# reason the citation band refuses to assert a bare backticked name and the parity baseline refuses a
# wildcard: a dimension whose exception list grows without bound is a suppression list wearing a check's
# clothes.
#
# What makes the report usable instead of a dump is that each row carries the evidence that is available
# without judgement, and nothing beyond it:
#
#   - the row is grouped under the document that covers the package, taken from where that document's own
#     entries link, so a symbol in a package no document covers is counted and not listed;
#   - `mentioned` means the covering document already writes the symbol as a code token and still has no
#     entry for it — the document is talking about a door it never lists. `absent` means the document does
#     not name it at all.
#
# Neither flag is a verdict. Ranking is all a tool can honestly do here; whether a door deserves an entry
# is read at the site.
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

UNDOCUMENTED_MODE_INTEGER=0
FILTER_STRING=""

for ARGUMENT_STRING in "$@"; do
    case "${ARGUMENT_STRING}" in
        --undocumented)
            UNDOCUMENTED_MODE_INTEGER=1
            ;;
        --*)
            fail "unknown option ${ARGUMENT_STRING}"
            ;;
        *)
            FILTER_STRING="${ARGUMENT_STRING}"
            ;;
    esac
done

if [[ "" != "${FILTER_STRING}" && 0 -eq ${UNDOCUMENTED_MODE_INTEGER} ]]; then
    fail "a filter is only read by --undocumented"
fi

# the symbol a list entry names, and the package it says the symbol lives in, separated by a tab.
#
# All three entry shapes the documents use are read — ``- `pkg.Ident(...)` ``, ``- [`(*Type).Ident(...)`](link)``
# and `- **pkg.Ident**` — under either bullet character, because the type lists are written in bold while
# the function lists are written in backticks, and one document is written entirely in `*`. Reading only
# one shape or only one bullet silently empties part of the inventory: a check that never learns a symbol
# exists can never report it missing.
#
# Two more shapes are read for the same reason, and both were found the way the first three were — by a
# symbol that could not possibly be missing reading as missing.
#
# The type lists group their entries under a label, ``- Tokens: [`AnonymousToken`](…), …``, and the
# responsibility lists name a door mid-sentence, `- route configuration via [`RouteOptions`](…)`. Neither
# begins with a symbol, so anchoring on the head of the bullet dropped both in full: `PathPrefixMatcher`
# and `AnonymousToken` are listed in plain sight and were enrolled nowhere. So every linked token in the
# bullet is read, not only the one the bullet opens with, and the head shapes are still read on top of
# that — widening both sides at once, since the same inventory decides what is demanded and what satisfies
# the demand.
#
# And a bullet is a bullet at any indent. The configuration sections nest their entries one level under a
# heading bullet — `- Builder:` over ten indented entries — which is 321 entries on a published major, the
# whole `security/config` surface among them, read by an anchor pinned to the first column as nothing at
# all. That one reported thirteen doors as undocumented that the document lists in plain sight, which is
# the failure this check exists to avoid, pointed the wrong way.
#
# Anchoring to the whole token matters as much: matching a `New`/`Must` prefix anywhere in the line
# instead picks `MustFromResolver` out of `LoggerMustFromResolver` and then reports the generic container
# helper as missing from every document that mentions a logger.
#
# The package comes from the entry's own link, which every document writes relative to itself as
# `../../<major-relative path>`, so it needs no guessing, and each link is read with its own target rather
# than with the first one on the line — a bullet naming three doors in three packages would otherwise look
# all three up inside the first. An entry without a link yields an empty package and is looked up across
# the whole major, the way every entry used to be.
#
# Only a target spelled `../../` is treated as a link. A looser pattern reads the `](` inside a backticked
# call — `MustFromResolver[T](resolver, Service)` — as one, and then looks the symbol up in a package named
# after the argument list.
extract_documented_entry_list() {
    local DOCUMENT_PATH_STRING="${1:?}"

    awk '
        function symbolOf(token,   candidate) {
            candidate = token

            # the declaration keyword is part of how the type lists are written — ``[`type Error`](…)``,
            # ``[`const ServiceRouter`](…)`` — and it starts with a lower-case letter, so the anchor that
            # keeps `MustFromResolver` out of `LoggerMustFromResolver` threw all 385 of them away. Every
            # contract list in the tree is written this way, which is why `Error`, `AlreadyLogged` and
            # `ContextProvider` read as undocumented on the two majors that list them in plain sight.
            sub(/^(type|func|const|var) /, "", candidate)

            sub(/^\(\*?[A-Za-z][A-Za-z0-9_]*\)\./, "", candidate)
            sub(/^[a-z][a-z0-9]*\./, "", candidate)

            if (0 == match(candidate, /^[A-Z][A-Za-z0-9_]*/)) {
                return ""
            }

            return substr(candidate, 1, RLENGTH)
        }

        function packageOf(target,   directory) {
            if (target !~ /\.go$/) {
                return target
            }

            if (0 == match(target, /^.*\//)) {
                return ""
            }

            directory = substr(target, 1, RLENGTH - 1)

            return directory
        }

        /^[ \t]*[-*] / {
            entry = $0
            sub(/^[ \t]*[-*] /, "", entry)

            headPackageDirectory = ""
            rest = entry

            # a linked entry names every symbol it covers inside the link text, and several list a whole
            # family on one line — the env key constants, the kernel parameter names. Reading only the first
            # leaves the rest outside the inventory, which is not merely incomplete: the missing ones are
            # then reported as undocumented for whichever major happens to list one of them first.
            while (0 != match(rest, /\]\(\.\.\/\.\.\/[^)]*\)/)) {
                closeStart = RSTART
                closeLength = RLENGTH
                head = substr(rest, 1, closeStart - 1)
                target = substr(rest, closeStart + 8, closeLength - 9)
                rest = substr(rest, closeStart + closeLength)

                # the link opens at the bracket that matches the one `](` closes, found by walking left at
                # bracket depth rather than by taking the nearest `[`. Half the constructor entries carry a
                # Go signature inside the link text, and the nearest bracket in
                # ``[`NewRoleHierarchy(inheritedRolesByRole map[string][]string)`](…)`` is the `[` of
                # `[]string` — which cuts the text after the symbol and drops the entry entirely.
                openAt = 0
                bracketDepthInteger = 0
                for (position = length(head); 1 <= position; position--) {
                    character = substr(head, position, 1)

                    if ("]" == character) {
                        bracketDepthInteger = bracketDepthInteger + 1
                    } else if ("[" == character) {
                        if (0 == bracketDepthInteger) {
                            openAt = position

                            break
                        }

                        bracketDepthInteger = bracketDepthInteger - 1
                    }
                }

                packageDirectory = packageOf(target)
                if ("" == headPackageDirectory) {
                    headPackageDirectory = packageDirectory
                }

                if (0 == openAt) {
                    continue
                }

                linkText = substr(head, openAt + 1)

                while (0 != match(linkText, /`[^`]+`/)) {
                    tokenStart = RSTART
                    tokenLength = RLENGTH
                    token = substr(linkText, tokenStart + 1, tokenLength - 2)
                    linkText = substr(linkText, tokenStart + tokenLength)

                    symbol = symbolOf(token)
                    if ("" != symbol) {
                        print symbol "\t" packageDirectory
                    }
                }
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
                print symbol "\t" headPackageDirectory
            }

            # an entry that opens with a symbol may carry its siblings on the same line, unlinked —
            # `- `SetBaseUrl`, `SetHeader`, `SetTimeout`` is one entry naming three methods, and reading the
            # head alone left two of them demanded of every other major and enrolled by none. The rest of
            # the bullet is scanned only because the head established it as an entry: the same scan over a
            # prose bullet would enrol whatever a sentence happens to quote, which is how a bare text search
            # made the check green over a deleted entry before it was an inventory.
            rest = entry

            while (0 != match(rest, /`[^`]+`/)) {
                tokenStart = RSTART
                tokenLength = RLENGTH
                token = substr(rest, tokenStart + 1, tokenLength - 2)
                rest = substr(rest, tokenStart + tokenLength)

                symbol = symbolOf(token)
                if ("" != symbol) {
                    print symbol "\t" headPackageDirectory
                }
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

# how many times a document links into each package directory, as `count<tab>package`. Both link spellings
# are read: a `.go` target names the package by its directory, a bare directory target names it outright.
list_document_linked_package() {
    local DOCUMENT_PATH_STRING="${1:?}"

    awk '
        {
            rest = $0

            while (0 != match(rest, /\]\(\.\.\/\.\.\/[^)#]*\)/)) {
                closeStart = RSTART
                closeLength = RLENGTH
                target = substr(rest, closeStart + 8, closeLength - 9)
                rest = substr(rest, closeStart + closeLength)

                sub(/\/$/, "", target)

                if (target ~ /\.go$/) {
                    if (0 == match(target, /^.*\//)) {
                        continue
                    }

                    target = substr(target, 1, RLENGTH - 1)
                } else if (target ~ /\.md$/) {
                    continue
                }

                if ("" != target) {
                    linkCount[target] = linkCount[target] + 1
                }
            }
        }

        END {
            for (target in linkCount) {
                print linkCount[target] "\t" target
            }
        }
    ' "${DOCUMENT_PATH_STRING}" 2>/dev/null | sort -k2
}

# every symbol a document writes as a code token, reduced the way an entry token is reduced so the two are
# read in the same spelling. A qualified `security.RolesReplacer` and a method `(*Manager).Get` both come
# back as the bare name the inventory carries.
#
# The scan position is read into locals before anything else can call match: `sub` inside the loop would
# otherwise move RSTART out from under the line that has to be advanced by it, and a token with nothing to
# substitute leaves the position on itself, so the line never shortens and the loop never ends.
list_document_mentioned_symbol() {
    local DOCUMENT_PATH_STRING="${1:?}"

    awk '
        {
            line = $0

            while (0 != match(line, /`[^`]+`/)) {
                token = substr(line, RSTART + 1, RLENGTH - 2)
                line = substr(line, RSTART + RLENGTH)

                sub(/^\(\*?[A-Za-z][A-Za-z0-9_]*\)\./, "", token)
                sub(/^[a-z][a-z0-9]*\./, "", token)

                if (0 != match(token, /^[A-Z][A-Za-z0-9_]*/)) {
                    print substr(token, 1, RLENGTH)
                }
            }
        }
    ' "${DOCUMENT_PATH_STRING}" 2>/dev/null | sort -u
}

BASELINE_PATH_STRING=".dev/validate/documentation.baseline"

if [[ ! -f "${BASELINE_PATH_STRING}" ]]; then
    fail "the baseline is missing: ${BASELINE_PATH_STRING}. Without it every recorded divergence reads as new, so restore it rather than let the run invent a verdict"
fi

trim_field() {
    local VALUE_STRING="${1-}"

    VALUE_STRING="${VALUE_STRING#"${VALUE_STRING%%[![:space:]]*}"}"
    VALUE_STRING="${VALUE_STRING%"${VALUE_STRING##*[![:space:]]}"}"

    printf '%s' "${VALUE_STRING}"
}

declare -A BASELINE_LINE_INTEGER_MAP=()
declare -A BASELINE_REASON_STRING_MAP=()
declare -A BASELINE_CONSUMED_INTEGER_MAP=()

BASELINE_LINE_NUMBER_INTEGER=0
BROKEN_COUNT_INTEGER=0

while IFS= read -r BASELINE_LINE_STRING || [[ "" != "${BASELINE_LINE_STRING}" ]]; do
    BASELINE_LINE_NUMBER_INTEGER=$((BASELINE_LINE_NUMBER_INTEGER + 1))

    if [[ "" = "${BASELINE_LINE_STRING// /}" ]]; then
        continue
    fi

    case "${BASELINE_LINE_STRING}" in \#*) continue ;; esac

    IFS='~' read -r ROW_MAJOR_STRING ROW_DOCUMENT_STRING ROW_SYMBOL_STRING ROW_REASON_STRING <<< "${BASELINE_LINE_STRING}"

    ROW_MAJOR_STRING="$(trim_field "${ROW_MAJOR_STRING}")"
    ROW_DOCUMENT_STRING="$(trim_field "${ROW_DOCUMENT_STRING}")"
    ROW_SYMBOL_STRING="$(trim_field "${ROW_SYMBOL_STRING}")"
    ROW_REASON_STRING="$(trim_field "${ROW_REASON_STRING}")"

    if [[ "" = "${ROW_MAJOR_STRING}" || "" = "${ROW_DOCUMENT_STRING}" || "" = "${ROW_SYMBOL_STRING}" || "" = "${ROW_REASON_STRING}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} does not split into major ~ document ~ symbol ~ reason"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))

        continue
    fi

    if [[ "${ROW_SYMBOL_STRING}" == *"*"* || "${ROW_DOCUMENT_STRING}" == *"*"* ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} carries a wildcard, which would silently grow to cover the next gap: write one line per fact"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))

        continue
    fi

    BASELINE_KEY_STRING="${ROW_MAJOR_STRING}"$'\t'"${ROW_DOCUMENT_STRING}"$'\t'"${ROW_SYMBOL_STRING}"

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]:-}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} repeats the entry first written at line ${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]}"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))

        continue
    fi

    BASELINE_LINE_INTEGER_MAP["${BASELINE_KEY_STRING}"]="${BASELINE_LINE_NUMBER_INTEGER}"
    BASELINE_REASON_STRING_MAP["${BASELINE_KEY_STRING}"]="${ROW_REASON_STRING}"
    BASELINE_CONSUMED_INTEGER_MAP["${BASELINE_KEY_STRING}"]=0
done < "${BASELINE_PATH_STRING}"

GAP_COUNT_INTEGER=0
CHECKED_COUNT_INTEGER=0
UNRECORDED_COUNT_INTEGER=0
STALE_COUNT_INTEGER=0

declare -A FINDING_SEEN_INTEGER_MAP=()
declare -A MAJOR_INVENTORY_STRING_MAP=()
for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
    MAJOR_INVENTORY_STRING_MAP["${MAJOR_DIRECTORY_STRING}"]=$'\n'"$(list_major_documented_symbol_inventory "${MAJOR_DIRECTORY_STRING}")"$'\n'
    load_major_declaration_index "${MAJOR_DIRECTORY_STRING}"
done

if [[ 1 -eq ${UNDOCUMENTED_MODE_INTEGER} ]]; then
    # the union is what makes the two passes partition the space rather than overlap: a symbol one major
    # documents and another ships is the gated check's finding, and it stays out of this report even while
    # it is missing here, so a run of this pass never re-reports what the gate already refuses to pass.
    UNION_INVENTORY_STRING=$'\n'"$(
        for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
            list_major_documented_symbol_inventory "${MAJOR_DIRECTORY_STRING}"
        done | sort -u
    )"$'\n'

    if [[ $'\n\n' = "${UNION_INVENTORY_STRING}" ]]; then
        fail "the documented inventory of every major is empty: the reading half is broken, and every symbol would read as undocumented"
    fi

    # which document covers which package, read from where that document links rather than from the
    # document's name: `HTTP.md` covers `http`, `http/contract`, `http/cors` and `http/static`, and no
    # naming rule says so.
    #
    # Every link is read, not only the ones that carry an entry, because a whole subpackage is otherwise
    # invisible to the report: `VALIDATION.md` reaches `validation/contract` through
    # ``[`validation/contract/`](../../validation/contract)``, whose link text is a path and yields no
    # symbol, so a coverage map built from entries alone left `ParameterizedConstraint` — the interface a
    # userland author has to implement — counted as outside every document and never listed.
    #
    # A target is kept only when the major really has that directory, so a link to a document, a heading or
    # a moved path cannot invent a package for symbols to be filed under.
    #
    # A package gets exactly one owning document, the one that links into it most, because every document
    # links into `application`, `container` and `http` while cross-referencing: filing a symbol under each
    # of them turned nine hundred rows into eight thousand and buried the one document a reader would
    # actually go to. The count decides it, ties go to the first name in order, and the winner is measured
    # rather than guessed from the document's title — `HTTP.md` owns `http/static`, and no naming rule
    # would have said so.
    declare -A PACKAGE_LINK_COUNT_MAP=()

    for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
        for DOCUMENT_PATH_STRING in "${MAJOR_DIRECTORY_STRING}"/.documentation/package/*.md; do
            if [[ ! -f "${DOCUMENT_PATH_STRING}" ]]; then
                continue
            fi

            DOCUMENT_NAME_STRING="$(basename "${DOCUMENT_PATH_STRING}")"

            while IFS=$'\t' read -r LINK_COUNT_INTEGER PACKAGE_DIRECTORY_STRING; do
                if [[ "" = "${PACKAGE_DIRECTORY_STRING}" ]]; then
                    continue
                fi

                if [[ ! -d "${MAJOR_DIRECTORY_STRING}/${PACKAGE_DIRECTORY_STRING}" ]]; then
                    continue
                fi

                PACKAGE_LINK_COUNT_MAP["${PACKAGE_DIRECTORY_STRING}"$'\t'"${DOCUMENT_NAME_STRING}"]=$((${PACKAGE_LINK_COUNT_MAP[${PACKAGE_DIRECTORY_STRING}$'\t'${DOCUMENT_NAME_STRING}]:-0} + LINK_COUNT_INTEGER))
            done < <(list_document_linked_package "${DOCUMENT_PATH_STRING}")
        done
    done

    declare -A PACKAGE_DOCUMENT_STRING_MAP=()
    for PACKAGE_DOCUMENT_KEY_STRING in "${!PACKAGE_LINK_COUNT_MAP[@]}"; do
        PACKAGE_DIRECTORY_STRING="${PACKAGE_DOCUMENT_KEY_STRING%%$'\t'*}"
        DOCUMENT_NAME_STRING="${PACKAGE_DOCUMENT_KEY_STRING#*$'\t'}"

        OWNING_DOCUMENT_STRING="${PACKAGE_DOCUMENT_STRING_MAP[${PACKAGE_DIRECTORY_STRING}]:-}"
        if [[ "" != "${OWNING_DOCUMENT_STRING}" ]]; then
            OWNING_COUNT_INTEGER="${PACKAGE_LINK_COUNT_MAP[${PACKAGE_DIRECTORY_STRING}$'\t'${OWNING_DOCUMENT_STRING}]}"
            CANDIDATE_COUNT_INTEGER="${PACKAGE_LINK_COUNT_MAP[${PACKAGE_DOCUMENT_KEY_STRING}]}"

            if [[ ${CANDIDATE_COUNT_INTEGER} -lt ${OWNING_COUNT_INTEGER} ]]; then
                continue
            fi

            if [[ ${CANDIDATE_COUNT_INTEGER} -eq ${OWNING_COUNT_INTEGER} && "${DOCUMENT_NAME_STRING}" > "${OWNING_DOCUMENT_STRING}" ]]; then
                continue
            fi
        fi

        PACKAGE_DOCUMENT_STRING_MAP["${PACKAGE_DIRECTORY_STRING}"]="${DOCUMENT_NAME_STRING}"
    done

    COVERED_PACKAGE_COUNT_INTEGER=${#PACKAGE_DOCUMENT_STRING_MAP[@]}

    if [[ 0 -eq ${COVERED_PACKAGE_COUNT_INTEGER} ]]; then
        fail "no document links to a package: every symbol would be counted outside a covered package and nothing would be listed"
    fi

    declare -A DOCUMENT_MENTION_STRING_MAP=()
    for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
        for DOCUMENT_PATH_STRING in "${MAJOR_DIRECTORY_STRING}"/.documentation/package/*.md; do
            if [[ ! -f "${DOCUMENT_PATH_STRING}" ]]; then
                continue
            fi

            DOCUMENT_MENTION_STRING_MAP["${MAJOR_DIRECTORY_STRING}"$'\t'"$(basename "${DOCUMENT_PATH_STRING}")"]=$'\n'"$(list_document_mentioned_symbol "${DOCUMENT_PATH_STRING}")"$'\n'
        done
    done

    TOTAL_LISTED_COUNT_INTEGER=0
    TOTAL_MENTIONED_COUNT_INTEGER=0
    TOTAL_OUTSIDE_COUNT_INTEGER=0

    for MAJOR_DIRECTORY_STRING in "${MAJOR_DIRECTORY_STRING_LIST[@]}"; do
        DECLARED_COUNT_INTEGER=0
        MAJOR_LISTED_COUNT_INTEGER=0
        MAJOR_OUTSIDE_COUNT_INTEGER=0

        declare -A DOCUMENT_ROW_STRING_MAP=()
        declare -A DOCUMENT_DECLARED_COUNT_MAP=()
        declare -A DOCUMENT_ENROLLED_COUNT_MAP=()

        while IFS=$'\t' read -r SYMBOL_STRING DECLARING_FILE_STRING; do
            if [[ "" = "${SYMBOL_STRING}" ]]; then
                continue
            fi

            DECLARED_COUNT_INTEGER=$((DECLARED_COUNT_INTEGER + 1))

            PACKAGE_DIRECTORY_STRING="$(dirname "${DECLARING_FILE_STRING}")"
            PACKAGE_DIRECTORY_STRING="${PACKAGE_DIRECTORY_STRING#./}"
            if [[ "." != "${MAJOR_DIRECTORY_STRING}" ]]; then
                PACKAGE_DIRECTORY_STRING="${PACKAGE_DIRECTORY_STRING#"${MAJOR_DIRECTORY_STRING}"/}"
            fi

            DOCUMENT_NAME_STRING="${PACKAGE_DOCUMENT_STRING_MAP[${PACKAGE_DIRECTORY_STRING}]:-}"
            if [[ "" = "${DOCUMENT_NAME_STRING}" ]]; then
                MAJOR_OUTSIDE_COUNT_INTEGER=$((MAJOR_OUTSIDE_COUNT_INTEGER + 1))

                continue
            fi

            if [[ "" != "${FILTER_STRING}" \
                && "${DOCUMENT_NAME_STRING}" != *"${FILTER_STRING}"* \
                && "${PACKAGE_DIRECTORY_STRING}" != *"${FILTER_STRING}"* ]]; then
                continue
            fi

            DOCUMENT_DECLARED_COUNT_MAP["${DOCUMENT_NAME_STRING}"]=$((${DOCUMENT_DECLARED_COUNT_MAP[${DOCUMENT_NAME_STRING}]:-0} + 1))

            if [[ "${UNION_INVENTORY_STRING}" == *$'\n'"${SYMBOL_STRING}"$'\n'* ]]; then
                DOCUMENT_ENROLLED_COUNT_MAP["${DOCUMENT_NAME_STRING}"]=$((${DOCUMENT_ENROLLED_COUNT_MAP[${DOCUMENT_NAME_STRING}]:-0} + 1))

                continue
            fi

            EVIDENCE_STRING="absent   "
            if [[ "${DOCUMENT_MENTION_STRING_MAP[${MAJOR_DIRECTORY_STRING}$'\t'${DOCUMENT_NAME_STRING}]:-}" == *$'\n'"${SYMBOL_STRING}"$'\n'* ]]; then
                EVIDENCE_STRING="mentioned"
                TOTAL_MENTIONED_COUNT_INTEGER=$((TOTAL_MENTIONED_COUNT_INTEGER + 1))
            fi

            DOCUMENT_ROW_STRING_MAP["${DOCUMENT_NAME_STRING}"]+="    ${EVIDENCE_STRING}  $(printf '%-42s' "${SYMBOL_STRING}")  ${DECLARING_FILE_STRING}"$'\n'
            MAJOR_LISTED_COUNT_INTEGER=$((MAJOR_LISTED_COUNT_INTEGER + 1))
        done < <(build_major_declaration_index "${MAJOR_DIRECTORY_STRING}")

        if [[ 0 -eq ${DECLARED_COUNT_INTEGER} ]]; then
            fail "${MAJOR_DIRECTORY_STRING} declares nothing: the declaring half is broken, and an empty report would read as a documented major"
        fi

        println ""
        info "${MAJOR_DIRECTORY_STRING}: ${DECLARED_COUNT_INTEGER} exported declaration(s), ${MAJOR_OUTSIDE_COUNT_INTEGER} in a package no document covers"

        for DOCUMENT_NAME_STRING in $(printf '%s\n' "${!DOCUMENT_DECLARED_COUNT_MAP[@]}" | sort); do
            println "  ${DOCUMENT_NAME_STRING}: ${DOCUMENT_ENROLLED_COUNT_MAP[${DOCUMENT_NAME_STRING}]:-0} of ${DOCUMENT_DECLARED_COUNT_MAP[${DOCUMENT_NAME_STRING}]} enrolled"

            if [[ "" != "${DOCUMENT_ROW_STRING_MAP[${DOCUMENT_NAME_STRING}]:-}" ]]; then
                printf '%s' "${DOCUMENT_ROW_STRING_MAP[${DOCUMENT_NAME_STRING}]}" | sort
            fi
        done

        TOTAL_LISTED_COUNT_INTEGER=$((TOTAL_LISTED_COUNT_INTEGER + MAJOR_LISTED_COUNT_INTEGER))
        TOTAL_OUTSIDE_COUNT_INTEGER=$((TOTAL_OUTSIDE_COUNT_INTEGER + MAJOR_OUTSIDE_COUNT_INTEGER))

        unset DOCUMENT_ROW_STRING_MAP DOCUMENT_DECLARED_COUNT_MAP DOCUMENT_ENROLLED_COUNT_MAP
    done

    println ""
    info "${TOTAL_LISTED_COUNT_INTEGER} symbol/document row(s) enrolled in no entry list of any major, ${TOTAL_MENTIONED_COUNT_INTEGER} of them mentioned by the covering document; ${TOTAL_OUTSIDE_COUNT_INTEGER} symbol(s) outside every covered package"
    success "reported without a verdict: whether a door deserves an entry is read at the site"

    exit 0
fi

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

            FINDING_KEY_STRING="${MAJOR_DIRECTORY_STRING}"$'\t'"${DOCUMENT_NAME_STRING}"$'\t'"${SYMBOL_STRING}"

            if [[ "" != "${FINDING_SEEN_INTEGER_MAP[${FINDING_KEY_STRING}]:-}" ]]; then
                continue
            fi

            FINDING_SEEN_INTEGER_MAP["${FINDING_KEY_STRING}"]=1
            GAP_COUNT_INTEGER=$((GAP_COUNT_INTEGER + 1))

            if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${FINDING_KEY_STRING}]:-}" ]]; then
                BASELINE_CONSUMED_INTEGER_MAP["${FINDING_KEY_STRING}"]=$((BASELINE_CONSUMED_INTEGER_MAP[${FINDING_KEY_STRING}] + 1))

                continue
            fi

            println "  gap  ${DOCUMENT_NAME_STRING}: ${MAJOR_DIRECTORY_STRING} declares ${SYMBOL_STRING} in ${DECLARING_FILE_STRING} and does not document it"
            UNRECORDED_COUNT_INTEGER=$((UNRECORDED_COUNT_INTEGER + 1))
        done
    done <<< "${ENTRY_UNION_STRING}"
done

for BASELINE_KEY_STRING in "${!BASELINE_CONSUMED_INTEGER_MAP[@]}"; do
    if [[ 0 -lt ${BASELINE_CONSUMED_INTEGER_MAP[${BASELINE_KEY_STRING}]} ]]; then
        continue
    fi

    println "  stale    ${BASELINE_PATH_STRING}:${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]} excuses a gap that no longer exists — delete the line (${BASELINE_REASON_STRING_MAP[${BASELINE_KEY_STRING}]})"
    STALE_COUNT_INTEGER=$((STALE_COUNT_INTEGER + 1))
done

info "checked ${CHECKED_COUNT_INTEGER} symbol/major pairs across ${#DOCUMENT_NAME_STRING_LIST[@]} package documents"
info "${GAP_COUNT_INTEGER} gap(s): $((GAP_COUNT_INTEGER - UNRECORDED_COUNT_INTEGER)) recorded in the baseline, ${UNRECORDED_COUNT_INTEGER} not"

if [[ 0 -lt ${BROKEN_COUNT_INTEGER} ]]; then
    fail "${BROKEN_COUNT_INTEGER} unreadable baseline line(s): no verdict is possible over a baseline that does not parse"
fi

if [[ 0 -lt ${UNRECORDED_COUNT_INTEGER} ]]; then
    fail "${UNRECORDED_COUNT_INTEGER} documentation gap(s): a major ships a symbol another major documents, and does not document it"
fi

if [[ 0 -lt ${STALE_COUNT_INTEGER} ]]; then
    fail "${STALE_COUNT_INTEGER} stale baseline line(s): the gap each one excuses has been closed, so the line has to go"
fi

success "package documentation agrees with the code of every major outside the ${GAP_COUNT_INTEGER} recorded divergences"
