#!/usr/bin/env bash

# Runs the live e2e harness (.dev/e2e) inside the dev container against the compose stack.
#
#   ./dc up:all          # bring the backends up first
#   .dev/e2e/run.sh      # every section
#
# Each section of the harness is gated on its backend env var; the shared defaults in common.sh set them
# all, so the default run exercises everything. Override any of them to point at other infrastructure, or
# clear one to skip its section:
#
#   REDIS_ADDRESS= .dev/e2e/run.sh                 # skip the redis-backed sections (the example http one too: it resets
#                                                  # the rate limit counters straight in redis)
#   EXAMPLE_BASE_URL= .dev/e2e/run.sh              # skip the live example http section
#
# The example http section drives the .example application the dev container already serves on :8080. It
# calls it over the loopback on purpose: 127.0.0.1 sits outside the example's trusted proxy list, which is
# what proves a spoofed X-Forwarded-For cannot mint a fresh rate limit budget.

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. "${SCRIPT_DIRECTORY_STRING}/common.sh"

e2e_require_dev_service

section_start "MELODY LIVE E2E HARNESS" "${TAG_VALIDATE}" "e2e"

if run_in_dev "${E2E_HARNESS_DIRECTORY_STRING}" "go run ."; then
    section_end "MELODY LIVE E2E HARNESS" "success" "${TAG_VALIDATE}" "e2e"
    exit 0
fi

section_end "MELODY LIVE E2E HARNESS" "failure" "${TAG_VALIDATE}" "e2e"
exit 1
