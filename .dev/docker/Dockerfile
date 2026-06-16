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
ENV GOPRIVATE=github.com/precision-soft
ENV GONOSUMDB=github.com/precision-soft

RUN apk add --update --no-cache git shadow bash

ENV GOBIN=/usr/local/bin
ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"
RUN go install github.com/cespare/reflex@v0.3.1

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
