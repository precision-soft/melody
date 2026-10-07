#!/bin/bash
set -e

source ${HOME}/.profile

export PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

cd ${WORKDIR}

mkdir -p /go/pkg/mod
mkdir -p /go/cache/go-build
touch ${HOME}/.bash_history

# put the shared icons (logo/favicon) into every example's public/ directory. They live in ONE place in
# the tree — <root>/.assets — and are git-ignored under the examples, so a checkout carries no copy that
# could go stale. The mapping from source name to served name is not restated here: each example's
# assets/sync-icons.mjs owns it, and this loop runs that script. It runs for all three majors rather than
# only the one this container serves, because the end-to-end harness builds all three out of the tree.
for EXAMPLE in "${WORKDIR}.example" "${WORKDIR}v2/.example" "${WORKDIR}v3/.example"; do
    [[ -f "${EXAMPLE}/assets/sync-icons.mjs" ]] || continue
    command -v node >/dev/null 2>&1 || continue
    ( cd "${EXAMPLE}/assets" && node sync-icons.mjs ) >/dev/null 2>&1 \
        && echo "[melody-dev] icons synced into ${EXAMPLE#${WORKDIR}}" \
        || echo "[melody-dev] icon sync FAILED for ${EXAMPLE#${WORKDIR}}; the pages will 404 on favicon and logo"
done

REFLEX_ENABLED="${MELODY_DEV_REFLEX_ENABLED:-1}"
EXAMPLE_DIR="${MELODY_DEV_EXAMPLE_DIR:-${WORKDIR}v3/.example}"
RUN_COMMAND="${MELODY_DEV_RUN_COMMAND:-go run .}"

if [[ "" = "${RUN_COMMAND}" ]] || [[ ! -d "${EXAMPLE_DIR}" ]]; then
    echo "[melody-dev] no example to run (run command empty or '${EXAMPLE_DIR}' missing); idling"
    exec sleep infinity
fi

cd "${EXAMPLE_DIR}"

# bound the supervised example's journal. The example logs at debug, and the dev prometheus scrapes /metrics every
# few seconds, so the file grows by every request's dispatch records with nothing else rotating it. Past
# MELODY_DEV_LOG_ROTATE_BYTES the file is renamed to <file>.1 (one generation, replaced at the next rotation) and the
# serving process is sent SIGHUP, which the framework's file journal answers by reopening its path. Only a major whose
# serving process arms that reopen can be rotated this way: a process that does not would take SIGHUP's terminating
# disposition, so the value is 0 (off) unless the service sets it. The target is the child of the supervisor's own run
# command started in this directory, never a cli process the e2e harness runs beside it. A hold file next to the
# journal, touched by the harness when a run starts, pauses the rotation for two hours, because the harness counts
# and reads journal lines across its sections and a rotation in the middle would move them out of the file it reads.
LOG_ROTATE_BYTES="${MELODY_DEV_LOG_ROTATE_BYTES:-0}"
LOG_FILE="${MELODY_DEV_LOG_FILE:-${EXAMPLE_DIR}/var/log/dev.log}"
LOG_ROTATE_HOLD_FILE="$(dirname "${LOG_FILE}")/.rotation-hold"
# go run names the binary it builds after the package directory; comm keeps the first 15 characters of a process name
EXAMPLE_BINARY_NAME="$(basename "${EXAMPLE_DIR}")"
EXAMPLE_BINARY_NAME="${EXAMPLE_BINARY_NAME:0:15}"

serving_process_id() {
    local candidate
    local run_process_id
    local child_process_id
    for candidate in /proc/[0-9]*; do
        [[ "${RUN_COMMAND} " = "$(tr '\0' ' ' <"${candidate}/cmdline" 2>/dev/null)" ]] || continue
        [[ "${EXAMPLE_DIR}" = "$(readlink "${candidate}/cwd" 2>/dev/null)" ]] || continue
        run_process_id="${candidate#/proc/}"
        # during a rebuild the child of go run is the compiler, which SIGHUP would kill; only the example's own binary is
        # the target, and a tick that finds none leaves the rotation to the next one
        for child_process_id in $(pgrep -P "${run_process_id}"); do
            [[ "${EXAMPLE_BINARY_NAME}" = "$(cat "/proc/${child_process_id}/comm" 2>/dev/null)" ]] || continue
            echo "${child_process_id}"
            return 0
        done
        return 0
    done
}

if [[ "${LOG_ROTATE_BYTES}" =~ ^[0-9]+$ ]] && [[ 0 -lt "${LOG_ROTATE_BYTES}" ]]; then
    (
        # the loop answers every failure itself; under the inherited errexit a failing last command of a list would end
        # the subshell between the rename and the SIGHUP, leaving the process writing into the renamed file
        set +e
        while true; do
            sleep 30
            [[ -f "${LOG_FILE}" ]] || continue
            [[ "${LOG_ROTATE_BYTES}" -lt "$(stat -c %s "${LOG_FILE}")" ]] || continue
            [[ -z "$(find "${LOG_ROTATE_HOLD_FILE}" -mmin -120 2>/dev/null)" ]] || continue
            serving_pid="$(serving_process_id)"
            [[ -n "${serving_pid}" ]] || continue
            mv -f "${LOG_FILE}" "${LOG_FILE}.1" || continue
            # the process runs as root; the file it reopens is created here first, owned as the renamed one was, so the
            # journal in the bind-mounted tree stays writable by the host user who owned it. noclobber: a journal something
            # recreated in the window after the rename is kept, not truncated, and owned the same way
            (set -o noclobber; : >"${LOG_FILE}") 2>/dev/null
            chown "$(stat -c %u:%g "${LOG_FILE}.1")" "${LOG_FILE}" \
                || echo "[melody-dev] journal recreated but its owner could not be set; ${LOG_FILE} stays owned by root"
            kill -HUP "${serving_pid}" \
                && echo "[melody-dev] journal rotated past ${LOG_ROTATE_BYTES} bytes ($(date '+%H:%M:%S')); ${LOG_FILE}.1 holds the previous one" \
                || echo "[melody-dev] journal renamed but process ${serving_pid} did not take SIGHUP; the example's next start opens a fresh ${LOG_FILE}"
        done
    ) &
    echo "[melody-dev] journal rotation armed at ${LOG_ROTATE_BYTES} bytes for ${LOG_FILE}"
fi

# build the example frontend bundle (TypeScript -> public/assets/app.js) so the
# example is functional in the browser on startup and picks up local .ts edits.
# The bundle is NOT committed — it is generated from assets/app.ts and git-ignored — so a build that
# cannot run leaves no app.js at all: the pages load and every interaction through
# window.melodyExample.* is dead until `cd assets && npm ci && npm run build` succeeds.
# Which example this builds is decided by MELODY_DEV_EXAMPLE_DIR, so each of the three supervised
# services (dev-v1, dev-v2, dev) builds its own major's bundle.
if [[ -f "assets/package.json" ]] && command -v npm >/dev/null 2>&1; then
    echo "[melody-dev] building example frontend bundle"
    (
        cd assets
        [[ -d node_modules ]] || npm ci --no-audit --no-fund
        npm run build
    ) && echo "[melody-dev] frontend bundle built" \
        || echo "[melody-dev] frontend bundle build FAILED; public/assets/app.js is absent and the browser interface will not work"

    # hot-reload the frontend bundle by polling the .ts sources: this host's bind
    # mount does not propagate inotify events into the container (so esbuild --watch
    # would never fire), but file CONTENT does sync, so re-hashing the sources and
    # rebuilding on change works. A browser refresh then serves the rebuilt bundle.
    if [[ "1" = "${REFLEX_ENABLED}" ]]; then
        (
            cd assets
            last=""
            while true; do
                current="$(cat ./*.ts 2>/dev/null | md5sum)"
                if [[ "${current}" != "${last}" ]]; then
                    if [[ -n "${last}" ]]; then
                        npm run build >/dev/null 2>&1 \
                            && echo "[melody-dev] frontend bundle rebuilt ($(date '+%H:%M:%S'))"
                    fi
                    last="${current}"
                fi
                sleep 1
            done
        ) &
        echo "[melody-dev] polling example frontend .ts for changes"
    fi
fi

if [[ "1" = "${REFLEX_ENABLED}" ]] && command -v reflex >/dev/null 2>&1; then
    echo "[melody-dev] reflex hot-reload watching ${EXAMPLE_DIR}"
    echo "[melody-dev] running: ${RUN_COMMAND}"
    # the generated bundle is ignored so an esbuild rebuild does not restart the
    # Go server (it serves public/assets/app.js from disk on each request anyway), and
    # var/ is ignored because it is what the running process writes — its journal and its
    # sessions file — so a sign-in that saves the sessions does not restart the server.
    exec reflex -s --all -r '\.go$|\.html$|\.css$|\.js$|\.svg$|(^|/)\.env(\..*)?$|\.ya?ml$|\.json$|\.toml$' -G '.git/' -R '(^|/)var/' -G 'public/assets/app.js' -- bash -c "
        export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"
        echo ''
        echo \"[melody-dev] rebuild triggered \$(date '+%Y-%m-%d %H:%M:%S')\"
        # supervisor: restart the example if it exits on its own (e.g. a boot panic outside the
        # DB/redis retry window), but exit cleanly when reflex signals a file-change restart so the
        # loop does not fight reflex's kill-and-rerun.
        trap 'exit 0' TERM INT
        while true; do
            ${RUN_COMMAND}
            echo \"[melody-dev] example exited (status \$?); restarting in 2s\"
            sleep 2
        done
    "
fi

echo "[melody-dev] reflex disabled; running with a restart supervisor: ${RUN_COMMAND}"
# run the example in the background and wait on it so this bash (PID 1) can service signals: on
# docker stop/restart, forward SIGTERM to the example for a graceful shutdown, wait for it, then exit
# the loop (a foreground child would defer the trap, so PID 1 would never relay the stop and docker
# would SIGKILL the whole tree after the grace period).
exec bash -c "
    export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"
    child=0
    trap 'kill -TERM \"\${child}\" 2>/dev/null; wait \"\${child}\" 2>/dev/null; exit 0' TERM INT
    while true; do
        ${RUN_COMMAND} &
        child=\$!
        wait \"\${child}\"
        echo \"[melody-dev] example exited (status \$?); restarting in 2s\"
        sleep 2
    done
"
