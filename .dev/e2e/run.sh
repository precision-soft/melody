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
#   MINIO_ENDPOINT= .dev/e2e/run.sh                # skip the object storage section
#   EXAMPLE_BASE_URL= .dev/e2e/run.sh              # skip the live example http section
#   MELODY_E2E_MAJORS='1 3' .dev/e2e/run.sh        # drive the v1 and v3 example applications, leave v2 out
#   MELODY_E2E_MAJORS= .dev/e2e/run.sh             # skip the per-major example application sections
#
# The example http section drives the .example application the dev container already serves on :8080. It
# calls it over the loopback on purpose: 127.0.0.1 sits outside the example's trusted proxy list, which is
# what proves a spoofed X-Forwarded-For cannot mint a fresh rate limit budget.
#
# The per-major EXAMPLE APPLICATION sections drive a DIFFERENT application: for each major listed in
# MELODY_E2E_MAJORS (all three by default) the harness builds that major's .example into its own workspace
# under /tmp, boots it on its own port (18081/18082/18083) and asserts the surface all three majors share —
# a public route, a real login flow through a cookie jar, the protected route before and after login, logout,
# encoded path-traversal containment, a 404, three command-line invocations and a single-SIGINT shutdown.
# Every line those sections print is prefixed with the major it ran against.

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
