#!/usr/bin/env bash

# Checks one major of melody against another, after the import path is rewritten.
#
#   .dev/validate/parity.sh [--pair v1:v2] [--module integrations/cron] [--report]
#
# v1 and v2 are the same framework twice over. The port was a copy with the module path rewritten, not a
# translation, and the two are separate Go modules, so no build, no vet run and no test binary ever sees
# them together: a fix that lands on one major and is forgotten on the other compiles cleanly, passes its
# suite and reads correctly in its own documents, on both sides. Nothing in the tree asserted otherwise
# until this check existed — every porting session measured it by hand in a scratchpad script and threw
# the script away, so the verdicts it reached could not be reproduced afterwards by anyone.
#
# Six dimensions, because each is blind to what the others see:
#
#   file      — a file present on one side only. The text comparison structurally cannot see it: it
#               compares pairs, and there is no pair. This is what makes an identical count honest.
#   text      — a change inside a file whose API did not move: an inverted condition, a dropped branch,
#               a constant, a comment still naming the wrong major, and the runtime symbol prefixes in
#               application/boot_collision.go, where a wrong prefix is a live defect that every
#               name-comparing dimension reads as identical.
#   symbol    — the exported production surface. On a pair that legitimately carries a large text diff,
#               this is the assertion that survives: the API boundary is still exactly what was agreed.
#   test      — the name of every test, and the file it lives in. The name catches a guard dropped in
#               the port, which the symbol dimension cannot see because a test is not production code.
#               The file catches the same tests re-housed under another name, which is how one
#               integration arrived looking as though it carried material of its own when it did not.
#   document  — the section titles of the per-major documents. The neighbouring documentation check
#               compares documents against code and never one major's documents against another's, so a
#               section that exists for one major and not for the next is invisible to it.
#   dependency— the set of modules a go.mod requires, without versions and without ordering. go.mod
#               cannot be compared byte for byte: the melody floor is per major by construction, the
#               require block re-sorts because melody/v2 sorts after melody/integrations/bunorm/v2, and
#               the root carries a retract directive that cannot exist on a later major.
#
# The rewrite runs FORWARD, from the older major into the newer major's namespace, and never canonicalizes
# both sides. That is not a preference. Suppose a file under v2 wrongly holds
# `melody/v2/integrations/cron`, which is not a module that exists. Stripping the major from both sides
# turns it into `melody/integrations/cron`, which is exactly what v1 says, so the two compare equal and the
# defect is invisible. Rewriting forward turns v1 into `melody/integrations/cron/v2` and the defect shows.
#
# The rewrite matches the LONGEST module path, one path segment at a time, against the module set the tree
# declares. A `melody/` -> `melody/v2/` substitution is the obvious implementation and it is wrong: it
# turns `melody/integrations/cron` into `melody/v2/integrations/cron`, a module that does not exist,
# because an integration takes its major at its own module boundary and not after `melody`. The control
# corpus below carries that exact row, and a boundary row — `integrations/cronjob`, which must NOT be read
# as `integrations/cron` plus a suffix.
#
# Both controls run before the first pair is compared, the way the mutation harness demands a green control
# before the first mutant, and a failed control produces NO verdict at all rather than a partial one:
#
#   the positive control has to EXERCISE the rewrite, not merely survive it. A control corpus that
#   reproduces files containing no melody import proves the tool is harmless, not that it is correct, so
#   the corpus below is nine rewrites with their expected output, and every pair additionally reports how
#   many of its files the rewrite actually CHANGED. A pair with files but zero rewrites is refused.
#
#   the negative control plants the very defect the check exists for — a file the porter forgot to
#   rewrite — and verifies the plant LANDED before believing the detection. A planted difference that
#   silently failed to apply reads exactly like a tool that sees nothing.
#
# This script has no Go mutants, being shell. Its equivalent proof is deliberate breakage, and the four
# experiments are written down rather than left implicit: break a v2 file and the run must report it;
# add a baseline row for a site that is clean and the run must call it stale; replace the rewriter with
# the naive substitution and the control corpus must fail on the trap row before anything is compared;
# aim the negative control at a file with no module path and the run must refuse to conclude.
#
# Only v1:v2 is gated. Measured: v2 against v3 is 154 identical framework pairs against 341 divergent,
# with 105 files v2 carries and v3 does not, and whole packages v3 has that no earlier major does. The
# invariant that makes a gate worth having — identical modulo the rewrite — is true of v1:v2 and is not
# true of any v3 pair and will not become true. A pair with no baseline is refused rather than compared,
# so it cannot be greened by accident; `--report` prints its findings without a verdict.
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

BASE_MODULE_PATH_STRING="github.com/precision-soft/melody"
BASELINE_PATH_STRING=".dev/validate/parity.baseline"

PAIR_STRING="v1:v2"
MODULE_FILTER_STRING=""
REPORT_BOOLEAN="false"

while [[ 0 -lt $# ]]; do
    if [[ "-h" = "${1}" ]]; then
        println "usage: parity.sh [-h] [--pair <from>:<to>] [--module <module-directory>] [--report]"
        println ""
        println "  -h                   show this help and exit"
        println "  --pair <from>:<to>   compare this pair of majors, e.g. v1:v2 (default), v1:v3, v2:v3"
        println "  --module <path>      narrow to one module pair, e.g. integrations/cron (default: all)"
        println "  --report             print the findings without a verdict, for a pair with no baseline"
        exit 0
    elif [[ "--pair" = "${1}" ]]; then
        if [[ "" = "${2-}" ]]; then
            fail "--pair needs a value, e.g. --pair v1:v2"
        fi
        PAIR_STRING="${2}"
        shift 2
    elif [[ "--module" = "${1}" ]]; then
        if [[ "" = "${2-}" ]]; then
            fail "--module needs a value, e.g. --module integrations/cron"
        fi
        MODULE_FILTER_STRING="${2}"
        shift 2
    elif [[ "--report" = "${1}" ]]; then
        REPORT_BOOLEAN="true"
        shift
    else
        fail "unknown flag: ${1}"
    fi
done

SOURCE_MAJOR_STRING="${PAIR_STRING%%:*}"
TARGET_MAJOR_STRING="${PAIR_STRING##*:}"

if [[ "${SOURCE_MAJOR_STRING}" = "${PAIR_STRING}" || "" = "${SOURCE_MAJOR_STRING}" || "" = "${TARGET_MAJOR_STRING}" ]]; then
    fail "the pair must be written <from>:<to>, e.g. v1:v2 — got '${PAIR_STRING}'"
fi

if [[ "${SOURCE_MAJOR_STRING}" = "${TARGET_MAJOR_STRING}" ]]; then
    fail "the pair compares a major with itself: ${PAIR_STRING}"
fi

for MAJOR_STRING in "${SOURCE_MAJOR_STRING}" "${TARGET_MAJOR_STRING}"; do
    if [[ ! "${MAJOR_STRING}" =~ ^v[0-9]+$ ]]; then
        fail "a major is written vN, e.g. v1, v2 or v3 — got '${MAJOR_STRING}'"
    fi
done

# v1 carries no suffix at all: its module path is the bare `github.com/precision-soft/melody` and its
# tree is the repository root. Every later major is both a path suffix and a directory.
major_suffix_for() {
    local MAJOR_STRING="${1:?}"

    if [[ "v1" = "${MAJOR_STRING}" ]]; then
        printf ''

        return 0
    fi

    printf '%s' "${MAJOR_STRING}"
}

SOURCE_SUFFIX_STRING="$(major_suffix_for "${SOURCE_MAJOR_STRING}")"
TARGET_SUFFIX_STRING="$(major_suffix_for "${TARGET_MAJOR_STRING}")"

# strips the source major from a path or a directory, so the target major can be appended to what is
# left. `integrations/cron/v2` and `github.com/…/integrations/cron/v2` both reduce to their v1 spelling.
strip_source_major() {
    local VALUE_STRING="${1:?}"

    if [[ "" = "${SOURCE_SUFFIX_STRING}" ]]; then
        printf '%s' "${VALUE_STRING}"

        return 0
    fi

    if [[ "${VALUE_STRING}" = "${SOURCE_SUFFIX_STRING}" ]]; then
        printf '.'

        return 0
    fi

    printf '%s' "${VALUE_STRING%/${SOURCE_SUFFIX_STRING}}"
}

append_target_major() {
    local VALUE_STRING="${1:?}"

    if [[ "." = "${VALUE_STRING}" ]]; then
        printf '%s' "${TARGET_SUFFIX_STRING}"

        return 0
    fi

    printf '%s/%s' "${VALUE_STRING}" "${TARGET_SUFFIX_STRING}"
}

# every module the tree declares, as `directory<tab>module path`. Discovered rather than listed, the way
# the live lane discovers its integration modules: a module added later joins on its own, and the list
# that has to be maintained by hand is exactly the one that gets forgotten. The example applications are
# left out because the three of them deliberately do not mirror one another, and the e2e harness because
# it is a v3-only module outside the workspace.
list_declared_module() {
    local GO_MOD_PATH_STRING
    while IFS= read -r GO_MOD_PATH_STRING; do
        if [[ "" = "${GO_MOD_PATH_STRING}" ]]; then
            continue
        fi

        local MODULE_DIRECTORY_STRING
        MODULE_DIRECTORY_STRING="$(dirname "${GO_MOD_PATH_STRING}")"

        if [[ "${MODULE_DIRECTORY_STRING}" == *".example"* || "${MODULE_DIRECTORY_STRING}" == .dev/* ]]; then
            continue
        fi

        local MODULE_PATH_STRING
        MODULE_PATH_STRING="$(awk '/^module / { print $2; exit }' "${GO_MOD_PATH_STRING}")"

        if [[ "" = "${MODULE_PATH_STRING}" ]]; then
            fail "${GO_MOD_PATH_STRING} declares no module path, so the rewrite cannot be built from it"
        fi

        printf '%s\t%s\n' "${MODULE_DIRECTORY_STRING}" "${MODULE_PATH_STRING}"
    done < <(git ls-files '*go.mod' | sort)
}

# the modules that belong to a given major. A module belongs to vN when its declared path ends in /vN,
# and to v1 when it ends in no major at all — the go module convention, read off the tree rather than
# guessed from the directory, because an integration takes its major at its own module boundary
# (integrations/bunorm/mysql/v2) while the framework takes it right after melody (melody/v2).
list_module_of_major() {
    local MAJOR_STRING="${1:?}"

    local MODULE_LINE_STRING
    while IFS= read -r MODULE_LINE_STRING; do
        local MODULE_PATH_STRING="${MODULE_LINE_STRING#*$'\t'}"

        local MODULE_MAJOR_STRING="v1"
        if [[ "${MODULE_PATH_STRING}" =~ /v[0-9]+$ ]]; then
            MODULE_MAJOR_STRING="${MODULE_PATH_STRING##*/}"
        fi

        if [[ "${MAJOR_STRING}" = "${MODULE_MAJOR_STRING}" ]]; then
            printf '%s\n' "${MODULE_LINE_STRING}"
        fi
    done < <(list_declared_module)
}

TEMPORARY_DIRECTORY_STRING="$(mktemp -d)"
trap 'rm -rf "${TEMPORARY_DIRECTORY_STRING}"' EXIT

REWRITER_PROGRAM_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/rewrite.awk"

cat > "${REWRITER_PROGRAM_PATH_STRING}" <<'REWRITER_PROGRAM'
# rewrites every occurrence of a source-major module path into the target major, at the module boundary,
# longest match first, in a single left-to-right pass. Two properties the obvious implementations lose:
#
#   a chain of independent substitutions rewrites its own output — `melody/integrations/cron` becomes
#   `melody/integrations/cron/v2` under the specific rule and then `melody/v2/integrations/cron/v2` under
#   the general one — so the pass has to consume what it has already rewritten and continue past it.
#
#   extending one path SEGMENT at a time is what keeps the module boundary honest: `integrations/cronjob`
#   extends to a candidate that is not a declared module, so it falls back to the root module instead of
#   being read as `integrations/cron` with a suffix glued on.
#
# An occurrence that matches no declared module at all is left exactly as it stands. Inventing a rewrite
# for a path this major does not declare would manufacture a difference rather than find one.
BEGIN {
    MODULE_COUNT_INTEGER = split(MODULE_PATH_LIST_STRING, MODULE_PART_LIST, " ")
    for (INDEX_INTEGER = 1; INDEX_INTEGER <= MODULE_COUNT_INTEGER; INDEX_INTEGER++) {
        if ("" != MODULE_PART_LIST[INDEX_INTEGER]) {
            MODULE_BOOLEAN_MAP[MODULE_PART_LIST[INDEX_INTEGER]] = 1
        }
    }
}

{
    remainingLine = $0
    rewrittenLine = ""

    while (0 != (occurrenceStart = index(remainingLine, BASE_MODULE_PATH_STRING))) {
        rewrittenLine = rewrittenLine substr(remainingLine, 1, occurrenceStart - 1)
        remainingLine = substr(remainingLine, occurrenceStart)

        longestModule = ""
        if (BASE_MODULE_PATH_STRING in MODULE_BOOLEAN_MAP) {
            longestModule = BASE_MODULE_PATH_STRING
        }

        candidateModule = BASE_MODULE_PATH_STRING
        tail = substr(remainingLine, length(BASE_MODULE_PATH_STRING) + 1)

        while (0 != match(tail, /^\/[A-Za-z0-9_.~-]+/)) {
            segment = substr(tail, RSTART, RLENGTH)
            tail = substr(tail, RSTART + RLENGTH)

            # a module path is followed by a dot in a runtime symbol literal — boot_collision.go writes
            # `melody/application.(*Application).` — so the segment as written can carry the symbol on its
            # tail. The head of such a segment is offered as a candidate too, and only a hit moves the match.
            dotIndex = index(segment, ".")
            if (0 != dotIndex) {
                headCandidate = candidateModule substr(segment, 1, dotIndex - 1)
                if (headCandidate in MODULE_BOOLEAN_MAP) {
                    longestModule = headCandidate
                }
            }

            candidateModule = candidateModule segment
            if (candidateModule in MODULE_BOOLEAN_MAP) {
                longestModule = candidateModule
            }
        }

        if ("" == longestModule) {
            rewrittenLine = rewrittenLine BASE_MODULE_PATH_STRING
            remainingLine = substr(remainingLine, length(BASE_MODULE_PATH_STRING) + 1)
            continue
        }

        rewrittenModule = longestModule
        if ("" != SOURCE_SUFFIX_STRING) {
            sourceTail = "/" SOURCE_SUFFIX_STRING
            if (substr(rewrittenModule, length(rewrittenModule) - length(sourceTail) + 1) == sourceTail) {
                rewrittenModule = substr(rewrittenModule, 1, length(rewrittenModule) - length(sourceTail))
            }
        }

        rewrittenLine = rewrittenLine rewrittenModule "/" TARGET_SUFFIX_STRING
        remainingLine = substr(remainingLine, length(longestModule) + 1)
    }

    print rewrittenLine remainingLine
}
REWRITER_PROGRAM

SOURCE_MODULE_PATH_LIST_STRING=""
while IFS= read -r MODULE_LINE_STRING; do
    if [[ "" = "${MODULE_LINE_STRING}" ]]; then
        continue
    fi
    SOURCE_MODULE_PATH_LIST_STRING="${SOURCE_MODULE_PATH_LIST_STRING} ${MODULE_LINE_STRING#*$'\t'}"
done < <(list_module_of_major "${SOURCE_MAJOR_STRING}")

if [[ "" = "${SOURCE_MODULE_PATH_LIST_STRING// /}" ]]; then
    fail "no module of ${SOURCE_MAJOR_STRING} was discovered, so the rewrite has nothing to match — nothing can be concluded"
fi

rewrite_module_path() {
    awk \
        -v BASE_MODULE_PATH_STRING="${BASE_MODULE_PATH_STRING}" \
        -v MODULE_PATH_LIST_STRING="${SOURCE_MODULE_PATH_LIST_STRING}" \
        -v SOURCE_SUFFIX_STRING="${SOURCE_SUFFIX_STRING}" \
        -v TARGET_SUFFIX_STRING="${TARGET_SUFFIX_STRING}" \
        -f "${REWRITER_PROGRAM_PATH_STRING}"
}

# ------------------------------------------------------------------------------------------------------
# the positive control, first layer: nine rewrites with the output each must produce. Written here rather
# than in a fixture so it cannot drift away from the code it certifies, and chosen so that every row is a
# shape the tree actually contains or a trap that has already been paid for. The corpus is expressed for
# v1:v2 and is only exercised when that is the pair, because it is that pair's spellings it asserts.
# ------------------------------------------------------------------------------------------------------
run_positive_control_corpus() {
    if [[ "v1" != "${SOURCE_MAJOR_STRING}" || "v2" != "${TARGET_MAJOR_STRING}" ]]; then
        info "the control corpus is written for v1:v2; the rewrite is exercised for ${PAIR_STRING} by the per-pair rewrite counts alone"

        return 0
    fi

    local CONTROL_ROW_STRING_LIST=(
        '"github.com/precision-soft/melody/exception"~"github.com/precision-soft/melody/v2/exception"'
        'melodybag "github.com/precision-soft/melody/bag/contract"~melodybag "github.com/precision-soft/melody/v2/bag/contract"'
        '"github.com/precision-soft/melody/application.(*Application)."~"github.com/precision-soft/melody/v2/application.(*Application)."'
        '"github.com/precision-soft/melody/integrations/cron"~"github.com/precision-soft/melody/integrations/cron/v2"'
        '"github.com/precision-soft/melody/integrations/bunorm/migrate"~"github.com/precision-soft/melody/integrations/bunorm/migrate/v2"'
        '"github.com/precision-soft/melody/integrations/bunorm/mysql/internal"~"github.com/precision-soft/melody/integrations/bunorm/mysql/v2/internal"'
        '"github.com/precision-soft/melody"~"github.com/precision-soft/melody/v2"'
        '"github.com/precision-soft/melody/integrations/cron.(*Runner)."~"github.com/precision-soft/melody/integrations/cron/v2.(*Runner)."'
        '"github.com/precision-soft/melody/integrations/cronjob"~"github.com/precision-soft/melody/v2/integrations/cronjob"'
    )

    local CONTROL_FAILURE_COUNT_INTEGER=0
    local CONTROL_ROW_STRING
    for CONTROL_ROW_STRING in "${CONTROL_ROW_STRING_LIST[@]}"; do
        local CONTROL_INPUT_STRING="${CONTROL_ROW_STRING%%~*}"
        local CONTROL_EXPECTED_STRING="${CONTROL_ROW_STRING#*~}"

        local CONTROL_ACTUAL_STRING
        CONTROL_ACTUAL_STRING="$(printf '%s\n' "${CONTROL_INPUT_STRING}" | rewrite_module_path)"

        if [[ "${CONTROL_EXPECTED_STRING}" != "${CONTROL_ACTUAL_STRING}" ]]; then
            println "  control  ${CONTROL_INPUT_STRING}"
            println "           expected ${CONTROL_EXPECTED_STRING}"
            println "           actual   ${CONTROL_ACTUAL_STRING}"
            CONTROL_FAILURE_COUNT_INTEGER=$((CONTROL_FAILURE_COUNT_INTEGER + 1))
        fi
    done

    if [[ 0 -lt ${CONTROL_FAILURE_COUNT_INTEGER} ]]; then
        fail "${CONTROL_FAILURE_COUNT_INTEGER} of ${#CONTROL_ROW_STRING_LIST[@]} control rewrites are wrong, so the rewrite certifies nothing and NO VERDICT was produced"
    fi

    info "control corpus green: ${#CONTROL_ROW_STRING_LIST[@]}/${#CONTROL_ROW_STRING_LIST[@]} rewrites"
}

# ------------------------------------------------------------------------------------------------------
# enumeration
# ------------------------------------------------------------------------------------------------------

# the go files that belong to a module and to nothing else: tracked, under the module directory, outside
# every nested module and every dot directory.
#
# git ls-files rather than find is what keeps the exclusions honest without a list to maintain — the
# module cache under .dev-data holds released copies of melody itself, var/ holds run artifacts, and
# scratch tests are named so they are ignored, and all three are already excluded by being untracked. The
# nested-module prune is not tidiness either: without it integrations/bunorm swallows migrate, mysql and
# pgsql together with their own v2 and v3 subtrees, and reports several hundred files as v1-only.
list_module_file() {
    local MODULE_DIRECTORY_STRING="${1:?}"

    local PRUNE_PREFIX_STRING_LIST=()
    local MODULE_LINE_STRING
    while IFS= read -r MODULE_LINE_STRING; do
        local NESTED_DIRECTORY_STRING="${MODULE_LINE_STRING%%$'\t'*}"

        if [[ "${NESTED_DIRECTORY_STRING}" = "${MODULE_DIRECTORY_STRING}" ]]; then
            continue
        fi

        if [[ "." = "${MODULE_DIRECTORY_STRING}" ]]; then
            PRUNE_PREFIX_STRING_LIST+=("${NESTED_DIRECTORY_STRING}/")
        elif [[ "${NESTED_DIRECTORY_STRING}" = "${MODULE_DIRECTORY_STRING}"/* ]]; then
            PRUNE_PREFIX_STRING_LIST+=("${NESTED_DIRECTORY_STRING}/")
        fi
    done < <(list_declared_module)

    local PATHSPEC_STRING="${MODULE_DIRECTORY_STRING}"
    if [[ "." = "${MODULE_DIRECTORY_STRING}" ]]; then
        PATHSPEC_STRING="."
    fi

    local TRACKED_PATH_STRING
    while IFS= read -r TRACKED_PATH_STRING; do
        if [[ "" = "${TRACKED_PATH_STRING}" || "${TRACKED_PATH_STRING}" != *.go ]]; then
            continue
        fi

        local RELATIVE_PATH_STRING="${TRACKED_PATH_STRING}"
        if [[ "." != "${MODULE_DIRECTORY_STRING}" ]]; then
            RELATIVE_PATH_STRING="${TRACKED_PATH_STRING#${MODULE_DIRECTORY_STRING}/}"
        fi

        if [[ "${RELATIVE_PATH_STRING}" = .* || "${RELATIVE_PATH_STRING}" == */.* ]]; then
            continue
        fi

        local IS_PRUNED_BOOLEAN="false"
        local PRUNE_PREFIX_STRING
        for PRUNE_PREFIX_STRING in ${PRUNE_PREFIX_STRING_LIST[@]+"${PRUNE_PREFIX_STRING_LIST[@]}"}; do
            if [[ "${TRACKED_PATH_STRING}" = "${PRUNE_PREFIX_STRING}"* ]]; then
                IS_PRUNED_BOOLEAN="true"
                break
            fi
        done

        if [[ "true" = "${IS_PRUNED_BOOLEAN}" ]]; then
            continue
        fi

        printf '%s\n' "${RELATIVE_PATH_STRING}"
    done < <(git ls-files "${PATHSPEC_STRING}" | sort)
}

# every exported production declaration of a module, as `package.Symbol`, the module root rendering as a
# bare dot. The grouped const/var member is tracked because the eye reads it as one declaration and a
# line-anchored pattern never sees it; the package scopes the name, so New declared in two packages stays
# two facts rather than one.
list_module_symbol() {
    local MODULE_DIRECTORY_STRING="${1:?}"

    local RELATIVE_PATH_STRING
    while IFS= read -r RELATIVE_PATH_STRING; do
        if [[ "" = "${RELATIVE_PATH_STRING}" || "${RELATIVE_PATH_STRING}" = *_test.go ]]; then
            continue
        fi

        local FILE_PATH_STRING="${RELATIVE_PATH_STRING}"
        if [[ "." != "${MODULE_DIRECTORY_STRING}" ]]; then
            FILE_PATH_STRING="${MODULE_DIRECTORY_STRING}/${RELATIVE_PATH_STRING}"
        fi

        local PACKAGE_DIRECTORY_STRING
        PACKAGE_DIRECTORY_STRING="$(dirname "${RELATIVE_PATH_STRING}")"

        awk -v packageDirectory="${PACKAGE_DIRECTORY_STRING}" '
            BEGIN {
                packagePrefix = packageDirectory "."
                if ("." == packageDirectory) {
                    packagePrefix = "."
                }
            }

            FNR == 1 { insideBlock = 0 }

            /^(const|var) \($/ { insideBlock = 1; next }

            1 == insideBlock && /^\)/ { insideBlock = 0; next }

            1 == insideBlock && 0 != match($0, /^    [A-Z][A-Za-z0-9_]*/) {
                print packagePrefix substr($0, 5, RLENGTH - 4)
                next
            }

            0 != match($0, /^func \([a-zA-Z_][A-Za-z0-9_]* \*?[A-Za-z_][A-Za-z0-9_]*\) [A-Z][A-Za-z0-9_]*/) {
                declaration = substr($0, RSTART, RLENGTH)
                sub(/^func \([^)]*\) /, "", declaration)
                print packagePrefix declaration
                next
            }

            0 != match($0, /^(func|type|const|var) [A-Z][A-Za-z0-9_]*/) {
                declaration = substr($0, RSTART, RLENGTH)
                sub(/^(func|type|const|var) /, "", declaration)
                print packagePrefix declaration
                next
            }
        ' "${FILE_PATH_STRING}"
    done < <(list_module_file "${MODULE_DIRECTORY_STRING}") | sort -u
}

# every test a module declares, as `package.Name<tab>file`. The file travels with the name because the
# layout rule is one rule for the framework and for every integration alike — one <source>_test.go per
# source, the test living beside the guard it proves — so the same tests re-housed under another roof is
# a divergence even when the set of names is identical.
list_module_test() {
    local MODULE_DIRECTORY_STRING="${1:?}"

    local RELATIVE_PATH_STRING
    while IFS= read -r RELATIVE_PATH_STRING; do
        if [[ "" = "${RELATIVE_PATH_STRING}" || "${RELATIVE_PATH_STRING}" != *_test.go ]]; then
            continue
        fi

        local FILE_PATH_STRING="${RELATIVE_PATH_STRING}"
        if [[ "." != "${MODULE_DIRECTORY_STRING}" ]]; then
            FILE_PATH_STRING="${MODULE_DIRECTORY_STRING}/${RELATIVE_PATH_STRING}"
        fi

        local PACKAGE_DIRECTORY_STRING
        PACKAGE_DIRECTORY_STRING="$(dirname "${RELATIVE_PATH_STRING}")"

        awk -v packageDirectory="${PACKAGE_DIRECTORY_STRING}" -v sourceFile="${RELATIVE_PATH_STRING}" '
            BEGIN {
                packagePrefix = packageDirectory "."
                if ("." == packageDirectory) {
                    packagePrefix = "."
                }
            }

            0 != match($0, /^func (Test|Benchmark|Fuzz|Example)[A-Za-z0-9_]*/) {
                declaration = substr($0, RSTART, RLENGTH)
                sub(/^func /, "", declaration)
                print packagePrefix declaration "\t" sourceFile
            }
        ' "${FILE_PATH_STRING}"
    done < <(list_module_file "${MODULE_DIRECTORY_STRING}") | sort -u
}

# the modules a go.mod requires, as a set of names without versions and without ordering, with the
# indirect mark kept because a dependency that changed from direct to indirect is a real change. The
# melody modules among them are rewritten, so a v1 module requiring melody and its v2 twin requiring
# melody/v2 agree instead of differing by construction.
list_module_dependency() {
    local MODULE_DIRECTORY_STRING="${1:?}"

    local GO_MOD_PATH_STRING="${MODULE_DIRECTORY_STRING}/go.mod"
    if [[ "." = "${MODULE_DIRECTORY_STRING}" ]]; then
        GO_MOD_PATH_STRING="go.mod"
    fi

    if [[ ! -f "${GO_MOD_PATH_STRING}" ]]; then
        return 0
    fi

    awk '
        /^require \($/ { insideBlock = 1; next }
        1 == insideBlock && /^\)/ { insideBlock = 0; next }

        1 == insideBlock && 0 != match($0, /^[ \t]+[^ \t]+/) {
            name = $1
            mark = ""
            if (0 != index($0, "// indirect")) {
                mark = " // indirect"
            }
            print name mark
            next
        }

        0 != match($0, /^require [^ ]+ /) {
            mark = ""
            if (0 != index($0, "// indirect")) {
                mark = " // indirect"
            }
            print $2 mark
        }
    ' "${GO_MOD_PATH_STRING}" | sort -u
}

# the section titles of the per-major documents, as `document<tab>title`. Only the titles: the prose
# diverges legitimately — one document says it pins v1's surface where the next says v2's — and a
# comparison that read prose would report that forever. The relative depth of a link inside a title is
# normalized away for the same reason: a document one directory deeper writes ../.. where its twin writes
# .., which is arithmetic, not a gap.
list_major_document_heading() {
    local MAJOR_DIRECTORY_STRING="${1:?}"

    local DOCUMENTATION_DIRECTORY_STRING="${MAJOR_DIRECTORY_STRING}/.documentation"
    if [[ "." = "${MAJOR_DIRECTORY_STRING}" ]]; then
        DOCUMENTATION_DIRECTORY_STRING=".documentation"
    fi

    if [[ ! -d "${DOCUMENTATION_DIRECTORY_STRING}" ]]; then
        return 0
    fi

    local DOCUMENT_PATH_STRING
    while IFS= read -r DOCUMENT_PATH_STRING; do
        if [[ "" = "${DOCUMENT_PATH_STRING}" ]]; then
            continue
        fi

        local RELATIVE_DOCUMENT_STRING="${DOCUMENT_PATH_STRING#${DOCUMENTATION_DIRECTORY_STRING}/}"

        awk -v document="${RELATIVE_DOCUMENT_STRING}" '
            /^#/ {
                heading = $0
                gsub(/\]\((\.\.\/)+/, "](@/", heading)
                print document " " heading
            }
        ' "${DOCUMENT_PATH_STRING}"
    done < <(git ls-files "${DOCUMENTATION_DIRECTORY_STRING}" | grep '\.md$' | sort) | sort -u
}

# ------------------------------------------------------------------------------------------------------
# comparison
# ------------------------------------------------------------------------------------------------------

FINDING_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/finding"
: > "${FINDING_PATH_STRING}"

record_finding() {
    local DIMENSION_STRING="${1:?}"
    local MODULE_STRING="${2:?}"
    local KEY_STRING="${3:?}"

    printf '%s\t%s\t%s\n' "${DIMENSION_STRING}" "${MODULE_STRING}" "${KEY_STRING}" >> "${FINDING_PATH_STRING}"
}

FILE_PAIR_COUNT_INTEGER=0
IDENTICAL_COUNT_INTEGER=0
REWRITTEN_COUNT_INTEGER=0
SYMBOL_COUNT_INTEGER=0
TEST_COUNT_INTEGER=0

NEGATIVE_CONTROL_DONE_BOOLEAN="false"

# plants the very defect the check exists to catch — a file the port forgot to rewrite — into a copy of a
# target file that currently compares identical, and requires the comparison to see it. The plant is
# verified to have LANDED first: a substitution that silently matched nothing reads exactly like a
# comparison that is blind, and one of those two conclusions is catastrophically wrong.
run_negative_control() {
    local SOURCE_FILE_PATH_STRING="${1:?}"
    local TARGET_FILE_PATH_STRING="${2:?}"

    local TARGET_MODULE_SPELLING_STRING="${BASE_MODULE_PATH_STRING}/${TARGET_SUFFIX_STRING}/"
    local SOURCE_MODULE_SPELLING_STRING="${BASE_MODULE_PATH_STRING}/"

    local BEFORE_COUNT_INTEGER
    BEFORE_COUNT_INTEGER="$(grep -c -F "${TARGET_MODULE_SPELLING_STRING}" "${TARGET_FILE_PATH_STRING}" || true)"

    if [[ 0 -eq ${BEFORE_COUNT_INTEGER} ]]; then
        return 1
    fi

    local PLANTED_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/planted"
    awk -v target="${TARGET_MODULE_SPELLING_STRING}" -v source="${SOURCE_MODULE_SPELLING_STRING}" '
        0 == done {
            position = index($0, target)
            if (0 != position) {
                $0 = substr($0, 1, position - 1) source substr($0, position + length(target))
                done = 1
            }
        }
        { print }
    ' "${TARGET_FILE_PATH_STRING}" > "${PLANTED_PATH_STRING}"

    if cmp -s "${TARGET_FILE_PATH_STRING}" "${PLANTED_PATH_STRING}"; then
        fail "the negative control could not be planted in ${TARGET_FILE_PATH_STRING}: the file is unchanged, so nothing can be concluded and NO VERDICT was produced"
    fi

    if rewrite_module_path < "${SOURCE_FILE_PATH_STRING}" | cmp -s - "${PLANTED_PATH_STRING}"; then
        fail "the negative control was planted in ${TARGET_FILE_PATH_STRING} and the comparison still reported it identical, so the comparison is blind and NO VERDICT was produced"
    fi

    info "negative control planted and caught in ${TARGET_FILE_PATH_STRING}"

    NEGATIVE_CONTROL_DONE_BOOLEAN="true"

    return 0
}

compare_module_pair() {
    local SOURCE_DIRECTORY_STRING="${1:?}"
    local TARGET_DIRECTORY_STRING="${2:?}"

    local SOURCE_FILE_LIST_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-file"
    local TARGET_FILE_LIST_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-file"

    list_module_file "${SOURCE_DIRECTORY_STRING}" > "${SOURCE_FILE_LIST_PATH_STRING}"
    list_module_file "${TARGET_DIRECTORY_STRING}" > "${TARGET_FILE_LIST_PATH_STRING}"

    if [[ ! -s "${SOURCE_FILE_LIST_PATH_STRING}" ]]; then
        fail "no tracked go file was enumerated for ${SOURCE_DIRECTORY_STRING}, so a clean report would mean nothing — NO VERDICT was produced"
    fi

    local ONLY_SOURCE_COUNT_INTEGER=0
    local ONLY_TARGET_COUNT_INTEGER=0
    local RELATIVE_PATH_STRING

    while IFS= read -r RELATIVE_PATH_STRING; do
        record_finding "file-source" "${SOURCE_DIRECTORY_STRING}" "${RELATIVE_PATH_STRING}"
        ONLY_SOURCE_COUNT_INTEGER=$((ONLY_SOURCE_COUNT_INTEGER + 1))
    done < <(comm -23 "${SOURCE_FILE_LIST_PATH_STRING}" "${TARGET_FILE_LIST_PATH_STRING}")

    while IFS= read -r RELATIVE_PATH_STRING; do
        record_finding "file-target" "${SOURCE_DIRECTORY_STRING}" "${RELATIVE_PATH_STRING}"
        ONLY_TARGET_COUNT_INTEGER=$((ONLY_TARGET_COUNT_INTEGER + 1))
    done < <(comm -13 "${SOURCE_FILE_LIST_PATH_STRING}" "${TARGET_FILE_LIST_PATH_STRING}")

    local PAIR_COUNT_INTEGER=0
    local PAIR_IDENTICAL_COUNT_INTEGER=0
    local PAIR_REWRITTEN_COUNT_INTEGER=0
    local PAIR_DIVERGENT_COUNT_INTEGER=0

    local FIRST_IDENTICAL_SOURCE_STRING=""
    local FIRST_IDENTICAL_TARGET_STRING=""

    while IFS= read -r RELATIVE_PATH_STRING; do
        local SOURCE_FILE_PATH_STRING="${RELATIVE_PATH_STRING}"
        if [[ "." != "${SOURCE_DIRECTORY_STRING}" ]]; then
            SOURCE_FILE_PATH_STRING="${SOURCE_DIRECTORY_STRING}/${RELATIVE_PATH_STRING}"
        fi

        local TARGET_FILE_PATH_STRING="${TARGET_DIRECTORY_STRING}/${RELATIVE_PATH_STRING}"

        PAIR_COUNT_INTEGER=$((PAIR_COUNT_INTEGER + 1))

        local REWRITTEN_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/rewritten"
        rewrite_module_path < "${SOURCE_FILE_PATH_STRING}" > "${REWRITTEN_PATH_STRING}"

        if ! cmp -s "${SOURCE_FILE_PATH_STRING}" "${REWRITTEN_PATH_STRING}"; then
            PAIR_REWRITTEN_COUNT_INTEGER=$((PAIR_REWRITTEN_COUNT_INTEGER + 1))
        fi

        if cmp -s "${REWRITTEN_PATH_STRING}" "${TARGET_FILE_PATH_STRING}"; then
            PAIR_IDENTICAL_COUNT_INTEGER=$((PAIR_IDENTICAL_COUNT_INTEGER + 1))

            if [[ "" = "${FIRST_IDENTICAL_SOURCE_STRING}" ]]; then
                if grep -q -F "${BASE_MODULE_PATH_STRING}/${TARGET_SUFFIX_STRING}/" "${TARGET_FILE_PATH_STRING}"; then
                    FIRST_IDENTICAL_SOURCE_STRING="${SOURCE_FILE_PATH_STRING}"
                    FIRST_IDENTICAL_TARGET_STRING="${TARGET_FILE_PATH_STRING}"
                fi
            fi
        else
            record_finding "text" "${SOURCE_DIRECTORY_STRING}" "${RELATIVE_PATH_STRING}"
            PAIR_DIVERGENT_COUNT_INTEGER=$((PAIR_DIVERGENT_COUNT_INTEGER + 1))
        fi
    done < <(comm -12 "${SOURCE_FILE_LIST_PATH_STRING}" "${TARGET_FILE_LIST_PATH_STRING}")

    if [[ 0 -lt ${PAIR_COUNT_INTEGER} && 0 -eq ${PAIR_REWRITTEN_COUNT_INTEGER} ]]; then
        fail "the rewrite changed no file at all in ${SOURCE_DIRECTORY_STRING}, so whatever it proved, it did not prove the rewrite is right — NO VERDICT was produced"
    fi

    if [[ "false" = "${NEGATIVE_CONTROL_DONE_BOOLEAN}" && "" != "${FIRST_IDENTICAL_SOURCE_STRING}" ]]; then
        run_negative_control "${FIRST_IDENTICAL_SOURCE_STRING}" "${FIRST_IDENTICAL_TARGET_STRING}" || true
    fi

    local SOURCE_SYMBOL_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-symbol"
    local TARGET_SYMBOL_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-symbol"

    list_module_symbol "${SOURCE_DIRECTORY_STRING}" > "${SOURCE_SYMBOL_PATH_STRING}"
    list_module_symbol "${TARGET_DIRECTORY_STRING}" > "${TARGET_SYMBOL_PATH_STRING}"

    SYMBOL_COUNT_INTEGER=$((SYMBOL_COUNT_INTEGER + $(wc -l < "${SOURCE_SYMBOL_PATH_STRING}")))

    local SYMBOL_LINE_STRING
    while IFS= read -r SYMBOL_LINE_STRING; do
        record_finding "symbol-source" "${SOURCE_DIRECTORY_STRING}" "${SYMBOL_LINE_STRING}"
    done < <(comm -23 "${SOURCE_SYMBOL_PATH_STRING}" "${TARGET_SYMBOL_PATH_STRING}")

    while IFS= read -r SYMBOL_LINE_STRING; do
        record_finding "symbol-target" "${SOURCE_DIRECTORY_STRING}" "${SYMBOL_LINE_STRING}"
    done < <(comm -13 "${SOURCE_SYMBOL_PATH_STRING}" "${TARGET_SYMBOL_PATH_STRING}")

    local SOURCE_TEST_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-test"
    local TARGET_TEST_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-test"

    list_module_test "${SOURCE_DIRECTORY_STRING}" > "${SOURCE_TEST_PATH_STRING}"
    list_module_test "${TARGET_DIRECTORY_STRING}" > "${TARGET_TEST_PATH_STRING}"

    TEST_COUNT_INTEGER=$((TEST_COUNT_INTEGER + $(wc -l < "${SOURCE_TEST_PATH_STRING}")))

    local SOURCE_TEST_NAME_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-test-name"
    local TARGET_TEST_NAME_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-test-name"

    cut -f1 "${SOURCE_TEST_PATH_STRING}" | sort -u > "${SOURCE_TEST_NAME_PATH_STRING}"
    cut -f1 "${TARGET_TEST_PATH_STRING}" | sort -u > "${TARGET_TEST_NAME_PATH_STRING}"

    local TEST_LINE_STRING
    while IFS= read -r TEST_LINE_STRING; do
        record_finding "test-source" "${SOURCE_DIRECTORY_STRING}" "${TEST_LINE_STRING}"
    done < <(comm -23 "${SOURCE_TEST_NAME_PATH_STRING}" "${TARGET_TEST_NAME_PATH_STRING}")

    while IFS= read -r TEST_LINE_STRING; do
        record_finding "test-target" "${SOURCE_DIRECTORY_STRING}" "${TEST_LINE_STRING}"
    done < <(comm -13 "${SOURCE_TEST_NAME_PATH_STRING}" "${TARGET_TEST_NAME_PATH_STRING}")

    while IFS= read -r TEST_LINE_STRING; do
        record_finding "test-moved" "${SOURCE_DIRECTORY_STRING}" "${TEST_LINE_STRING}"
    done < <(join -t $'\t' -j 1 -o '0,1.2,2.2' \
        <(sort -t $'\t' -k1,1 -u "${SOURCE_TEST_PATH_STRING}") \
        <(sort -t $'\t' -k1,1 -u "${TARGET_TEST_PATH_STRING}") |
        awk -F'\t' '$2 != $3 { print $1 " " $2 " -> " $3 }')

    local SOURCE_DEPENDENCY_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-dependency"
    local TARGET_DEPENDENCY_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-dependency"

    list_module_dependency "${SOURCE_DIRECTORY_STRING}" | rewrite_module_path | sort -u > "${SOURCE_DEPENDENCY_PATH_STRING}"
    list_module_dependency "${TARGET_DIRECTORY_STRING}" > "${TARGET_DEPENDENCY_PATH_STRING}"

    local DEPENDENCY_LINE_STRING
    while IFS= read -r DEPENDENCY_LINE_STRING; do
        record_finding "dependency-source" "${SOURCE_DIRECTORY_STRING}" "${DEPENDENCY_LINE_STRING}"
    done < <(comm -23 "${SOURCE_DEPENDENCY_PATH_STRING}" "${TARGET_DEPENDENCY_PATH_STRING}")

    while IFS= read -r DEPENDENCY_LINE_STRING; do
        record_finding "dependency-target" "${SOURCE_DIRECTORY_STRING}" "${DEPENDENCY_LINE_STRING}"
    done < <(comm -13 "${SOURCE_DEPENDENCY_PATH_STRING}" "${TARGET_DEPENDENCY_PATH_STRING}")

    FILE_PAIR_COUNT_INTEGER=$((FILE_PAIR_COUNT_INTEGER + PAIR_COUNT_INTEGER))
    IDENTICAL_COUNT_INTEGER=$((IDENTICAL_COUNT_INTEGER + PAIR_IDENTICAL_COUNT_INTEGER))
    REWRITTEN_COUNT_INTEGER=$((REWRITTEN_COUNT_INTEGER + PAIR_REWRITTEN_COUNT_INTEGER))

    printf '  %-30s %5d pairs %5d identical %5d rewritten %5d divergent %3d only-%s %3d only-%s\n' \
        "${SOURCE_DIRECTORY_STRING}" \
        "${PAIR_COUNT_INTEGER}" \
        "${PAIR_IDENTICAL_COUNT_INTEGER}" \
        "${PAIR_REWRITTEN_COUNT_INTEGER}" \
        "${PAIR_DIVERGENT_COUNT_INTEGER}" \
        "${ONLY_SOURCE_COUNT_INTEGER}" "${SOURCE_MAJOR_STRING}" \
        "${ONLY_TARGET_COUNT_INTEGER}" "${TARGET_MAJOR_STRING}"
}

compare_major_document() {
    local SOURCE_DIRECTORY_STRING="${1:?}"
    local TARGET_DIRECTORY_STRING="${2:?}"

    local SOURCE_HEADING_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source-heading"
    local TARGET_HEADING_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/target-heading"

    list_major_document_heading "${SOURCE_DIRECTORY_STRING}" > "${SOURCE_HEADING_PATH_STRING}"
    list_major_document_heading "${TARGET_DIRECTORY_STRING}" > "${TARGET_HEADING_PATH_STRING}"

    if [[ ! -s "${SOURCE_HEADING_PATH_STRING}" ]]; then
        return 0
    fi

    local HEADING_LINE_STRING
    while IFS= read -r HEADING_LINE_STRING; do
        record_finding "document-source" "${SOURCE_DIRECTORY_STRING}" "${HEADING_LINE_STRING}"
    done < <(comm -23 "${SOURCE_HEADING_PATH_STRING}" "${TARGET_HEADING_PATH_STRING}")

    while IFS= read -r HEADING_LINE_STRING; do
        record_finding "document-target" "${SOURCE_DIRECTORY_STRING}" "${HEADING_LINE_STRING}"
    done < <(comm -13 "${SOURCE_HEADING_PATH_STRING}" "${TARGET_HEADING_PATH_STRING}")

    local SOURCE_HEADING_COUNT_INTEGER
    SOURCE_HEADING_COUNT_INTEGER="$(wc -l < "${SOURCE_HEADING_PATH_STRING}")"

    printf '  %-30s %5d headings compared against %s\n' \
        "${SOURCE_DIRECTORY_STRING}/.documentation" "${SOURCE_HEADING_COUNT_INTEGER}" "${TARGET_DIRECTORY_STRING}"
}

# ------------------------------------------------------------------------------------------------------
# run
# ------------------------------------------------------------------------------------------------------

run_positive_control_corpus

MODULE_PAIR_STRING_LIST=()
while IFS= read -r MODULE_LINE_STRING; do
    if [[ "" = "${MODULE_LINE_STRING}" ]]; then
        continue
    fi

    SOURCE_DIRECTORY_STRING="${MODULE_LINE_STRING%%$'\t'*}"

    if [[ "" != "${MODULE_FILTER_STRING}" && "${MODULE_FILTER_STRING}" != "${SOURCE_DIRECTORY_STRING}" ]]; then
        continue
    fi

    TARGET_DIRECTORY_STRING="$(append_target_major "$(strip_source_major "${SOURCE_DIRECTORY_STRING}")")"

    if [[ ! -d "${TARGET_DIRECTORY_STRING}" ]]; then
        println "  missing  ${SOURCE_DIRECTORY_STRING} has no ${TARGET_MAJOR_STRING} twin at ${TARGET_DIRECTORY_STRING}"
        record_finding "module" "${SOURCE_DIRECTORY_STRING}" "${TARGET_DIRECTORY_STRING}"
        continue
    fi

    MODULE_PAIR_STRING_LIST+=("${SOURCE_DIRECTORY_STRING}"$'\t'"${TARGET_DIRECTORY_STRING}")
done < <(list_module_of_major "${SOURCE_MAJOR_STRING}")

if [[ 0 -eq ${#MODULE_PAIR_STRING_LIST[@]} ]]; then
    fail "no module pair was discovered for ${PAIR_STRING}${MODULE_FILTER_STRING:+ under ${MODULE_FILTER_STRING}} — nothing was compared, so nothing can be concluded"
fi

for MODULE_PAIR_STRING in "${MODULE_PAIR_STRING_LIST[@]}"; do
    compare_module_pair "${MODULE_PAIR_STRING%%$'\t'*}" "${MODULE_PAIR_STRING#*$'\t'}"
done

if [[ "" = "${MODULE_FILTER_STRING}" || "." = "${MODULE_FILTER_STRING}" ]]; then
    compare_major_document "$(strip_source_major ".")" "$(append_target_major ".")"
fi

if [[ "false" = "${NEGATIVE_CONTROL_DONE_BOOLEAN}" ]]; then
    fail "the negative control never ran: no compared pair held an identical file carrying a module path, so the comparison is unproven and NO VERDICT was produced"
fi

info "compared ${FILE_PAIR_COUNT_INTEGER} file pairs, ${SYMBOL_COUNT_INTEGER} exported symbols and ${TEST_COUNT_INTEGER} test names across ${#MODULE_PAIR_STRING_LIST[@]} module pairs (${SOURCE_MAJOR_STRING} -> ${TARGET_MAJOR_STRING}); ${IDENTICAL_COUNT_INTEGER} identical, ${REWRITTEN_COUNT_INTEGER} rewritten"

FINDING_COUNT_INTEGER="$(wc -l < "${FINDING_PATH_STRING}")"

if [[ "true" = "${REPORT_BOOLEAN}" ]]; then
    sort "${FINDING_PATH_STRING}" | while IFS=$'\t' read -r DIMENSION_STRING MODULE_STRING KEY_STRING; do
        println "  ${DIMENSION_STRING}  ${MODULE_STRING}: ${KEY_STRING}"
    done

    warning "report only: ${FINDING_COUNT_INTEGER} finding(s) printed and NO VERDICT was produced"

    exit 0
fi

# ------------------------------------------------------------------------------------------------------
# the baseline, read as a set of claims rather than as a filter
# ------------------------------------------------------------------------------------------------------

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

declare -A DISCOVERED_MODULE_BOOLEAN_MAP=()
for MODULE_PAIR_STRING in "${MODULE_PAIR_STRING_LIST[@]}"; do
    DISCOVERED_MODULE_BOOLEAN_MAP["${MODULE_PAIR_STRING%%$'\t'*}"]="true"
done

BASELINE_LINE_NUMBER_INTEGER=0
BROKEN_COUNT_INTEGER=0

while IFS= read -r BASELINE_LINE_STRING || [[ "" != "${BASELINE_LINE_STRING}" ]]; do
    BASELINE_LINE_NUMBER_INTEGER=$((BASELINE_LINE_NUMBER_INTEGER + 1))

    if [[ "" = "${BASELINE_LINE_STRING// /}" ]]; then
        continue
    fi

    case "${BASELINE_LINE_STRING}" in \#*) continue ;; esac

    IFS='~' read -r ROW_PAIR_STRING ROW_DIMENSION_STRING ROW_MODULE_STRING ROW_KEY_STRING ROW_REASON_STRING <<< "${BASELINE_LINE_STRING}"

    # every field is trimmed of surrounding whitespace, deliberately and unlike the mutation table, so the
    # columns can be aligned for a reader without a trailing space turning a module name into one nothing
    # matches. A key's own inner spacing is kept: a document heading is mostly spaces.
    ROW_PAIR_STRING="$(trim_field "${ROW_PAIR_STRING}")"
    ROW_DIMENSION_STRING="$(trim_field "${ROW_DIMENSION_STRING}")"
    ROW_MODULE_STRING="$(trim_field "${ROW_MODULE_STRING}")"
    ROW_KEY_STRING="$(trim_field "${ROW_KEY_STRING}")"
    ROW_REASON_STRING="$(trim_field "${ROW_REASON_STRING}")"

    if [[ "" = "${ROW_PAIR_STRING}" || "" = "${ROW_DIMENSION_STRING}" || "" = "${ROW_MODULE_STRING}" || "" = "${ROW_KEY_STRING}" || "" = "${ROW_REASON_STRING}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} does not split into pair ~ dimension ~ module ~ key ~ reason"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    if [[ "${PAIR_STRING}" != "${ROW_PAIR_STRING}" ]]; then
        continue
    fi

    if [[ "" != "${MODULE_FILTER_STRING}" && "${MODULE_FILTER_STRING}" != "${ROW_MODULE_STRING}" ]]; then
        continue
    fi

    if [[ "true" != "${DISCOVERED_MODULE_BOOLEAN_MAP[${ROW_MODULE_STRING}]:-false}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} names the module ${ROW_MODULE_STRING}, which discovery did not produce for ${PAIR_STRING}"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    if [[ "${ROW_KEY_STRING}" == *"*"* ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} carries a wildcard, which would silently grow to cover the next divergence: write one line per fact"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    BASELINE_KEY_STRING="${ROW_DIMENSION_STRING}"$'\t'"${ROW_MODULE_STRING}"$'\t'"${ROW_KEY_STRING}"

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]:-}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} repeats the entry first written at line ${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]}"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    BASELINE_LINE_INTEGER_MAP["${BASELINE_KEY_STRING}"]="${BASELINE_LINE_NUMBER_INTEGER}"
    BASELINE_REASON_STRING_MAP["${BASELINE_KEY_STRING}"]="${ROW_REASON_STRING}"
    BASELINE_CONSUMED_INTEGER_MAP["${BASELINE_KEY_STRING}"]=0
done < "${BASELINE_PATH_STRING}"

# a pair nobody has ever recorded is refused rather than compared, and the refusal names the way out. The
# findings would otherwise arrive as thousands of lines that read as a catastrophe rather than as the
# ordinary state of a major that was redesigned, and a list authored under the pressure of turning a red
# lane green is exactly the dead-entry list the staleness rule exists to prevent. A pair that genuinely
# diverges nowhere still passes: this refuses only when there is something to record and nothing recorded.
if [[ 0 -eq ${#BASELINE_LINE_INTEGER_MAP[@]} && 0 -lt ${FINDING_COUNT_INTEGER} ]]; then
    fail "no baseline records the ${PAIR_STRING} pair, and the run found ${FINDING_COUNT_INTEGER} difference(s): run with --report to read them without a verdict, and record them deliberately rather than in bulk"
fi

DIVERGENCE_COUNT_INTEGER=0
BASELINED_COUNT_INTEGER=0

while IFS= read -r FINDING_LINE_STRING; do
    if [[ "" = "${FINDING_LINE_STRING}" ]]; then
        continue
    fi

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${FINDING_LINE_STRING}]:-}" ]]; then
        BASELINE_CONSUMED_INTEGER_MAP["${FINDING_LINE_STRING}"]=$((BASELINE_CONSUMED_INTEGER_MAP[${FINDING_LINE_STRING}] + 1))
        BASELINED_COUNT_INTEGER=$((BASELINED_COUNT_INTEGER + 1))
        continue
    fi

    FINDING_DIMENSION_STRING="${FINDING_LINE_STRING%%$'\t'*}"
    FINDING_REMAINDER_STRING="${FINDING_LINE_STRING#*$'\t'}"
    FINDING_MODULE_STRING="${FINDING_REMAINDER_STRING%%$'\t'*}"
    FINDING_KEY_STRING="${FINDING_REMAINDER_STRING#*$'\t'}"

    println "  diverged ${FINDING_DIMENSION_STRING}  ${FINDING_MODULE_STRING}: ${FINDING_KEY_STRING}"
    DIVERGENCE_COUNT_INTEGER=$((DIVERGENCE_COUNT_INTEGER + 1))
done < <(sort "${FINDING_PATH_STRING}")

STALE_COUNT_INTEGER=0
for BASELINE_KEY_STRING in "${!BASELINE_CONSUMED_INTEGER_MAP[@]}"; do
    if [[ 0 -eq ${BASELINE_CONSUMED_INTEGER_MAP[${BASELINE_KEY_STRING}]} ]]; then
        println "  stale    ${BASELINE_PATH_STRING}:${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]} excuses a divergence that no longer exists — delete the line (${BASELINE_REASON_STRING_MAP[${BASELINE_KEY_STRING}]})"
        STALE_COUNT_INTEGER=$((STALE_COUNT_INTEGER + 1))
    fi
done

info "${FINDING_COUNT_INTEGER} finding(s): ${BASELINED_COUNT_INTEGER} recorded in the baseline, ${DIVERGENCE_COUNT_INTEGER} not"

if [[ 0 -lt ${BROKEN_COUNT_INTEGER} ]]; then
    fail "${BROKEN_COUNT_INTEGER} baseline row(s) cannot be read, so the run abandons rather than conclude anything from a list it does not understand"
fi

if [[ 0 -lt ${DIVERGENCE_COUNT_INTEGER} || 0 -lt ${STALE_COUNT_INTEGER} ]]; then
    fail "${DIVERGENCE_COUNT_INTEGER} unrecorded divergence(s) and ${STALE_COUNT_INTEGER} stale baseline entr(ies) between ${SOURCE_MAJOR_STRING} and ${TARGET_MAJOR_STRING}"
fi

success "${SOURCE_MAJOR_STRING} and ${TARGET_MAJOR_STRING} agree everywhere outside the ${BASELINED_COUNT_INTEGER} recorded divergences"
