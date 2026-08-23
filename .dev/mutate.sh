#!/usr/bin/env bash
#
# Mutation harness. Proves a guard by mutating its own line and requiring a named test to fail.
#
#   docker exec melody-dev-1 bash /app/.dev/run-batch.sh "bash /app/.dev/mutate.sh /app/.dev/mutate/lot<N>.table"
#
# A table row has seven tilde-separated fields:
#
#   file ~ expected line ~ occurrence ~ anchor ~ replacement ~ package ~ -run pattern
#
# Blank lines and lines opening with # are skipped.
#
# The mutation is applied to a container-local copy of the tree, never through the mount: writing
# through /app reaches the host working tree, and a restore that copies back has left mutants
# accumulated there before.
#
# A row is abandoned as BROKEN rather than reported when it cannot prove anything:
#
#   - it does not split into all seven fields, or the anchor is empty
#   - the file does not exist in the copy
#   - the requested occurrence of the anchor does not sit on the expected line — textual twins put the
#     same anchor at the same indentation in two functions, and a mistaken occurrence moves the
#     mutation into the twin, where the target test passes through the other half and the guard is
#     reported as surviving
#   - the anchor survives the replacement, so the mutation never landed
#   - the mutant does not compile — a substitution that leaves an import or a variable unused proves
#     nothing, and neither does `if false`, `X && !X` (vet's bools check) or a rename
#   - the -run pattern matches no test
#
# The unmutated copy must be green for every package the table names before any mutant runs: a red
# result over an already-red package says nothing about the mutation.

set -uo pipefail

TABLE="${1:-}"

if [ -z "${TABLE}" ]; then
    echo "usage: mutate.sh <table>"
    exit 2
fi

if [ ! -f "${TABLE}" ]; then
    echo "the table ${TABLE} does not exist"
    exit 2
fi

SOURCE=/app
COPY="/tmp/mutrepo-$(basename "${TABLE}" | tr -c 'a-zA-Z0-9_-' '_')"
BACKUP="${COPY}.backup"

rm -rf "${COPY}"
mkdir -p "${COPY}"
tar -C "${SOURCE}" --exclude=.dev-data --exclude=.git --exclude=node_modules -cf - . | tar -C "${COPY}" -xf -

cd "${COPY}" || exit 1

killed=0
survived=0
broken=0
total=0

while IFS='~' read -r file line occurrence anchor replacement package runpattern tags; do
    [ -z "${file}" ] && continue
    case "${file}" in \#*) continue ;; esac
    echo "${package} ${tags}"
done < "${TABLE}" | sort -u | while read -r package tags; do
    if [ -n "${tags}" ]; then
        if ! go test -tags "${tags}" "${package}" >/dev/null 2>&1; then
            echo "CONTROL FAILED for ${package} under -tags ${tags} — abandoning, nothing can be concluded"
            exit 1
        fi
        echo "control green: ${package} (-tags ${tags})"
    else
        if ! go test "${package}" >/dev/null 2>&1; then
            echo "CONTROL FAILED for ${package} — abandoning, nothing can be concluded"
            exit 1
        fi
        echo "control green: ${package}"
    fi
done || exit 1

while IFS='~' read -r file line occurrence anchor replacement package runpattern tags; do
    [ -z "${file}" ] && continue
    case "${file}" in \#*) continue ;; esac

    total=$((total + 1))

    # the eighth field is optional: a source behind a build tag is invisible to the default build,
    # so both its guard and its test only exist under that tag
    if [ -n "${tags}" ]; then
        TAGFLAG="-tags ${tags}"
    else
        TAGFLAG=""
    fi

    if [ -z "${anchor}" ] || [ -z "${line}" ] || [ -z "${occurrence}" ] || [ -z "${package}" ] || [ -z "${runpattern}" ]; then
        echo "[${total}] BROKEN: row does not split into all fields"
        broken=$((broken + 1))
        continue
    fi

    if [ ! -f "${COPY}/${file}" ]; then
        echo "[${total}] BROKEN: ${file} does not exist in the copy"
        broken=$((broken + 1))
        continue
    fi

    actual_line=$(grep -n -F "${anchor}" "${COPY}/${file}" | sed -n "${occurrence}p" | cut -d: -f1)
    if [ "${actual_line}" != "${line}" ]; then
        echo "[${total}] BROKEN: occurrence ${occurrence} of the anchor sits on line ${actual_line:-<none>}, expected ${line}"
        broken=$((broken + 1))
        continue
    fi

    before=$(grep -c -F "${anchor}" "${COPY}/${file}")

    cp "${COPY}/${file}" "${BACKUP}"

    awk -v ln="${line}" -v old="${anchor}" -v new="${replacement}" '
        NR == ln {
            idx = index($0, old)
            if (idx > 0) {
                $0 = substr($0, 1, idx - 1) new substr($0, idx + length(old))
            }
        }
        { print }
    ' "${BACKUP}" > "${COPY}/${file}"

    after=$(grep -c -F "${anchor}" "${COPY}/${file}")

    if [ "${after}" -ge "${before}" ]; then
        echo "[${total}] BROKEN: the mutation did not land (anchor count ${before} -> ${after})"
        broken=$((broken + 1))
        cp "${BACKUP}" "${COPY}/${file}"
        continue
    fi

    listed=$(go test ${TAGFLAG} "${package}" -run "${runpattern}" -list "${runpattern}" 2>&1)
    if echo "${listed}" | grep -qE "build failed|cannot use|undefined:|syntax error|declared and not used|too many|not enough"; then
        echo "[${total}] BROKEN: the mutant does not compile"
        broken=$((broken + 1))
        cp "${BACKUP}" "${COPY}/${file}"
        continue
    fi

    matched=$(echo "${listed}" | grep -c "^Test")
    if [ "${matched}" -eq 0 ]; then
        echo "[${total}] BROKEN: -run '${runpattern}' matches no test"
        broken=$((broken + 1))
        cp "${BACKUP}" "${COPY}/${file}"
        continue
    fi

    if go test ${TAGFLAG} "${package}" -run "${runpattern}" >/dev/null 2>&1; then
        echo "[${total}] SURVIVED: ${file}:${line} '${anchor}' -> '${replacement}' (${matched} test(s))"
        survived=$((survived + 1))
    else
        echo "[${total}] KILLED: ${file}:${line} '${anchor}' -> '${replacement}' (${matched} test(s))"
        killed=$((killed + 1))
    fi

    cp "${BACKUP}" "${COPY}/${file}"
done < "${TABLE}"

rm -f "${BACKUP}"

echo "----"
echo "total=${total} killed=${killed} survived=${survived} broken=${broken}"
