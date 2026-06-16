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

if [[ "1" = "${REFLEX_ENABLED}" ]] && command -v reflex >/dev/null 2>&1; then
    echo "[melody-dev] reflex hot-reload watching ${EXAMPLE_DIR}"
    echo "[melody-dev] running: ${RUN_COMMAND}"
    exec reflex -s --all -r '\.go$|\.html$|\.css$|\.js$|\.svg$|(^|/)\.env(\..*)?$|\.ya?ml$|\.json$|\.toml$' -G '.git/' -- bash -c "
        export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"
        echo ''
        echo \"[melody-dev] rebuild triggered \$(date '+%Y-%m-%d %H:%M:%S')\"
        ${RUN_COMMAND}
    "
fi

echo "[melody-dev] reflex disabled; running: ${RUN_COMMAND}"
exec bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"; ${RUN_COMMAND}"
