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
    exec reflex -s --all -r '\.go$|(^|/)\.env(\..*)?$|\.ya?ml$|\.json$|\.toml$' -G '.git/' -- bash -c "
        export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"
        echo ''
        echo \"[melody-dev] rebuild triggered \$(date '+%Y-%m-%d %H:%M:%S')\"
        ${RUN_COMMAND}
    "
fi

echo "[melody-dev] reflex disabled; running: ${RUN_COMMAND}"
exec bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\${PATH}\"; ${RUN_COMMAND}"
