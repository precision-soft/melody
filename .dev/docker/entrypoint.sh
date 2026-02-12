#!/bin/bash
set -e

source ${HOME}/.profile

cd ${WORKDIR}

if [[ -d ".git" ]] && [[ -f ".dev/git-hook/install.sh" ]]; then
    bash .dev/git-hook/install.sh || true
fi

mkdir -p /go/pkg/mod
mkdir -p /go/cache/go-build
touch ${HOME}/.bash_history

sleep infinity
