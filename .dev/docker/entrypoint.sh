#!/bin/bash
set -e

source ${HOME}/.profile

export PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

cd ${WORKDIR}

if [[ -d ".git" ]] && [[ -f ".dev/git-hook/install.sh" ]]; then
    bash .dev/git-hook/install.sh || true
fi

mkdir -p /go/pkg/mod
mkdir -p /go/cache/go-build
touch ${HOME}/.bash_history

# sync shared assets (logo/favicon) into the example apps so they have a single
# source of truth in .assets (the example copies are git-ignored and generated here)
ASSETS_DIR="${WORKDIR}.assets"
if [[ -d "${ASSETS_DIR}" ]]; then
    for EXAMPLE in "${WORKDIR}.example" "${WORKDIR}v2/.example" "${WORKDIR}v3/.example"; do
        [[ -d "${EXAMPLE}/public" ]] || continue
        mkdir -p "${EXAMPLE}/public/assets"
        cp -f "${ASSETS_DIR}/favicon.ico" "${EXAMPLE}/public/favicon.ico" 2>/dev/null || true
        cp -f "${ASSETS_DIR}/logo.svg" "${EXAMPLE}/public/assets/favicon.svg" 2>/dev/null || true
        cp -f "${ASSETS_DIR}/logo.png" "${EXAMPLE}/public/assets/logo.png" 2>/dev/null || true
        cp -f "${ASSETS_DIR}/logo.png" "${EXAMPLE}/public/assets/apple-touch-icon.png" 2>/dev/null || true
    done
    echo "[melody-dev] assets synced into examples from ${ASSETS_DIR}"
fi

REFLEX_ENABLED="${MELODY_DEV_REFLEX_ENABLED:-1}"
EXAMPLE_DIR="${MELODY_DEV_EXAMPLE_DIR:-${WORKDIR}v3/.example}"
RUN_COMMAND="${MELODY_DEV_RUN_COMMAND:-go run .}"

if [[ "" = "${RUN_COMMAND}" ]] || [[ ! -d "${EXAMPLE_DIR}" ]]; then
    echo "[melody-dev] no example to run (run command empty or '${EXAMPLE_DIR}' missing); idling"
    exec sleep infinity
fi

cd "${EXAMPLE_DIR}"

# build the example frontend bundle (TypeScript -> public/assets/app.js) so the
# example is functional in the browser on startup and picks up local .ts edits;
# the committed bundle stays as the fallback if the build cannot run.
if [[ -f "assets/package.json" ]] && command -v npm >/dev/null 2>&1; then
    echo "[melody-dev] building example frontend bundle"
    (
        cd assets
        [[ -d node_modules ]] || npm ci --no-audit --no-fund
        npm run build
    ) && echo "[melody-dev] frontend bundle built" \
        || echo "[melody-dev] frontend bundle build failed; serving the committed bundle"

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
        ${RUN_COMMAND}
    "
fi

echo "[melody-dev] reflex disabled; running: ${RUN_COMMAND}"
exec bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"; ${RUN_COMMAND}"
