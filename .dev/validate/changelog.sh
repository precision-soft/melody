#!/usr/bin/env bash

# Checks the SHAPE of every changelog block in the tree: which sections a release block opens, in which
# order, and whether the two published majors file the same entry under the same one.
#
#   .dev/validate/changelog.sh [--report]
#
# The band exists because a misfiled entry is invisible to every other check here. The documentation band
# asks whether a symbol a major ships is documented; the citation band asks whether what a document names
# exists; the parity band compares one major's sources with the next major's. None of them reads a heading.
# Measured on the tree that prompted this: ten of the fourteen `[Unreleased]` blocks of the two published
# majors carried the SAME section twice, because a session wrote its entries into a new section opened at
# the head of the block instead of into the one already there, and a reader looking for the fixes of a
# cycle found the half that was written last.
#
# Four dimensions are asserted:
#
#   duplicate    — the same `### <section>` opened twice inside one `## ` block. A reader who finds the
#                  first one has no way to know a second exists, so half the block is written for nobody.
#   vocabulary   — a `### <section>` outside the list every changelog of this organisation is written
#                  against. An unrecognised heading is a section that no release tooling will carry.
#   order        — the sections of a block in an order other than the declared one, so that a section a
#                  reader looks for is where the previous block did not put it.
#   cross-major  — an entry common to v1 and v2 word for word, filed under different sections. This one is
#                  never excused: the two published majors carry the same framework, so the same sentence
#                  cannot be a fix on one and a feature on the other. Paired within a FAMILY — the root
#                  changelog of one major against the root changelog of the other, an integration's against
#                  the same integration's — because a sentence repeated across two different modules is two
#                  facts about two modules, not one entry filed twice.
#   orphan       — a section opened outside any release block, or an entry standing under no section at
#                  all. Neither can be judged by the three dimensions above, so each is reported rather
#                  than skipped: an entry under no section is an entry no reader of that block will find.
#
# What this band deliberately does NOT do, written here rather than left to be read as coverage: it does
# not decide which section an entry BELONGS under. That judgement needs the code behind the entry — whether
# the door it describes existed before the cycle, whether the sentence repairs a defect or announces a
# surface — and no reader of markdown can make it. Reading for that is session work, and the cross-major
# dimension is what proves such a reading was applied to both majors rather than to one.
#
# The section vocabulary is NOT invented here. It is the list the release auditor (precision-soft/git-audit)
# already judges published release bodies against — `standardSections` in its audit command — narrowed to
# the eight the melody tree actually writes. The three it also accepts and this tree does not use (`Notes`,
# `Upgrade Notes`, `Bug Fixes`) are left out on purpose: a vocabulary wider than the tree lets the next
# spelling in without anyone deciding to admit it. Two sources of truth for the same list is how they drift,
# so when a section is added to one it belongs in the other.
#
# The declared order is derived from the tree rather than chosen:
#
#   Breaking Changes > Added > Changed > Deprecated > Removed > Fixed > Security > Documentation
#
# `Breaking Changes` opens its block in 10 of 10 blocks that have one, `Documentation` closes it in 3 of 3,
# and the middle is the Keep a Changelog order that the header of every one of these files names as the
# format it follows. One judgement is made rather than measured, and is written here with its count:
# `Security` appears three times, twice ahead of `Added` and once in the Keep a Changelog slot, and this
# band takes the Keep a Changelog slot. Three occurrences are too few to call a convention, and the format
# the files declare is the tie-breaker.
#
# Division of labour with the release auditor, so neither grows into the other: that tool reads a RELEASED
# `## [vX.Y.Z]` block and judges it against the GitHub release it was published as — heading grammar,
# compare link, title agreement, body overlap, section whitelist. It needs the network and the tags. This
# band reads the tree alone, judges the internal shape of ANY block including `[Unreleased]`, and never
# looks at a release. They meet on the section vocabulary, which is why this one adopts that one's list
# instead of writing a second. Note for whoever reorders a released block: the auditor's body comparison
# is a token SET (`overlapRatio` over `tokenizeForOverlap`), so it is blind to section order and reordering
# does not move it — what changes is only that the body published on GitHub keeps the old order.
#
# The baseline is a set of claims, not a filter: an entry that stops matching fails the run exactly as
# loudly as an unrecorded finding, so the list cannot rot into a suppression list, and wildcards are
# refused because a pattern grows on its own to cover the next divergence. It carries the v3 residue, which
# is the next stage's work, for the reason the neighbouring bands give for theirs: a lane that is red for a
# known reason teaches a team to ignore the lane.
#
# A cross-major finding is the one thing the baseline cannot hold, and the refusal is deliberate. There is
# no state of the tree in which the same sentence belongs under two different sections on two majors that
# ship the same framework, so a row excusing one would excuse the exact defect the dimension was built to
# catch.
#
# This script has no Go mutants, being shell. Its proof is deliberate breakage, and the experiments are
# written down rather than left implicit: open a second `### Fixed` in a block and the run must report it;
# invent a `### Notes` and the vocabulary dimension must fail; swap two sections of one block and the order
# dimension must fail; move one shared entry on one major alone and the cross-major dimension must fail;
# add a baseline row for a block that is clean and the run must call it stale.
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

BASELINE_PATH_STRING=".dev/validate/changelog.baseline"

# the majors this run gates. All three are gated: v3's changelogs were brought to the same bar during the
# stabilization sweep, so a finding there is as actionable as one on the published majors. The cross-major
# dimension stays a v1/v2 affair by construction — it is the proof of their mirroring.
GATED_SCOPE_STRING_LIST=(
    "v1"
    "v2"
    "v1+v2"
    "v3"
)

# the sections this tree writes, adopted from the release auditor's list rather than written afresh.
SECTION_ORDER_STRING_LIST=(
    "Breaking Changes"
    "Added"
    "Changed"
    "Deprecated"
    "Removed"
    "Fixed"
    "Security"
    "Documentation"
)

REPORT_BOOLEAN="false"

while [[ 0 -lt $# ]]; do
    if [[ "-h" = "${1}" ]]; then
        println "usage: changelog.sh [-h] [--report]"
        println ""
        println "  -h        show this help and exit"
        println "  --report  print the findings without a verdict"
        exit 0
    elif [[ "--report" = "${1}" ]]; then
        REPORT_BOOLEAN="true"
        shift
    else
        fail "unknown flag: ${1}"
    fi
done

TEMPORARY_DIRECTORY_STRING="$(mktemp -d)"
trap 'rm -rf "${TEMPORARY_DIRECTORY_STRING}"' EXIT

# every path the tree would carry into a commit: what git tracks, plus what it does not yet track and does
# not ignore either. `git ls-files` alone answers with the index, and the index belongs to whoever is
# committing, so a changelog a session ADDS would be invisible to every dimension below until someone
# staged it — the shape of blindness the parity band was measured to have and had to be repaired for.
list_repository_path() {
    git ls-files --cached --others --exclude-standard -- "$@" | sort -u
}

# ------------------------------------------------------------------------------------------------------
# the corpus, and the major each file belongs to
# ------------------------------------------------------------------------------------------------------

# a changelog belongs to the module it sits in, and the module names its major: the bare melody path and a
# bare integration path are v1, and every `.../vN` is vN. Discovered rather than listed, for the reason the
# citation band discovers its majors — the list kept by hand is the one forgotten when the next major is
# cut.
major_of_changelog() {
    local CHANGELOG_PATH_STRING="${1:?}"

    local DIRECTORY_STRING
    DIRECTORY_STRING="$(dirname "${CHANGELOG_PATH_STRING}")"

    while [[ "." != "${DIRECTORY_STRING}" && "/" != "${DIRECTORY_STRING}" ]]; do
        if [[ -f "${DIRECTORY_STRING}/go.mod" ]]; then
            break
        fi

        DIRECTORY_STRING="$(dirname "${DIRECTORY_STRING}")"
    done

    if [[ ! -f "${DIRECTORY_STRING}/go.mod" ]]; then
        printf 'unknown'

        return 0
    fi

    local MODULE_PATH_STRING
    MODULE_PATH_STRING="$(awk '/^module / { print $2; exit }' "${DIRECTORY_STRING}/go.mod")"

    if [[ "${MODULE_PATH_STRING}" =~ /(v[0-9]+)$ ]]; then
        printf '%s' "${BASH_REMATCH[1]}"

        return 0
    fi

    printf 'v1'
}

# the family a changelog belongs to: the same module of every major shares one. It is what the cross-major
# dimension pairs within, so that the root changelog of v1 is asked about against the root changelog of v2
# and an integration's against the same integration's, rather than every entry of the tree against every
# other entry that happens to read the same.
family_of_changelog() {
    local CHANGELOG_PATH_STRING="${1:?}"

    local DIRECTORY_STRING
    DIRECTORY_STRING="$(dirname "${CHANGELOG_PATH_STRING}")"

    if [[ "${DIRECTORY_STRING}" =~ ^(.*)/v[0-9]+$ ]]; then
        printf '%s' "${BASH_REMATCH[1]}"

        return 0
    fi

    if [[ "${DIRECTORY_STRING}" =~ ^v[0-9]+$ ]]; then
        printf '.'

        return 0
    fi

    printf '%s' "${DIRECTORY_STRING}"
}

CHANGELOG_PATH_STRING_LIST=()
while IFS= read -r CHANGELOG_PATH_STRING; do
    if [[ "" = "${CHANGELOG_PATH_STRING}" ]]; then
        continue
    fi

    CHANGELOG_PATH_STRING_LIST+=("${CHANGELOG_PATH_STRING}")
done < <(list_repository_path '*CHANGELOG.md' 'CHANGELOG.md')

if [[ 0 -eq ${#CHANGELOG_PATH_STRING_LIST[@]} ]]; then
    fail "the reader found no changelog to check, which is the reader being broken rather than the tree being clean — NO VERDICT was produced"
fi

# ------------------------------------------------------------------------------------------------------
# the reader
# ------------------------------------------------------------------------------------------------------

FINDING_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/finding"
: > "${FINDING_PATH_STRING}"

CROSS_MAJOR_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/cross-major"
: > "${CROSS_MAJOR_PATH_STRING}"

# every entry of the two published majors, as `<major> <TAB> <section> <TAB> <entry text>`, which is what
# the cross-major dimension pairs on.
ENTRY_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/entry"
: > "${ENTRY_PATH_STRING}"

record_finding() {
    local SCOPE_STRING="${1:?}"
    local DIMENSION_STRING="${2:?}"
    local KEY_STRING="${3:?}"

    printf '%s\t%s\t%s\n' "${SCOPE_STRING}" "${DIMENSION_STRING}" "${KEY_STRING}" >> "${FINDING_PATH_STRING}"
}

is_known_section() {
    local SECTION_STRING="${1:?}"

    local KNOWN_SECTION_STRING
    for KNOWN_SECTION_STRING in "${SECTION_ORDER_STRING_LIST[@]}"; do
        if [[ "${KNOWN_SECTION_STRING}" = "${SECTION_STRING}" ]]; then
            return 0
        fi
    done

    return 1
}

section_rank_of() {
    local SECTION_STRING="${1:?}"

    local RANK_INTEGER=0
    local KNOWN_SECTION_STRING
    for KNOWN_SECTION_STRING in "${SECTION_ORDER_STRING_LIST[@]}"; do
        RANK_INTEGER=$((RANK_INTEGER + 1))

        if [[ "${KNOWN_SECTION_STRING}" = "${SECTION_STRING}" ]]; then
            printf '%s' "${RANK_INTEGER}"

            return 0
        fi
    done

    printf '0'
}

BLOCK_COUNT_INTEGER=0
SECTION_COUNT_INTEGER=0
ENTRY_COUNT_INTEGER=0

for CHANGELOG_PATH_STRING in "${CHANGELOG_PATH_STRING_LIST[@]}"; do
    MAJOR_STRING="$(major_of_changelog "${CHANGELOG_PATH_STRING}")"
    FAMILY_STRING="$(family_of_changelog "${CHANGELOG_PATH_STRING}")"

    BLOCK_TITLE_STRING=""
    NEWEST_RELEASED_BLOCK_TITLE_STRING=""
    CURRENT_SECTION_STRING=""
    PREVIOUS_SECTION_STRING=""
    declare -A BLOCK_SECTION_LINE_INTEGER_MAP=()
    FILE_ENTRY_COUNT_INTEGER=0

    LINE_NUMBER_INTEGER=0
    while IFS= read -r LINE_STRING || [[ "" != "${LINE_STRING}" ]]; do
        LINE_NUMBER_INTEGER=$((LINE_NUMBER_INTEGER + 1))

        if [[ "${LINE_STRING}" == "## "* ]]; then
            BLOCK_TITLE_STRING="${LINE_STRING#\#\# }"
            CURRENT_SECTION_STRING=""
            PREVIOUS_SECTION_STRING=""
            BLOCK_SECTION_LINE_INTEGER_MAP=()
            BLOCK_COUNT_INTEGER=$((BLOCK_COUNT_INTEGER + 1))

            if [[ "" = "${NEWEST_RELEASED_BLOCK_TITLE_STRING}" && "Unreleased" != "${BLOCK_TITLE_STRING//[\[\]]/}" ]]; then
                NEWEST_RELEASED_BLOCK_TITLE_STRING="${BLOCK_TITLE_STRING}"
            fi

            continue
        fi

        if [[ "${LINE_STRING}" == "### "* ]]; then
            if [[ "" = "${BLOCK_TITLE_STRING}" ]]; then
                record_finding "${MAJOR_STRING}" "orphan" "${CHANGELOG_PATH_STRING}:${LINE_NUMBER_INTEGER} opens a section outside any release block"

                continue
            fi

            CURRENT_SECTION_STRING="${LINE_STRING#\#\#\# }"
            SECTION_COUNT_INTEGER=$((SECTION_COUNT_INTEGER + 1))

            if [[ "" != "${BLOCK_SECTION_LINE_INTEGER_MAP[${CURRENT_SECTION_STRING}]:-}" ]]; then
                record_finding "${MAJOR_STRING}" "duplicate" "${CHANGELOG_PATH_STRING} ${BLOCK_TITLE_STRING}: ### ${CURRENT_SECTION_STRING} is opened again at line ${LINE_NUMBER_INTEGER}, the first at line ${BLOCK_SECTION_LINE_INTEGER_MAP[${CURRENT_SECTION_STRING}]}"
            else
                BLOCK_SECTION_LINE_INTEGER_MAP["${CURRENT_SECTION_STRING}"]="${LINE_NUMBER_INTEGER}"
            fi

            if ! is_known_section "${CURRENT_SECTION_STRING}"; then
                record_finding "${MAJOR_STRING}" "vocabulary" "${CHANGELOG_PATH_STRING} ${BLOCK_TITLE_STRING}: ### ${CURRENT_SECTION_STRING} is not one of the sections this tree writes"

                PREVIOUS_SECTION_STRING=""

                continue
            fi

            if [[ "" != "${PREVIOUS_SECTION_STRING}" ]]; then
                PREVIOUS_RANK_INTEGER="$(section_rank_of "${PREVIOUS_SECTION_STRING}")"
                CURRENT_RANK_INTEGER="$(section_rank_of "${CURRENT_SECTION_STRING}")"

                if [[ ${CURRENT_RANK_INTEGER} -lt ${PREVIOUS_RANK_INTEGER} ]]; then
                    record_finding "${MAJOR_STRING}" "order" "${CHANGELOG_PATH_STRING} ${BLOCK_TITLE_STRING}: ### ${PREVIOUS_SECTION_STRING} stands before ### ${CURRENT_SECTION_STRING}"
                fi
            fi

            PREVIOUS_SECTION_STRING="${CURRENT_SECTION_STRING}"

            continue
        fi

        if [[ "${LINE_STRING}" == "- "* ]]; then
            if [[ "" = "${CURRENT_SECTION_STRING}" ]]; then
                record_finding "${MAJOR_STRING}" "orphan" "${CHANGELOG_PATH_STRING}:${LINE_NUMBER_INTEGER} carries an entry under no section"

                continue
            fi

            FILE_ENTRY_COUNT_INTEGER=$((FILE_ENTRY_COUNT_INTEGER + 1))
            ENTRY_COUNT_INTEGER=$((ENTRY_COUNT_INTEGER + 1))

            if [[ ("Unreleased" = "${BLOCK_TITLE_STRING//[\[\]]/}" || "${NEWEST_RELEASED_BLOCK_TITLE_STRING}" = "${BLOCK_TITLE_STRING}") && ("v1" = "${MAJOR_STRING}" || "v2" = "${MAJOR_STRING}") ]]; then
                printf '%s\t%s\t%s\t%s\n' "${MAJOR_STRING}" "${CURRENT_SECTION_STRING}" "${FAMILY_STRING}" "${LINE_STRING#- }" >> "${ENTRY_PATH_STRING}"
            fi
        fi
    done < "${CHANGELOG_PATH_STRING}"

    # the reader knows one spelling of an entry — a `- ` at the first column — and this is what refuses a
    # verdict when a file writes another. It is the control the documentation band was measured to be
    # missing, where half its reader saw 376 of 770 entries and stayed green for months over the other 394:
    # a reader anchored on one form reports a clean file and a file it cannot read with the same silence.
    # An indented bullet is deliberately not an entry — it is a list inside the entry above it, which is how
    # the cron changelog spells out the fields of a type — so only the first column is asked about.
    ALIEN_BULLET_COUNT_INTEGER="$(grep -c '^[*+] ' "${CHANGELOG_PATH_STRING}" || true)"

    if [[ 0 -lt ${ALIEN_BULLET_COUNT_INTEGER} ]]; then
        fail "${CHANGELOG_PATH_STRING} writes ${ALIEN_BULLET_COUNT_INTEGER} entr(ies) with a bullet this reader does not know, so what it did read is a subset of the file — NO VERDICT was produced"
    fi

    if [[ 0 -eq ${FILE_ENTRY_COUNT_INTEGER} ]]; then
        fail "the reader took no entry out of ${CHANGELOG_PATH_STRING}, and a changelog with nothing in it is the reader being broken rather than the file being empty — NO VERDICT was produced"
    fi
done

# ------------------------------------------------------------------------------------------------------
# cross-major: the same sentence, filed under two different sections
# ------------------------------------------------------------------------------------------------------

# paired on the entry text itself rather than on position, because the two blocks diverge by hundreds of
# lines and neither is a reordering of the other. An entry that only one major carries is not a finding —
# the two majors legitimately diverge, the example applications most of all — so only the common ones are
# asked about. The corpus is the `[Unreleased]` block plus the newest released block of each file: a
# release train empties `[Unreleased]` into a dated block, and a pairing that read only `[Unreleased]`
# would go empty at that exact commit — the newest released pair carries the same twin proof one commit
# later, and reading both keeps the non-vacuity control below armed across the whole cycle.
CROSS_MAJOR_COUNT_INTEGER=0
COMMON_ENTRY_COUNT_INTEGER=0

if [[ -s "${ENTRY_PATH_STRING}" ]]; then
    awk -F'\t' '
        {
            key = $3 "\x01" $4

            if ("v1" == $1) {
                if (key in firstSection) {
                    next
                }

                firstSection[key] = $2

                next
            }

            secondSection[key] = $2
        }
        END {
            for (key in secondSection) {
                if (!(key in firstSection)) {
                    continue
                }

                printf "%s\n", "common"

                if (firstSection[key] != secondSection[key]) {
                    split(key, part, "\x01")
                    printf "%s\t%s\t%s\t%s\n", firstSection[key], secondSection[key], part[1], substr(part[2], 1, 110) > "/dev/stderr"
                }
            }
        }
    ' "${ENTRY_PATH_STRING}" > "${TEMPORARY_DIRECTORY_STRING}/common" 2> "${CROSS_MAJOR_PATH_STRING}"

    COMMON_ENTRY_COUNT_INTEGER="$(wc -l < "${TEMPORARY_DIRECTORY_STRING}/common")"

    while IFS=$'\t' read -r FIRST_SECTION_STRING SECOND_SECTION_STRING FAMILY_STRING ENTRY_TEXT_STRING; do
        if [[ "" = "${FIRST_SECTION_STRING}" ]]; then
            continue
        fi

        println "  cross-major  ${FAMILY_STRING}: v1 files it under ### ${FIRST_SECTION_STRING} and v2 under ### ${SECOND_SECTION_STRING}: ${ENTRY_TEXT_STRING}"
        CROSS_MAJOR_COUNT_INTEGER=$((CROSS_MAJOR_COUNT_INTEGER + 1))
    done < "${CROSS_MAJOR_PATH_STRING}"
fi

# the dimension is empty by construction on a tree whose two majors were written from one another, and a
# green it produces is not coverage. It is built anyway, and this is what it is for: it is the only thing
# that can prove a session which moved an entry moved it on BOTH majors, which the reading that decides
# where an entry belongs cannot prove about itself.
if [[ 0 -eq ${COMMON_ENTRY_COUNT_INTEGER} ]]; then
    fail "the reader paired no entry between v1 and v2, which is the pairing being broken rather than the two majors having nothing in common — NO VERDICT was produced"
fi

# ------------------------------------------------------------------------------------------------------
# the verdict
# ------------------------------------------------------------------------------------------------------

if [[ 0 -eq ${BLOCK_COUNT_INTEGER} || 0 -eq ${SECTION_COUNT_INTEGER} || 0 -eq ${ENTRY_COUNT_INTEGER} ]]; then
    fail "the reader found no block, section or entry to check, which is the reader being broken rather than the tree being clean — NO VERDICT was produced"
fi

info "${#CHANGELOG_PATH_STRING_LIST[@]} changelog(s): ${BLOCK_COUNT_INTEGER} block(s), ${SECTION_COUNT_INTEGER} section(s), ${ENTRY_COUNT_INTEGER} entr(ies); ${COMMON_ENTRY_COUNT_INTEGER} entr(ies) common to v1 and v2"

FINDING_COUNT_INTEGER="$(wc -l < "${FINDING_PATH_STRING}")"

if [[ "true" = "${REPORT_BOOLEAN}" ]]; then
    while IFS=$'\t' read -r FINDING_SCOPE_STRING FINDING_DIMENSION_STRING FINDING_KEY_STRING; do
        if [[ "" = "${FINDING_SCOPE_STRING}" ]]; then
            continue
        fi

        println "  finding  ${FINDING_DIMENSION_STRING}  ${FINDING_SCOPE_STRING}: ${FINDING_KEY_STRING}"
    done < <(sort "${FINDING_PATH_STRING}")

    warning "report only: ${FINDING_COUNT_INTEGER} finding(s) and ${CROSS_MAJOR_COUNT_INTEGER} cross-major finding(s) printed and NO VERDICT was produced"

    exit 0
fi

# ------------------------------------------------------------------------------------------------------
# the baseline, read as a set of claims rather than as a filter
# ------------------------------------------------------------------------------------------------------

if [[ ! -f "${BASELINE_PATH_STRING}" ]]; then
    fail "the baseline is missing: ${BASELINE_PATH_STRING}. Without it every recorded block reads as new, so restore it rather than let the run invent a verdict"
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

    IFS='~' read -r ROW_SCOPE_STRING ROW_DIMENSION_STRING ROW_KEY_STRING ROW_REASON_STRING <<< "${BASELINE_LINE_STRING}"

    ROW_SCOPE_STRING="$(trim_field "${ROW_SCOPE_STRING}")"
    ROW_DIMENSION_STRING="$(trim_field "${ROW_DIMENSION_STRING}")"
    ROW_KEY_STRING="$(trim_field "${ROW_KEY_STRING}")"
    ROW_REASON_STRING="$(trim_field "${ROW_REASON_STRING}")"

    if [[ "" = "${ROW_SCOPE_STRING}" || "" = "${ROW_DIMENSION_STRING}" || "" = "${ROW_KEY_STRING}" || "" = "${ROW_REASON_STRING}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} does not split into scope ~ dimension ~ key ~ reason"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    if [[ "cross-major" = "${ROW_DIMENSION_STRING}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} excuses a cross-major finding, which is the one thing this list may not hold: the same sentence cannot belong under two sections on two majors that ship the same framework"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    if [[ "${ROW_KEY_STRING}" == *"*"* ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} carries a wildcard, which would silently grow to cover the next block: write one line per fact"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    BASELINE_KEY_STRING="${ROW_SCOPE_STRING}"$'\t'"${ROW_DIMENSION_STRING}"$'\t'"${ROW_KEY_STRING}"

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]:-}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} repeats the entry first written at line ${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]}"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    BASELINE_LINE_INTEGER_MAP["${BASELINE_KEY_STRING}"]="${BASELINE_LINE_NUMBER_INTEGER}"
    BASELINE_REASON_STRING_MAP["${BASELINE_KEY_STRING}"]="${ROW_REASON_STRING}"
    BASELINE_CONSUMED_INTEGER_MAP["${BASELINE_KEY_STRING}"]=0
done < "${BASELINE_PATH_STRING}"

UNRECORDED_COUNT_INTEGER=0
RECORDED_COUNT_INTEGER=0
UNGATED_COUNT_INTEGER=0

is_gated_scope() {
    local SCOPE_STRING="${1:?}"

    local GATED_SCOPE_STRING
    for GATED_SCOPE_STRING in "${GATED_SCOPE_STRING_LIST[@]}"; do
        if [[ "${GATED_SCOPE_STRING}" = "${SCOPE_STRING}" ]]; then
            return 0
        fi
    done

    return 1
}

while IFS= read -r FINDING_LINE_STRING; do
    if [[ "" = "${FINDING_LINE_STRING}" ]]; then
        continue
    fi

    FINDING_SCOPE_STRING="${FINDING_LINE_STRING%%$'\t'*}"

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${FINDING_LINE_STRING}]:-}" ]]; then
        BASELINE_CONSUMED_INTEGER_MAP["${FINDING_LINE_STRING}"]=$((BASELINE_CONSUMED_INTEGER_MAP[${FINDING_LINE_STRING}] + 1))
        RECORDED_COUNT_INTEGER=$((RECORDED_COUNT_INTEGER + 1))
        continue
    fi

    FINDING_REMAINDER_STRING="${FINDING_LINE_STRING#*$'\t'}"
    FINDING_DIMENSION_STRING="${FINDING_REMAINDER_STRING%%$'\t'*}"
    FINDING_KEY_STRING="${FINDING_REMAINDER_STRING#*$'\t'}"

    if ! is_gated_scope "${FINDING_SCOPE_STRING}"; then
        UNGATED_COUNT_INTEGER=$((UNGATED_COUNT_INTEGER + 1))
        continue
    fi

    println "  unrecorded ${FINDING_DIMENSION_STRING}  ${FINDING_SCOPE_STRING}: ${FINDING_KEY_STRING}"
    UNRECORDED_COUNT_INTEGER=$((UNRECORDED_COUNT_INTEGER + 1))
done < <(sort -u "${FINDING_PATH_STRING}")

STALE_COUNT_INTEGER=0
for BASELINE_KEY_STRING in "${!BASELINE_CONSUMED_INTEGER_MAP[@]}"; do
    if [[ 0 -eq ${BASELINE_CONSUMED_INTEGER_MAP[${BASELINE_KEY_STRING}]} ]]; then
        println "  stale    ${BASELINE_PATH_STRING}:${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]} excuses a block that no longer needs excusing — delete the line (${BASELINE_REASON_STRING_MAP[${BASELINE_KEY_STRING}]})"
        STALE_COUNT_INTEGER=$((STALE_COUNT_INTEGER + 1))
    fi
done

info "${FINDING_COUNT_INTEGER} finding(s): ${RECORDED_COUNT_INTEGER} recorded in the baseline, ${UNRECORDED_COUNT_INTEGER} not, ${UNGATED_COUNT_INTEGER} on a major this band does not gate"

if [[ 0 -lt ${BROKEN_COUNT_INTEGER} ]]; then
    fail "${BROKEN_COUNT_INTEGER} baseline row(s) cannot be read, so the run abandons rather than conclude anything from a list it does not understand"
fi

if [[ 0 -lt ${CROSS_MAJOR_COUNT_INTEGER} ]]; then
    fail "${CROSS_MAJOR_COUNT_INTEGER} entr(ies) common to v1 and v2 are filed under different sections, which no baseline row may excuse"
fi

if [[ 0 -lt ${UNRECORDED_COUNT_INTEGER} || 0 -lt ${STALE_COUNT_INTEGER} ]]; then
    fail "${UNRECORDED_COUNT_INTEGER} unrecorded finding(s) and ${STALE_COUNT_INTEGER} stale baseline entr(ies)"
fi

success "every changelog block opens each of its sections once, in the declared order, and the two published majors file every shared entry alike"
