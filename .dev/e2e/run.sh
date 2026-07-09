#!/usr/bin/env bash

# Runs the live e2e harness (.dev/e2e) inside the dev container against the compose stack.
#
#   ./dc up:all          # bring the backends up first
#   .dev/e2e/run.sh      # every section
#
# Each section of the harness is gated on its backend env var; this script sets them all, so the default
# run exercises everything. Override any of them to point at other infrastructure, or clear one to skip
# its section:
#
#   REDIS_ADDRESS= .dev/e2e/run.sh                 # skip the redis-backed sections
#   EXAMPLE_BASE_URL= .dev/e2e/run.sh              # skip the live example http section
#
# The example http section drives the .example application the dev container already serves on :8080. It
# calls it over the loopback on purpose: 127.0.0.1 sits outside the example's trusted proxy list, which is
# what proves a spoofed X-Forwarded-For cannot mint a fresh rate limit budget.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "${SCRIPT_DIRECTORY_STRING}/../.." && pwd)"

. "${REPOSITORY_ROOT_DIRECTORY_STRING}/.dev/utility.sh"

SERVICE_NAME_STRING="dev"

require_docker
require_docker_daemon

if ! docker_compose_service_exists "${SERVICE_NAME_STRING}"; then
    fail "missing docker compose service: ${SERVICE_NAME_STRING}"
fi

ensure_service_running "${SERVICE_NAME_STRING}"

# every value is overridable from the environment; the defaults address the compose services by their
# service name on the compose network, as seen from inside the dev container
REDIS_ADDRESS="${REDIS_ADDRESS-redis:6379}"
POSTGRES_DSN="${POSTGRES_DSN-postgres://melody:melody@postgres:5432/melody_test?sslmode=disable}"
AMQP_DSN="${AMQP_DSN-amqp://guest:guest@rabbitmq:5672/}"
EXAMPLE_BASE_URL="${EXAMPLE_BASE_URL-http://127.0.0.1:8080}"
EXAMPLE_LOAD_BALANCER_URL="${EXAMPLE_LOAD_BALANCER_URL-http://load-balancer:80}"

section_start "MELODY LIVE E2E HARNESS" "${TAG_VALIDATE}" "e2e"

COMMAND_STRING="$(
    printf '%s' \
        "export PATH=/usr/local/go/bin:\$PATH" \
        " GOWORK=off" \
        " REDIS_ADDRESS='${REDIS_ADDRESS}'" \
        " POSTGRES_DSN='${POSTGRES_DSN}'" \
        " AMQP_DSN='${AMQP_DSN}'" \
        " EXAMPLE_BASE_URL='${EXAMPLE_BASE_URL}'" \
        " EXAMPLE_LOAD_BALANCER_URL='${EXAMPLE_LOAD_BALANCER_URL}'" \
        " && cd /app/.dev/e2e && go run ."
)"

if run_in_service_shell "${SERVICE_NAME_STRING}" "${COMMAND_STRING}"; then
    section_end "MELODY LIVE E2E HARNESS" "success" "${TAG_VALIDATE}" "e2e"
    exit 0
fi

section_end "MELODY LIVE E2E HARNESS" "failure" "${TAG_VALIDATE}" "e2e"
exit 1
