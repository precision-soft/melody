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
    # Go server (it serves public/assets/app.js from disk on each request anyway).
    exec reflex -s --all -r '\.go$|\.html$|\.css$|\.js$|\.svg$|(^|/)\.env(\..*)?$|\.ya?ml$|\.json$|\.toml$' -G '.git/' -G 'public/assets/app.js' -- bash -c "
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
