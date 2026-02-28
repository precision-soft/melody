FROM golang:1.25-alpine

ARG DOCKER_PATH
ARG WORKDIR
ARG CONFIG_PATH
ARG ROOT_PATH

ENV WORKDIR=${WORKDIR}
ENV CONFIG_PATH=${CONFIG_PATH}
ENV ROOT_PATH=${ROOT_PATH}

ENV HOME=/home

ENV GOCACHE=/go/cache/go-build

RUN apk add --update --no-cache git shadow bash

RUN mkdir -p "${HOME}"
RUN mkdir -p /go/cache/go-build

COPY ${DOCKER_PATH}/.profile* ${HOME}/

ENV ENV="${HOME}/.profile"

RUN printf '%s\n' \
  'if [ -f "$HOME/.profile" ]; then' \
  '  . "$HOME/.profile"' \
  'fi' \
  > "${HOME}/.bashrc" \
  && printf '%s\n' \
  'if [ -f "$HOME/.bashrc" ]; then' \
  '  . "$HOME/.bashrc"' \
  'fi' \
  > "${HOME}/.bash_profile"

WORKDIR ${WORKDIR}

ENV CONFIG_PATH_ENTRYPOINT_SH=${ROOT_PATH}${CONFIG_PATH}/entrypoint.sh

ENV ENTRYPOINT_SH=/entrypoint.sh
COPY ${DOCKER_PATH}/entrypoint.sh ${ENTRYPOINT_SH}

ENTRYPOINT exec ${ENTRYPOINT_SH}

EXPOSE 8080