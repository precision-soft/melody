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
#   REDIS_ADDRESS= .dev/e2e/run.sh                 # skip the redis-backed sections (EXAMPLE OVER HTTP too: it resets
#                                                  # the rate limit counters straight in redis, and SERVER-SENT EVENTS,
#                                                  # whose cross-replica half needs the backplane)
#   MINIO_ENDPOINT= .dev/e2e/run.sh                # skip the object storage section
#   EXAMPLE_BASE_URL= .dev/e2e/run.sh              # skip ALL eleven sections that drive the supervised example
#   MYSQL_DSN= .dev/e2e/run.sh                     # keep MYSQL AND AUDIT but skip the out-of-band re-read of the
#                                                  # ciphertext, and leave the two-factor enrollment row behind
#                                                  # (both are announced, never silent)
#   PROMETHEUS_URL= .dev/e2e/run.sh                # keep the metric deltas but skip the scrape half
#   MELODY_E2E_MAJORS='1 3' .dev/e2e/run.sh        # drive the v1 and v3 example applications, leave v2 out
#   MELODY_E2E_MAJORS= .dev/e2e/run.sh             # skip the per-major example application sections
#
# The example http sections drive the .example application the dev container already serves on :8080. EXAMPLE
# OVER HTTP calls it over the loopback on purpose: 127.0.0.1 sits outside the example's trusted proxy list, which
# is what proves a spoofed X-Forwarded-For cannot mint a fresh rate limit budget.
#
# Ten further sections drive that same supervised application: OPENAPI SERVED (the document the serving process
# builds), TRANSLATION (catalogues, ICU plurals, the fallback chain and the token firewall's json entry point),
# SCOPED SERVICE (a service declared by a //melody:scoped constructor, proven to be one instance per request
# and none between requests), TOKEN AUTHENTICATION and SWITCH-USER IMPERSONATION (credentials minted by the application's own auth:token
# command), HMAC OVER HTTP (an internal:sign envelope verified across a real process boundary), TWO-FACTOR
# (enroll/verify with totp:code plus four refusals), MYSQL AND AUDIT (encrypted at rest, re-read out of band),
# MULTIPART (a multipart body the framework leaves unread and its urlencoded twin), SERVER-SENT EVENTS (the harness
# joins the redis backplane as a second replica) and METRICS, which runs LAST because it asserts exact
# request-count deltas and any later request would move them.
#
# Because all eleven share one application, no new section may write to the nomenclature (EXAMPLE OVER HTTP asserts the
# exact call at which the budget is exhausted), none may log in or carry a cookie jar, and none may touch
# /outbox/* (a stack.sh check asserts its sent-count delta). The rule is repeated in .dev/e2e/examplehttp.go,
# beside the calls it governs.
#
# The per-major EXAMPLE APPLICATION sections drive a DIFFERENT application: for each major listed in
# MELODY_E2E_MAJORS (all three by default) the harness builds that major's .example into its own workspace
# under /tmp, boots it on its own port (18081/18082/18083) and asserts the surface all three majors share —
# a public route, a real login flow through a cookie jar, the protected route before and after login, logout,
# encoded path-traversal containment, a 404, three command-line invocations and a single-SIGINT shutdown.
# Every line those sections print is prefixed with the major it ran against.
#
# The v1 and v2 sections additionally drive their live-integration demos, between the login and the logout: the
# clock-stamped catalogue report (no backend needed), a redis cache round trip whose key the harness reads back
# out of band, a mysql note written and re-read out of band and then removed again, and the exact exhaustion
# point of the redis rate limiter. They keep their own key prefixes (melody-example-app:), so the counter the
# EXAMPLE OVER HTTP section measures on the supervised application is never spent by them; v3's per-major run
# leaves the demos alone for the same reason. Clearing REDIS_ADDRESS or MYSQL_DSN skips the out-of-band
# verification, not the application's behaviour — the example still wires the integration and still serves it.

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
