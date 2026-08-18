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
#   EXAMPLE_BASE_URL= .dev/e2e/run.sh              # skip ALL thirteen sections that drive the supervised example
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
# Twelve further sections drive that same supervised application: OPAQUE TOKEN REVOCATION (a revocation refuses a
#   token while its entry is still in redis, and an entry written after the revocation but stamped before it is
#   refused too — the case an index walk could never cover), OPENAPI SERVED (the document the serving process
# builds), TRANSLATION (catalogues, ICU plurals, the fallback chain and the token firewall's json entry point),
# SCOPED SERVICE (the request-scoped journal trail: the entry a write leaves is proof the event listener and
# the flush middleware held one instance, and its request id is proof no instance was carried between requests),
# CATALOG NOTIFICATION (two live sockets on the running application; one write and both are told the same thing),
# TOKEN AUTHENTICATION and SWITCH-USER IMPERSONATION (credentials minted by the application's own auth:token
# command), HMAC OVER HTTP (an internal:sign envelope verified across a real process boundary), TWO-FACTOR
# (enroll/verify with totp:code plus four refusals), MYSQL AND AUDIT (the second factor's secret encrypted at
# rest and re-read out of band; the trail of a real catalogue write, and a password change recorded as a change
# with its value masked),
# MULTIPART (a multipart body the framework leaves unread and its urlencoded twin), SERVER-SENT EVENTS (the harness
# joins the redis backplane as a second replica) and METRICS, which runs LAST because it asserts exact
# request-count deltas and any later request would move them.
#
# Because all twelve share one application, no new section may write to the nomenclature BEFORE EXAMPLE OVER
# HTTP (it asserts the exact call at which the budget is exhausted); a section that runs after it and writes —
# SCOPED SERVICE, CATALOG NOTIFICATION and MYSQL AND AUDIT all do, because what they assert is what a write
# leaves behind — must reset the budget first.
# Sections built on the shared liveExampleClient may not log in or carry a cookie jar; one that must
# authenticate opens its own jarred client. None may touch /outbox/* (a stack.sh check asserts its sent-count
# delta). The rule is repeated in .dev/e2e/examplehttp.go, beside the calls it governs.
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
#
# The v1 and v2 sections also prove the showcase wirings those two examples carry (.dev/e2e/exampleshowcase.go):
# the cors preflight answered 204 before routing and security, the 401 that still carries the cors headers,
# the api-key firewall on /products/api (the key alone reads the catalogue, a wrong key is 401, cookie flows
# keep working), the gzip-compressed listing inflated back to its identity twin, the two-mistake payload
# refused 400 with one errors entry per field, and a throttled write carrying X-Forwarded-For budgeted against
# the forwarded address — read back out of the redis rate-limit key. It then restarts the process and proves
# the pre-restart session cookie still admits, which only the file-backed session storage can make true.

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
