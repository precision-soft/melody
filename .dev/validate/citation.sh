#!/usr/bin/env bash

# Checks what the documents of a major CITE against what that major declares.
#
#   .dev/validate/citation.sh [--major v2] [--report] [--samples]
#
# The neighbouring documentation check asks whether a symbol a major ships is documented. It cannot ask the
# opposite, and the opposite is where the rot is: a document keeps naming what the code stopped having.
# Measured, that check stayed green for months over a section of UPGRADE.md that announced, with a code
# example and a symptom, a method (`session/contract.Session.SetShared`) that no major declares — the change
# was reverted before release, the changelogs were cleaned and the upgrade guide was not. A reader following
# that note implements against an API that does not exist, and nothing in the tree could say so.
#
# The parity check cannot say so either. It compares one major's documents with the next major's, and only
# their section titles: prose is never compared, and a claim that is equally wrong on both majors is equally
# invisible. It also reads nothing outside `<major>/.documentation`, so the per-integration README and
# CHANGELOG files — the documents a consumer of an integration actually reads — are compared to nothing at
# all, by anything, anywhere.
#
# Three dimensions are asserted, each blind to what the others see:
#
#   symbol  — a citation qualified by a package this major declares, such as `cache.Item`. The qualifier is
#             what makes the citation checkable: it says the name belongs to melody rather than to the
#             standard library, so the name must be declared somewhere in this major.
#   linked  — a citation that links to a .go file, such as [`(*Manager).Session`](../../session/manager.go).
#             This is the strongest claim a document makes, because it names a symbol AND the file that
#             declares it, and it is the one that rots silently when code moves between files.
#   link    — every relative link target of every document resolves on disk. Cheap, and it is the half of a
#             linked citation that survives when the link text names no symbol at all.
#
# One dimension is deliberately NOT asserted, and the omission is written here rather than left to be read
# as coverage. A bare backticked CamelCase token — `Storage`, `ETag`, `MELODY_LOG_LEVEL`, `GET` — cannot be
# told apart from a header name, an environment key, an http verb, a third-party type or a placeholder
# spelling such as `NewXxx`. Measured on the v2 corpus: 189 of roughly 650 distinct bare tokens resolve to
# no declaration, and reading them one by one puts every one of those classes in the baseline, where they
# would drown the two or three that matter. A citation earns an assertion by carrying a qualifier or a link.
#
# The `sample` pass — extracting the fenced go blocks that are whole programs and type-checking them — is
# behind --samples rather than on by default, and is deliberately NOT wired into the full local run or into
# CI. Both neighbouring bands are readable from a checkout alone and run on the host in seconds; this one
# needs a Go toolchain, which on this project lives in the development container. Wiring it into the host
# lane would make the lane fail for want of a compiler rather than for want of a correct document. Run it
# where Go is, when documents change:
#
#   ./dc exec -T dev bash -lc '.dev/validate/citation.sh --samples'
#
# It is the only executable proof a documentation session has — a sample that stopped compiling is a defect
# no amount of reading catches, and one was found exactly that way.
#
# The baseline is a set of claims, not a filter: an entry that stops matching fails the run exactly as
# loudly as an unrecorded citation, so the list cannot rot into a suppression list, and wildcards are
# refused because a pattern grows on its own to cover the next divergence. That refusal is the same
# instinct as the `reviewed_until` expiry in the release auditor's exception list, reached by a different
# route: there an accepted warning resurfaces when the date lapses, here a row dies the moment the thing it
# excuses is fixed.
#
# Division of labour with the release auditor (precision-soft/git-audit), so neither grows into the other:
# that tool reads the RELEASED `## [vX.Y.Z]` sections of a CHANGELOG and judges them against the GitHub
# release they were published as — heading grammar, compare link, title agreement, body overlap, section
# whitelist. It never reads Go. This band never reads GitHub, never looks at a tag, and never judges the
# shape of a changelog section; it reads what every document of a major CITES and asks the code whether
# that exists. The two meet only on the file name, and on opposite halves of it: the auditor owns the
# released blocks, this owns the claims inside any block, `[Unreleased]` included.
#
# This script has no Go mutants, being shell. Its proof is deliberate breakage, and the experiments are
# written down rather than left implicit: plant a symbol no major declares and the run must report it;
# repoint a link at a file that does not exist and the link dimension must fail; add a baseline row for a
# citation that is clean and the run must call it stale; empty the declaration index and the run must refuse
# to conclude rather than report every citation as broken.
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

BASELINE_PATH_STRING=".dev/validate/citation.baseline"

# the majors this run gates. v3 is measured and printed like the rest and is not gated, for the reason the
# parity band gives for its own pair list: it is the line still under development, its documents are the
# next stage's work, and a lane that is red for a known reason teaches a team to ignore the lane.
GATED_MAJOR_STRING_LIST=(
    "v1"
    "v2"
)

MAJOR_FILTER_STRING=""
REPORT_BOOLEAN="false"
SAMPLES_BOOLEAN="false"

while [[ 0 -lt $# ]]; do
    if [[ "-h" = "${1}" ]]; then
        println "usage: citation.sh [-h] [--major <vN>] [--report] [--samples]"
        println ""
        println "  -h            show this help and exit"
        println "  --major <vN>  narrow to one major, e.g. v2 (default: every major the tree declares)"
        println "  --report      print the findings without a verdict"
        println "  --samples     also extract the whole-program go blocks and compile them (needs Go)"
        exit 0
    elif [[ "--major" = "${1}" ]]; then
        if [[ "" = "${2-}" ]]; then
            fail "--major needs a value, e.g. --major v2"
        fi
        MAJOR_FILTER_STRING="${2}"
        shift 2
    elif [[ "--report" = "${1}" ]]; then
        REPORT_BOOLEAN="true"
        shift
    elif [[ "--samples" = "${1}" ]]; then
        SAMPLES_BOOLEAN="true"
        shift
    else
        fail "unknown flag: ${1}"
    fi
done

TEMPORARY_DIRECTORY_STRING="$(mktemp -d)"
trap 'rm -rf "${TEMPORARY_DIRECTORY_STRING}"' EXIT

# every path the tree would carry into a commit: what git tracks, plus what it does not yet track and does
# not ignore either. `git ls-files` alone answers with the index, and the index belongs to whoever is
# committing, so a document a session ADDS would be invisible to every dimension below until someone staged
# it — the shape of blindness the parity band was measured to have and had to be repaired for.
list_repository_path() {
    git ls-files --cached --others --exclude-standard -- "$@" | sort -u
}

# ------------------------------------------------------------------------------------------------------
# the majors, discovered rather than listed
# ------------------------------------------------------------------------------------------------------

# a major exists when the tree declares a framework module for it: the bare melody path is v1 and every
# `melody/vN` is vN. Discovered for the reason the parity band discovers its modules — the list that has to
# be maintained by hand is the one that gets forgotten when the next major is cut.
list_declared_major() {
    local GO_MOD_PATH_STRING
    while IFS= read -r GO_MOD_PATH_STRING; do
        local MODULE_PATH_STRING
        MODULE_PATH_STRING="$(awk '/^module / { print $2; exit }' "${GO_MOD_PATH_STRING}")"

        if [[ "github.com/precision-soft/melody" = "${MODULE_PATH_STRING}" ]]; then
            printf 'v1\n'
        elif [[ "${MODULE_PATH_STRING}" =~ ^github\.com/precision-soft/melody/(v[0-9]+)$ ]]; then
            printf '%s\n' "${BASH_REMATCH[1]}"
        fi
    done < <(list_repository_path '*go.mod') | sort -u
}

major_directory_for() {
    local MAJOR_STRING="${1:?}"

    if [[ "v1" = "${MAJOR_STRING}" ]]; then
        printf '.'

        return 0
    fi

    printf '%s' "${MAJOR_STRING}"
}

# the source files that belong to a major: its own tree plus the integration modules cut for it. The example
# application is included deliberately — the changelog of a major cites the example's own types by name, and
# excluding it would report every one of those as a symbol the major does not declare.
list_major_source_file() {
    local MAJOR_STRING="${1:?}"

    if [[ "v1" = "${MAJOR_STRING}" ]]; then
        list_repository_path '*.go' \
            | grep -v '_test\.go$' \
            | grep -v '^v[0-9]\+/' \
            | grep -v '^\.dev/' \
            | grep -v '^integrations/[^/]*/v[0-9]\+/' \
            | grep -v '^integrations/[^/]*/[^/]*/v[0-9]\+/'

        return 0
    fi

    {
        list_repository_path "${MAJOR_STRING}/*.go"
        list_repository_path 'integrations/*.go' | grep -E "/${MAJOR_STRING}/"
    } | grep -v '_test\.go$' | sort -u
}

# the documents that belong to a major. Wider than the neighbouring check on purpose: the per-integration
# README and CHANGELOG are the documents a consumer of that integration reads, and no band has ever looked
# at them. The shared repository documents — CONTRIBUTING, SECURITY, the issue templates — belong to no
# major and are left out, so that a single copy is not judged three times against three different trees.
list_major_document() {
    local MAJOR_STRING="${1:?}"
    local MAJOR_DIRECTORY_STRING
    MAJOR_DIRECTORY_STRING="$(major_directory_for "${MAJOR_STRING}")"

    if [[ "." = "${MAJOR_DIRECTORY_STRING}" ]]; then
        list_repository_path '.documentation/*.md' 'README.md' 'CHANGELOG.md'
        list_repository_path 'integrations/*.md' \
            | grep -v '^integrations/[^/]*/v[0-9]\+/' \
            | grep -v '^integrations/[^/]*/[^/]*/v[0-9]\+/' \
            | grep -v '^integrations/README\.md$'

        return 0
    fi

    list_repository_path "${MAJOR_DIRECTORY_STRING}/.documentation/*.md" \
        "${MAJOR_DIRECTORY_STRING}/README.md" "${MAJOR_DIRECTORY_STRING}/CHANGELOG.md"
    list_repository_path 'integrations/*.md' | grep -E "/${MAJOR_STRING}/"
}

# ------------------------------------------------------------------------------------------------------
# the two readers: what the code declares, and what the documents cite
# ------------------------------------------------------------------------------------------------------

DECLARATION_READER_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/declaration.awk"
cat > "${DECLARATION_READER_PATH_STRING}" <<'DECLARATION_AWK'
FNR == 1 { insideBlock = 0; insideType = 0 }

/^(const|var) \($/ { insideBlock = 1; next }

1 == insideBlock && /^\)/ { insideBlock = 0; next }

1 == insideBlock && 0 != match($0, /^    [A-Z][A-Za-z0-9_]*/) {
    print substr($0, 5, RLENGTH - 4) "\t" FILENAME
    next
}

# a type block that opens a brace. Its members — an interface method, a struct field — are cited by their
# own name and are not top-level lines, so an index built from top-level lines alone reports every one of
# them as undeclared. The type's OWN name is printed here rather than left to the top-level rule below,
# because this rule matches first and consumes the line: a form that only opened the block indexed the
# members and lost every interface and every struct in the tree, which reads as a document citing types
# that do not exist.
/^type [A-Za-z_][A-Za-z0-9_]* (interface|struct) \{$/ {
    insideType = 1
    if (0 != match($2, /^[A-Z][A-Za-z0-9_]*$/)) {
        print $2 "\t" FILENAME
    }
    next
}

1 == insideType && /^\}/ { insideType = 0; next }

1 == insideType && 0 != match($0, /^    [A-Z][A-Za-z0-9_]*/) {
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
DECLARATION_AWK

CITATION_READER_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/citation.awk"
cat > "${CITATION_READER_PATH_STRING}" <<'CITATION_AWK'
# prints every code citation a document makes, as `kind<tab>symbol<tab>target<tab>qualifier<tab>anchor`.
# The target is the link the citation carries, or empty. The qualifier is the package name a qualified
# citation was written under, or empty. The anchor identifies the one `[...](...)` the citation came out of,
# so that a link naming a family is judged as one claim rather than as one claim per name.
#
# A citation is a backticked token, wherever it stands. Restricting the reading to list bullets the way the
# neighbouring symbol check does would drop the whole of the upgrade guide, whose claims are made in prose
# and are exactly the ones nothing has ever verified.

function emitToken(token, target,    candidate, previous, count, part, index_, head) {
    candidate = token

    # the end-of-document listings write the declaration keyword: `const ServiceClock`, `type Clock`.
    sub(/^(const|func|type|var) /, "", candidate)

    # a receiver spelling reduces to the name it carries, BEFORE the argument lists go: `(*Manager).Session`
    # becomes Manager.Session. Stripping parentheses first would take the receiver with them and leave a
    # citation beginning with a dot.
    sub(/^\(\*?([A-Za-z][A-Za-z0-9_]*)\)\./, "\\1.", candidate)

    # a generic's type parameter is punctuation on the name: `NewListPayload[T](...)` cites NewListPayload.
    gsub(/\[[^][]*\]/, "", candidate)

    # every argument list goes, innermost first, so that a chain survives as a chain. Dropping everything
    # from the first parenthesis onward instead reduces `Http().SessionTtl()` to `Http`, which is the
    # accessor rather than the setting the link is about — measured, that spelling alone produced fourteen
    # findings across the three majors, every one of them the reader's own doing.
    do {
        previous = candidate
        gsub(/\([^()]*\)/, "", candidate)
    } while (candidate != previous)

    # what is left of a full signature is the declared name followed by the return type, and only the first
    # is what the link asserts. Keeping the rest reads the return type's package-qualified name as a second
    # citation aimed at the same file: measured, `ClockMustFromContainer(…) clockcontract.Clock` linked to
    # clock/service_resolver.go was reported as that file failing to declare `Clock`, which is true and
    # beside the point, because the document never said it did.
    sub(/[[:space:]].*$/, "", candidate)

    # link text that is a path cites no symbol — the roadmap writes [`../http/router.go`](../http/router.go)
    # — and neither does a bare import path, nor a standard library symbol written with its path.
    if (candidate ~ /\//) {
        return
    }

    count = split(candidate, part, ".")
    if (0 == count) {
        return
    }

    # a lowercase leading segment is a package qualifier, and it is what makes the citation checkable on its
    # own: it says the name belongs to this tree rather than to the standard library. Every other segment is
    # a name the document asserts exists.
    head = ""
    index_ = 1
    if (part[1] ~ /^[a-z]/) {
        head = part[1]
        index_ = 2
    }

    for (; index_ <= count; index_++) {
        if ("" != head) {
            emitName("qualified", part[index_], target, head)
        } else {
            emitName("plain", part[index_], target, "")
        }
    }
}

function emitName(kind, name, target, qualifier) {
    if (0 == match(name, /^[A-Z][A-Za-z0-9_]*$/)) {
        return
    }

    print kind "\t" name "\t" target "\t" qualifier "\t" anchorIdentity
}

# the opening bracket that matches the `]` at closePosition, or zero when the line carries none. Walking
# back with a depth counter rather than matching `\[[^]]*\]` is what a nested bracket forces: the documents
# write [`output.NewListPayload[T](...)`](../../cli/output/list_payload.go), and a non-greedy match ends the
# link text at the `]` of `[T]`, which leaves `...` standing as the link target and reports the document as
# pointing at a file named `...`.
function openBracketFor(text, closePosition,    position, depth, character) {
    depth = 0

    for (position = closePosition; 1 <= position; position--) {
        character = substr(text, position, 1)

        if ("]" == character) {
            depth++
        } else if ("[" == character) {
            depth--

            if (0 == depth) {
                return position
            }
        }
    }

    return 0
}

# whether the position falls inside a backtick span. A `](` written inside code — the `[T](...)` of a
# generic call — is punctuation in a sentence, not a link, and reading it as one invents a link target that
# no document ever wrote and that no file could ever satisfy.
function insideCode(text, position,    index_, count) {
    count = 0

    for (index_ = 1; index_ < position; index_++) {
        if ("`" == substr(text, index_, 1)) {
            count++
        }
    }

    return (1 == count % 2)
}

/^[[:space:]]*```/ { insideFence = 1 - insideFence; next }

# a fenced block is code, not prose. Its contents are read by the sample pass, which compiles them; read as
# prose they invent links out of Go that merely looks like markdown — measured, a generic call written
# `container.MustFromResolver[*bunorm.ManagerRegistry](resolver, ServiceManagerRegistryId)` was reported as a
# document pointing at a file whose name carries a comma.
1 == insideFence { next }

{
    line = $0
    scanFrom = 1

    # a linked citation first, so the link travels with the token. Several documents name a whole family
    # inside one link text, so every backticked token in the text is read rather than only the first.
    while (0 != (closePosition = index(substr(line, scanFrom), "](")) ) {
        closePosition = closePosition + scanFrom - 1

        if (1 == insideCode(line, closePosition)) {
            scanFrom = closePosition + 2

            continue
        }

        openPosition = openBracketFor(line, closePosition)
        if (0 == openPosition) {
            scanFrom = closePosition + 2

            continue
        }

        targetEnd = index(substr(line, closePosition + 2), ")")
        if (0 == targetEnd) {
            scanFrom = closePosition + 2

            continue
        }
        targetEnd = targetEnd + closePosition + 1

        linkText = substr(line, openPosition + 1, closePosition - openPosition - 1)
        linkTarget = substr(line, closePosition + 2, targetEnd - closePosition - 2)
        sub(/#.*$/, "", linkTarget)

        scanFrom = targetEnd + 1
        anchorOrdinal++
        anchorIdentity = NR ":" anchorOrdinal

        print "link\t\t" linkTarget "\t\t" anchorIdentity

        # the position is read into locals BEFORE the token is handed on, because emitToken calls match and
        # sub, which reset RSTART and RLENGTH. Advancing on them after the call advances by whatever the
        # callee matched last, which for a token needing no substitution is the token itself: measured
        # before the fix, one line of one document produced output without end.
        while (0 != match(linkText, /`[^`]+`/)) {
            tokenStart = RSTART
            tokenLength = RLENGTH
            token = substr(linkText, tokenStart + 1, tokenLength - 2)
            linkText = substr(linkText, tokenStart + tokenLength)
            emitToken(token, linkTarget)
        }
    }

    line = $0
    anchorIdentity = ""
    while (0 != match(line, /`[^`]+`/)) {
        tokenStart = RSTART
        tokenLength = RLENGTH
        token = substr(line, tokenStart + 1, tokenLength - 2)
        line = substr(line, tokenStart + tokenLength)
        emitToken(token, "")
    }
}
CITATION_AWK

# ------------------------------------------------------------------------------------------------------
# the join
# ------------------------------------------------------------------------------------------------------

JOIN_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/join.awk"
cat > "${JOIN_PATH_STRING}" <<'JOIN_AWK'
function normalizePath(path,    part, count, stack, depth, index_, result) {
    count = split(path, part, "/")
    depth = 0

    for (index_ = 1; index_ <= count; index_++) {
        if ("" == part[index_] || "." == part[index_]) {
            continue
        }

        if (".." == part[index_]) {
            if (0 < depth) {
                depth--
            }

            continue
        }

        stack[++depth] = part[index_]
    }

    result = ""
    for (index_ = 1; index_ <= depth; index_++) {
        result = result (1 == index_ ? "" : "/") stack[index_]
    }

    return result
}

BEGIN { FS = "\t"; OFS = "\t" }

"declaration" == mode { declaredInMajor[$1] = 1; next }

# the file index spans the whole tree rather than the major under test. A link names a FILE, and the claim
# it makes — this file is where that is described — is checkable wherever the file lives. Several documents
# of one major deliberately link into the next, because the capability they describe was only ever built
# there; judging those against the major that wrote the link reports every one of them as broken, which
# would put a dozen deliberate cross-major links into the baseline to be re-read forever.
"global" == mode { declaredInFile[$1 "\t" $2] = 1; next }

"package" == mode { melodyPackage[$1] = 1; next }

# the citation stream, as `kind symbol target qualifier anchor document directory`
{
    kind = $1
    symbol = $2
    target = $3
    qualifier = $4
    anchor = $5
    document = $6
    directory = $7

    if ("link" == kind) {
        if ("" == target || target ~ /^[a-z]+:/ || target ~ /^#/) {
            next
        }

        resolved = normalizePath(directory "/" target)
        linkSeen[resolved] = linkSeen[resolved] " " document

        next
    }

    if ("" != target && target ~ /\.go$/) {
        resolved = normalizePath(directory "/" target)
        anchorKey = document "\t" anchor

        anchorFile[anchorKey] = resolved
        anchorDocument[anchorKey] = document
        if ("" == anchorSymbol[anchorKey]) {
            anchorSymbol[anchorKey] = symbol
        } else if (anchorSymbol[anchorKey] !~ ("(^|/)" symbol "(/|$)")) {
            anchorSymbol[anchorKey] = anchorSymbol[anchorKey] "/" symbol
        }

        if (1 == declaredInFile[symbol "\t" resolved]) {
            anchorSatisfied[anchorKey] = 1
        }

        next
    }

    if ("qualified" == kind) {
        if (1 != melodyPackage[qualifier]) {
            next
        }

        key = qualifier "." symbol
        qualifiedTotal[key] = 1
        qualifiedDocument[key] = document

        if (1 == declaredInMajor[symbol]) {
            qualifiedSatisfied[key] = 1
        }
    }
}

END {
    for (key in qualifiedTotal) {
        countQualified++
        if (1 != qualifiedSatisfied[key]) {
            print "finding", "symbol", key, qualifiedDocument[key]
        }
    }

    # a linked citation is judged per ANCHOR rather than per symbol or per file: a link whose text names a
    # family — the documents write [`MinLength`/`MaxLength`](…) over one file — asserts that the file is
    # where that family is described, not that every name in it is declared there. Requiring every name
    # turns an ordinary shared link into a finding, and requiring none turns the dimension off.
    #
    # The scope is the anchor and not the file, which is the difference between a dimension that works and
    # one that reads as working. Keyed by file, a single correct citation anywhere in the corpus excuses
    # every other citation aimed at that file: measured, moving `Manager` off cache/manager.go and onto
    # cache/in_memory.go left the run green, because other symbols legitimately point at in_memory.go.
    for (key in anchorFile) {
        countLinked++
        if (1 != anchorSatisfied[key]) {
            print "finding", "linked", anchorSymbol[key] " -> " anchorFile[key], anchorDocument[key]
        }
    }

    for (resolved in linkSeen) {
        countLink++
        print "link-target", resolved, linkSeen[resolved], ""
    }

    print "count", countQualified + 0, countLinked + 0, countLink + 0
}
JOIN_AWK

# ------------------------------------------------------------------------------------------------------
# the run
# ------------------------------------------------------------------------------------------------------

FINDING_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/finding"
: > "${FINDING_PATH_STRING}"

record_finding() {
    local MAJOR_STRING="${1:?}"
    local DIMENSION_STRING="${2:?}"
    local KEY_STRING="${3:?}"

    printf '%s\t%s\t%s\n' "${MAJOR_STRING}" "${DIMENSION_STRING}" "${KEY_STRING}" >> "${FINDING_PATH_STRING}"
}

MAJOR_STRING_LIST=()
while IFS= read -r MAJOR_STRING; do
    if [[ "" != "${MAJOR_FILTER_STRING}" && "${MAJOR_FILTER_STRING}" != "${MAJOR_STRING}" ]]; then
        continue
    fi

    MAJOR_STRING_LIST+=("${MAJOR_STRING}")
done < <(list_declared_major)

if [[ 0 -eq ${#MAJOR_STRING_LIST[@]} ]]; then
    fail "no major was discovered${MAJOR_FILTER_STRING:+ for the filter ${MAJOR_FILTER_STRING}}, so there is nothing to check and a green run would mean nothing"
fi

# one index of every declaration the tree makes, keyed by the file that makes it, for the linked dimension
# alone. Built once rather than per major because a link is answered by the file it names wherever that file
# lives, and rebuilding it per major would both cost the walk three times and give the same answer.
GLOBAL_DECLARATION_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/declaration.global"
list_repository_path '*.go' | grep -v '_test\.go$' | grep -v '^\.dev/' \
    | xargs awk -f "${DECLARATION_READER_PATH_STRING}" | sort -u > "${GLOBAL_DECLARATION_PATH_STRING}"

if [[ 0 -eq "$(wc -l < "${GLOBAL_DECLARATION_PATH_STRING}")" ]]; then
    fail "the tree declares no symbol the reader can see, which is the reader being broken rather than the tree being empty — NO VERDICT was produced"
fi

QUALIFIED_COUNT_INTEGER=0
LINKED_COUNT_INTEGER=0
LINK_COUNT_INTEGER=0
DOCUMENT_COUNT_INTEGER=0
SAMPLE_COUNT_INTEGER=0
SAMPLE_FAILURE_COUNT_INTEGER=0
SAMPLE_COMPILED_COUNT_INTEGER=0
declare -A SAMPLE_OUTPUT_STRING_MAP=()

for MAJOR_STRING in "${MAJOR_STRING_LIST[@]}"; do
    MAJOR_SOURCE_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/source.${MAJOR_STRING}"
    list_major_source_file "${MAJOR_STRING}" > "${MAJOR_SOURCE_PATH_STRING}"

    MAJOR_DOCUMENT_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/document.${MAJOR_STRING}"
    list_major_document "${MAJOR_STRING}" > "${MAJOR_DOCUMENT_PATH_STRING}"

    SOURCE_FILE_COUNT_INTEGER="$(wc -l < "${MAJOR_SOURCE_PATH_STRING}")"
    DOCUMENT_FILE_COUNT_INTEGER="$(wc -l < "${MAJOR_DOCUMENT_PATH_STRING}")"

    # an empty half is refused rather than reported. With no declarations every citation reads as broken,
    # and with no documents every run reads as clean: both are the shape of a check that is measuring
    # nothing, and both would otherwise be indistinguishable from an answer.
    if [[ 0 -eq ${SOURCE_FILE_COUNT_INTEGER} ]]; then
        fail "${MAJOR_STRING} yielded no source file, so every citation would read as undeclared and NO VERDICT was produced"
    fi

    if [[ 0 -eq ${DOCUMENT_FILE_COUNT_INTEGER} ]]; then
        fail "${MAJOR_STRING} yielded no document, so the run would be green having read nothing and NO VERDICT was produced"
    fi

    DECLARATION_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/declaration.${MAJOR_STRING}"
    xargs awk -f "${DECLARATION_READER_PATH_STRING}" < "${MAJOR_SOURCE_PATH_STRING}" | sort -u > "${DECLARATION_PATH_STRING}"

    if [[ 0 -eq "$(wc -l < "${DECLARATION_PATH_STRING}")" ]]; then
        fail "${MAJOR_STRING} declares no symbol the reader can see, which is the reader being broken rather than the tree being empty — NO VERDICT was produced"
    fi

    # the package names this major declares, which is what tells a melody citation from a standard library
    # one. The example application is excluded here while its declarations are kept: it holds a package
    # named `url`, and counting that as melody's would turn every `url.Values` and `url.PathEscape` in the
    # documents into a citation of a symbol melody does not have.
    PACKAGE_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/package.${MAJOR_STRING}"
    grep -v -E '(^|/)\.example/' "${MAJOR_SOURCE_PATH_STRING}" \
        | xargs awk '/^package / { print $2; nextfile }' \
        | sed 's/_test$//' | sort -u > "${PACKAGE_PATH_STRING}"

    CITATION_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/citation.${MAJOR_STRING}"
    : > "${CITATION_PATH_STRING}"

    while IFS= read -r DOCUMENT_PATH_STRING; do
        DOCUMENT_DIRECTORY_STRING="$(dirname "${DOCUMENT_PATH_STRING}")"

        awk -f "${CITATION_READER_PATH_STRING}" "${DOCUMENT_PATH_STRING}" \
            | awk -F'\t' -v document="${DOCUMENT_PATH_STRING}" -v directory="${DOCUMENT_DIRECTORY_STRING}" \
                '{ print $0 "\t" document "\t" directory }' >> "${CITATION_PATH_STRING}"
    done < "${MAJOR_DOCUMENT_PATH_STRING}"

    RESULT_PATH_STRING="${TEMPORARY_DIRECTORY_STRING}/result.${MAJOR_STRING}"
    awk -f "${JOIN_PATH_STRING}" \
        mode=declaration "${DECLARATION_PATH_STRING}" \
        mode=global "${GLOBAL_DECLARATION_PATH_STRING}" \
        mode=package "${PACKAGE_PATH_STRING}" \
        mode=citation "${CITATION_PATH_STRING}" > "${RESULT_PATH_STRING}"

    while IFS=$'\t' read -r RESULT_KIND_STRING RESULT_FIRST_STRING RESULT_SECOND_STRING RESULT_THIRD_STRING; do
        if [[ "count" = "${RESULT_KIND_STRING}" ]]; then
            QUALIFIED_COUNT_INTEGER=$((QUALIFIED_COUNT_INTEGER + RESULT_FIRST_STRING))
            LINKED_COUNT_INTEGER=$((LINKED_COUNT_INTEGER + RESULT_SECOND_STRING))
            LINK_COUNT_INTEGER=$((LINK_COUNT_INTEGER + RESULT_THIRD_STRING))

            continue
        fi

        if [[ "finding" = "${RESULT_KIND_STRING}" ]]; then
            record_finding "${MAJOR_STRING}" "${RESULT_FIRST_STRING}" "${RESULT_SECOND_STRING}"

            continue
        fi

        if [[ "link-target" = "${RESULT_KIND_STRING}" ]]; then
            if [[ ! -e "${RESULT_FIRST_STRING}" ]]; then
                record_finding "${MAJOR_STRING}" "link" "${RESULT_FIRST_STRING}"
            fi
        fi
    done < "${RESULT_PATH_STRING}"

    DOCUMENT_COUNT_INTEGER=$((DOCUMENT_COUNT_INTEGER + DOCUMENT_FILE_COUNT_INTEGER))
done

# ------------------------------------------------------------------------------------------------------
# the sample pass, behind --samples because it needs a Go toolchain
# ------------------------------------------------------------------------------------------------------

# type-checks the fenced go blocks that are whole programs. Only those: a fragment is written to be read in
# the middle of a sentence and has no imports and no enclosing declaration, so compiling it would report the
# document as broken for being prose. The count of what is skipped is printed rather than left implicit.
#
# The block is written INSIDE the major's own module, under an ignored directory, because that is what makes
# its imports resolve the way a reader's would — a scratch module elsewhere would need a require and a sum
# entry for every third-party library a sample touches, and would then be type-checking a different
# dependency graph than the one the document describes.
#
# `package main` is rewritten to a unique package name. A main package with no `func main` fails to build for
# a reason that has nothing to do with the sample being correct, and every sample here is an excerpt rather
# than a program someone runs.
# the module that owns a document: the nearest ancestor directory carrying a go.mod. A sample in
# integrations/rueidis/v2/README.md belongs to the rueidis v2 module and not to the framework major that
# happens to share its version number, and building it from the wrong module resolves a different import
# graph than the reader's.
module_directory_for_document() {
    local DOCUMENT_PATH_STRING="${1:?}"
    local DIRECTORY_STRING
    DIRECTORY_STRING="$(dirname "${DOCUMENT_PATH_STRING}")"

    while [[ "." != "${DIRECTORY_STRING}" && "/" != "${DIRECTORY_STRING}" ]]; do
        if [[ -f "${DIRECTORY_STRING}/go.mod" ]]; then
            printf '%s' "${DIRECTORY_STRING}"

            return 0
        fi

        DIRECTORY_STRING="$(dirname "${DIRECTORY_STRING}")"
    done

    printf '.'
}

run_sample_pass() {
    local MAJOR_STRING="${1:?}"

    local WHOLE_COUNT_INTEGER=0
    local FRAGMENT_COUNT_INTEGER=0

    local DOCUMENT_PATH_STRING
    while IFS= read -r DOCUMENT_PATH_STRING; do
        local MODULE_DIRECTORY_STRING
        MODULE_DIRECTORY_STRING="$(module_directory_for_document "${DOCUMENT_PATH_STRING}")"

        local BLOCK_ORDINAL_INTEGER BLOCK_KIND_STRING BLOCK_BODY_PATH_STRING
        while IFS=$'\t' read -r BLOCK_ORDINAL_INTEGER BLOCK_KIND_STRING BLOCK_BODY_PATH_STRING; do
            if [[ "fragment" = "${BLOCK_KIND_STRING}" ]]; then
                FRAGMENT_COUNT_INTEGER=$((FRAGMENT_COUNT_INTEGER + 1))

                continue
            fi

            WHOLE_COUNT_INTEGER=$((WHOLE_COUNT_INTEGER + 1))
            SAMPLE_COUNT_INTEGER=$((SAMPLE_COUNT_INTEGER + 1))

            local SAMPLE_RELATIVE_STRING=".temp/citation-sample/block${BLOCK_ORDINAL_INTEGER}"
            local SAMPLE_DIRECTORY_STRING="${MODULE_DIRECTORY_STRING}/${SAMPLE_RELATIVE_STRING}"

            rm -rf "${SAMPLE_DIRECTORY_STRING}"
            mkdir -p "${SAMPLE_DIRECTORY_STRING}"
            sed 's/^package [A-Za-z_][A-Za-z0-9_]*$/package citationsample/' "${BLOCK_BODY_PATH_STRING}" \
                > "${SAMPLE_DIRECTORY_STRING}/sample.go"

            local BUILD_OUTPUT_STRING
            # -o /dev/null because the question is whether it compiles, not whether it links to something
            # someone keeps: `go build ./path` writes the executable into the working directory, which left
            # a binary standing in the module root of every major the pass touched.
            if BUILD_OUTPUT_STRING="$( cd "${MODULE_DIRECTORY_STRING}" && go build -o /dev/null "./${SAMPLE_RELATIVE_STRING}" 2>&1 )"; then
                SAMPLE_COMPILED_COUNT_INTEGER=$((SAMPLE_COMPILED_COUNT_INTEGER + 1))
            else
                record_finding "${MAJOR_STRING}" "sample" "${DOCUMENT_PATH_STRING} block ${BLOCK_ORDINAL_INTEGER}"
                SAMPLE_OUTPUT_STRING_MAP["${DOCUMENT_PATH_STRING} block ${BLOCK_ORDINAL_INTEGER}"]="${BUILD_OUTPUT_STRING}"
            fi

            rm -rf "${MODULE_DIRECTORY_STRING}/.temp/citation-sample"
        done < <(
            awk -v out="${TEMPORARY_DIRECTORY_STRING}" '
                /^[[:space:]]*```go$/ { inside = 1; ordinal++; body = ""; first = ""; hasImport = 0; next }

                1 == inside && /^[[:space:]]*```/ {
                    inside = 0

                    # a whole program opens with a package clause AND declares its imports. The package
                    # clause alone is not enough: the documents write `package main` over excerpts whose
                    # identifiers come from the paragraph above — a bare interface declaration, three lines
                    # of a call sequence — and type-checking those reports the document as broken for being
                    # written the way documents are written. Measured, requiring the import clause took the
                    # v2 pass from twelve failures to two, and both survivors were real.
                    if (first !~ /^package / || 0 == hasImport) {
                        print ordinal "\tfragment\t"

                        next
                    }

                    path = out "/block" ordinal ".go"
                    printf "%s", body > path
                    close(path)
                    print ordinal "\twhole\t" path

                    next
                }

                1 == inside {
                    body = body $0 "\n"
                    if ("" == first && $0 !~ /^[[:space:]]*$/) {
                        first = $0
                    }
                    if ($0 ~ /^import[ (]/) {
                        hasImport = 1
                    }
                }
            ' "${DOCUMENT_PATH_STRING}"
        )
    done < "${TEMPORARY_DIRECTORY_STRING}/document.${MAJOR_STRING}"

    info "${MAJOR_STRING}: ${WHOLE_COUNT_INTEGER} whole-program sample(s) type-checked, ${FRAGMENT_COUNT_INTEGER} fragment(s) skipped as prose"
}

if [[ "true" = "${SAMPLES_BOOLEAN}" ]]; then
    if ! command -v go > /dev/null 2>&1; then
        fail "--samples needs a Go toolchain and none is on the path, so the pass would be skipped silently"
    fi

    for MAJOR_STRING in "${MAJOR_STRING_LIST[@]}"; do
        run_sample_pass "${MAJOR_STRING}"
    done

    if [[ 0 -eq ${SAMPLE_COUNT_INTEGER} ]]; then
        fail "--samples type-checked nothing, which is the extractor being broken rather than the documents carrying no code — NO VERDICT was produced"
    fi

    # a pass in which NOTHING compiled is the harness being wrong, not every document being wrong, and the
    # two read identically in the output. Measured while this was written: a first form built every sample
    # from the wrong working directory and reported 37 of 37 broken, every line of it the harness's own
    # doing. One sample compiling is the cheapest positive control there is.
    if [[ 0 -eq ${SAMPLE_COMPILED_COUNT_INTEGER} ]]; then
        fail "not one of the ${SAMPLE_COUNT_INTEGER} sample(s) compiled, which is this pass being broken rather than every document being wrong — NO VERDICT was produced"
    fi

    SAMPLE_FAILURE_COUNT_INTEGER=$((SAMPLE_COUNT_INTEGER - SAMPLE_COMPILED_COUNT_INTEGER))

    info "${SAMPLE_COUNT_INTEGER} sample(s) type-checked, ${SAMPLE_COMPILED_COUNT_INTEGER} compiled, ${SAMPLE_FAILURE_COUNT_INTEGER} broken"
fi

info "${DOCUMENT_COUNT_INTEGER} document(s) across ${#MAJOR_STRING_LIST[@]} major(s): ${QUALIFIED_COUNT_INTEGER} qualified citation(s), ${LINKED_COUNT_INTEGER} linked citation(s), ${LINK_COUNT_INTEGER} link target(s)"

# a run that resolved nothing is refused for the reason the parity band refuses a pair whose rewrite
# changed no file: a reader that silently matched nothing produces exactly the output of a tree with
# nothing wrong in it, and one of those two readings is worthless.
if [[ 0 -eq ${QUALIFIED_COUNT_INTEGER} || 0 -eq ${LINKED_COUNT_INTEGER} ]]; then
    fail "the reader found no citation to check, which is the reader being broken rather than the documents being clean — NO VERDICT was produced"
fi

FINDING_COUNT_INTEGER="$(wc -l < "${FINDING_PATH_STRING}")"

if [[ "true" = "${REPORT_BOOLEAN}" ]]; then
    while IFS=$'\t' read -r FINDING_MAJOR_STRING FINDING_DIMENSION_STRING FINDING_KEY_STRING; do
        if [[ "" = "${FINDING_MAJOR_STRING}" ]]; then
            continue
        fi

        println "  finding  ${FINDING_DIMENSION_STRING}  ${FINDING_MAJOR_STRING}: ${FINDING_KEY_STRING}"
    done < <(sort "${FINDING_PATH_STRING}")

    warning "report only: ${FINDING_COUNT_INTEGER} finding(s) printed and NO VERDICT was produced"

    exit 0
fi

# ------------------------------------------------------------------------------------------------------
# the baseline, read as a set of claims rather than as a filter
# ------------------------------------------------------------------------------------------------------

if [[ ! -f "${BASELINE_PATH_STRING}" ]]; then
    fail "the baseline is missing: ${BASELINE_PATH_STRING}. Without it every recorded citation reads as new, so restore it rather than let the run invent a verdict"
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

declare -A DISCOVERED_MAJOR_BOOLEAN_MAP=()
for MAJOR_STRING in "${MAJOR_STRING_LIST[@]}"; do
    DISCOVERED_MAJOR_BOOLEAN_MAP["${MAJOR_STRING}"]="true"
done

BASELINE_LINE_NUMBER_INTEGER=0
BROKEN_COUNT_INTEGER=0

while IFS= read -r BASELINE_LINE_STRING || [[ "" != "${BASELINE_LINE_STRING}" ]]; do
    BASELINE_LINE_NUMBER_INTEGER=$((BASELINE_LINE_NUMBER_INTEGER + 1))

    if [[ "" = "${BASELINE_LINE_STRING// /}" ]]; then
        continue
    fi

    case "${BASELINE_LINE_STRING}" in \#*) continue ;; esac

    IFS='~' read -r ROW_MAJOR_STRING ROW_DIMENSION_STRING ROW_KEY_STRING ROW_REASON_STRING <<< "${BASELINE_LINE_STRING}"

    ROW_MAJOR_STRING="$(trim_field "${ROW_MAJOR_STRING}")"
    ROW_DIMENSION_STRING="$(trim_field "${ROW_DIMENSION_STRING}")"
    ROW_KEY_STRING="$(trim_field "${ROW_KEY_STRING}")"
    ROW_REASON_STRING="$(trim_field "${ROW_REASON_STRING}")"

    if [[ "" = "${ROW_MAJOR_STRING}" || "" = "${ROW_DIMENSION_STRING}" || "" = "${ROW_KEY_STRING}" || "" = "${ROW_REASON_STRING}" ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} does not split into major ~ dimension ~ key ~ reason"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    if [[ "true" != "${DISCOVERED_MAJOR_BOOLEAN_MAP[${ROW_MAJOR_STRING}]:-false}" ]]; then
        continue
    fi

    if [[ "${ROW_KEY_STRING}" == *"*"* ]]; then
        println "  broken   ${BASELINE_PATH_STRING}:${BASELINE_LINE_NUMBER_INTEGER} carries a wildcard, which would silently grow to cover the next citation: write one line per fact"
        BROKEN_COUNT_INTEGER=$((BROKEN_COUNT_INTEGER + 1))
        continue
    fi

    BASELINE_KEY_STRING="${ROW_MAJOR_STRING}"$'\t'"${ROW_DIMENSION_STRING}"$'\t'"${ROW_KEY_STRING}"

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

is_gated_major() {
    local MAJOR_STRING="${1:?}"

    local GATED_MAJOR_STRING
    for GATED_MAJOR_STRING in "${GATED_MAJOR_STRING_LIST[@]}"; do
        if [[ "${GATED_MAJOR_STRING}" = "${MAJOR_STRING}" ]]; then
            return 0
        fi
    done

    return 1
}

while IFS= read -r FINDING_LINE_STRING; do
    if [[ "" = "${FINDING_LINE_STRING}" ]]; then
        continue
    fi

    FINDING_MAJOR_STRING="${FINDING_LINE_STRING%%$'\t'*}"

    if [[ "" != "${BASELINE_LINE_INTEGER_MAP[${FINDING_LINE_STRING}]:-}" ]]; then
        BASELINE_CONSUMED_INTEGER_MAP["${FINDING_LINE_STRING}"]=$((BASELINE_CONSUMED_INTEGER_MAP[${FINDING_LINE_STRING}] + 1))
        RECORDED_COUNT_INTEGER=$((RECORDED_COUNT_INTEGER + 1))
        continue
    fi

    FINDING_REMAINDER_STRING="${FINDING_LINE_STRING#*$'\t'}"
    FINDING_DIMENSION_STRING="${FINDING_REMAINDER_STRING%%$'\t'*}"
    FINDING_KEY_STRING="${FINDING_REMAINDER_STRING#*$'\t'}"

    if ! is_gated_major "${FINDING_MAJOR_STRING}"; then
        UNGATED_COUNT_INTEGER=$((UNGATED_COUNT_INTEGER + 1))
        continue
    fi

    println "  unrecorded ${FINDING_DIMENSION_STRING}  ${FINDING_MAJOR_STRING}: ${FINDING_KEY_STRING}"
    if [[ "sample" = "${FINDING_DIMENSION_STRING}" ]]; then
        printf '%s\n' "${SAMPLE_OUTPUT_STRING_MAP[${FINDING_KEY_STRING}]:-}" | sed 's/^/             /'
    fi
    UNRECORDED_COUNT_INTEGER=$((UNRECORDED_COUNT_INTEGER + 1))
done < <(sort -u "${FINDING_PATH_STRING}")

STALE_COUNT_INTEGER=0
for BASELINE_KEY_STRING in "${!BASELINE_CONSUMED_INTEGER_MAP[@]}"; do
    # a sample row can only be consumed by a run that ran the sample pass, and that pass is opt-in. Judging
    # it stale in a run without --samples would make every ordinary run demand the deletion of rows the next
    # run with --samples immediately needs back, which is how a staleness rule teaches people to ignore it.
    if [[ "${BASELINE_KEY_STRING}" == *$'\t'"sample"$'\t'* && "true" != "${SAMPLES_BOOLEAN}" ]]; then
        continue
    fi

    if [[ 0 -eq ${BASELINE_CONSUMED_INTEGER_MAP[${BASELINE_KEY_STRING}]} ]]; then
        println "  stale    ${BASELINE_PATH_STRING}:${BASELINE_LINE_INTEGER_MAP[${BASELINE_KEY_STRING}]} excuses a citation that no longer needs excusing — delete the line (${BASELINE_REASON_STRING_MAP[${BASELINE_KEY_STRING}]})"
        STALE_COUNT_INTEGER=$((STALE_COUNT_INTEGER + 1))
    fi
done

info "${FINDING_COUNT_INTEGER} finding(s): ${RECORDED_COUNT_INTEGER} recorded in the baseline, ${UNRECORDED_COUNT_INTEGER} not, ${UNGATED_COUNT_INTEGER} on a major this band does not gate"

if [[ 0 -lt ${BROKEN_COUNT_INTEGER} ]]; then
    fail "${BROKEN_COUNT_INTEGER} baseline row(s) cannot be read, so the run abandons rather than conclude anything from a list it does not understand"
fi

if [[ 0 -lt ${UNRECORDED_COUNT_INTEGER} || 0 -lt ${STALE_COUNT_INTEGER} ]]; then
    fail "${UNRECORDED_COUNT_INTEGER} unrecorded finding(s) and ${STALE_COUNT_INTEGER} stale baseline entr(ies)"
fi

success "every citation the documents make resolves in the major that makes it"
