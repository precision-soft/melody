/* Command e2e is a live, functional exercise of melody's cross-app / integration primitives against real
infrastructure — the behaviours that only show up end-to-end against a real broker, database or cache. It
is DEV TOOLING, kept under .dev (not shipped in any module tag) and grouped with the other dev scripts.

Run it against the dev compose stack (./dc up:all) with .dev/e2e/run.sh, which sets every backend env var
and executes the harness inside the dev container. Each section runs only when its backend env var is set,
so you can exercise one or all by invoking it by hand instead:

    docker exec \
      -e REDIS_ADDRESS=redis:6379 \
      -e POSTGRES_DSN='postgres://melody:melody@postgres:5432/melody_test?sslmode=disable' \
      -e AMQP_DSN='amqp://guest:guest@rabbitmq:5672/' \
      -e MINIO_ENDPOINT=localstack:4566 -e MINIO_ACCESS_KEY=test -e MINIO_SECRET_KEY=test \
      -e GOWORK=off melody-dev-1 \
      sh -c 'export PATH=/usr/local/go/bin:$PATH; cd /app/.dev/e2e && go run .'

Sections:
  - WEBSOCKET FAN-OUT      (in-process) — handshake + hub broadcast delivered to two live sockets, then keepalive: a client that pongs stays, one that falls silent is reaped
  - ENCRYPT COMPARTMENTS   (in-process) — named-cipher isolation, redaction, unregistered compartment errors
  - SESSION ID ROTATION    (in-process) — rotate + republish in one call, the destroyed id buys nothing, an unpublished rotation logs the client out
  - CROSS-APP HMAC AUTH    (redis)    — sign→verify, actor propagation, replay/audience/tamper rejection
  - CACHE                  (redis)    — set/get round-trip, time-to-live expiry, miss, atomic increment
  - RATE LIMIT             (redis)    — shared budget, window reset, fail-closed default, opt-in fail-open
  - RUN EXCLUSIVE          (redis)    — one holder per tick, release on return, fail-closed on store outage
  - LEADER GATE            (redis)    — one leader of two contenders, follower promoted on shutdown
  - OUTBOX                 (postgres) — transactional enqueue → relay drains to transport, poison dead-letter
  - PGSQL ADVISORY LOCK    (postgres) — mutual exclusion, release hand-off
  - MIGRATE                (postgres) — up creates+seeds a table, down rolls it back; per-context isolation
  - AMQP PUBLISH/CONSUME   (rabbitmq) — round-trip publish → consume → ack, and delayed redelivery
  - OBJECT STORAGE PUT     (localstack) — the declared-size contract: a short declaration is refused before the bucket is touched, so the object already at the key survives
  - MAIL                   (mailpit)  — smtp transport sends a whole session under the per-step deadline; mailpit confirms receipt
  - EXAMPLE OVER HTTP      (example)  — forwarded-client-ip trust boundary and the rate limit over real HTTP
  - OPENAPI SERVED         (example)  — the document the SERVING process builds: the booted routes, a typed response's component schema, every $ref resolving
  - TRANSLATION            (example)  — both catalogues, all three ICU plural branches, the locale fallback chain, and the token firewall's own json entry point
  - TOKEN AUTHENTICATION   (example)  — a token minted by the application's own auth:token command; refusals for no header, a foreign key, a flipped signature and alg:none
  - SWITCH-USER IMPERSONATION (example) — both identities under a switch; a caller without the switch role stays itself; an unknown target fails closed
  - HMAC OVER HTTP         (example)  — an internal:sign envelope verified across a real process boundary; refusals for replay, tamper, body and query mismatch
  - TWO-FACTOR             (example)  — enroll and verify with totp:code; refusals for replay, whitespace-normalised replay, an expired code and a user never enrolled
  - MYSQL AND AUDIT        (example + mysql) — encrypted at rest, re-read out of band through the harness's own connection; the audit trail's exact entries and change set
  - MULTIPART              (example)  — a multipart body reaches the handler unread and byte-identical; the urlencoded twin is drained and restored
  - SERVER-SENT EVENTS     (example + redis) — preamble and headers, in-process and cross-replica delivery, id/retry framing, id sanitisation, origin suppression, topic isolation, malformed payloads
  - METRICS                (example + prometheus) — the counter and the histogram grow by EXACTLY the requests issued, a control route stays put, and a real prometheus scrapes the endpoint
  - EXAMPLE APPLICATION v1 (example)  — the .example application of major 1, built and booted by the harness
  - EXAMPLE APPLICATION v2 (example)  — the same, for major 2
  - EXAMPLE APPLICATION v3 (example)  — the same, for major 3

The eleven example-over-http sections all drive the ONE application the dev container supervises on
EXAMPLE_BASE_URL, so they share a fixture and observe two rules stated in examplehttp.go beside the calls: no
section other than EXAMPLE OVER HTTP may call /ratelimit/demo (it asserts an exact exhaustion point), and nothing
may touch /outbox/* (a stack.sh check asserts its sent-count delta). METRICS runs last of the group because its
assertions are exact request-count deltas.

The per-major EXAMPLE APPLICATION sections build each major's .example into its own workspace, boot it on its own
port and drive it end to end: a public route, the login flow through a real cookie jar, the protected route before
and after login, logout, encoded path-traversal containment, a 404, three command-line invocations and a graceful
shutdown on one SIGINT. MELODY_E2E_MAJORS selects which majors run and defaults to all three.

Clear-to-skip recipes beyond the backend variables above:

    MYSQL_DSN=       — MYSQL AND AUDIT keeps its application-side half and announces that the ciphertext was NOT
                       re-read out of band; TWO-FACTOR announces the enrollment row it left behind
    PROMETHEUS_URL=  — METRICS keeps its delta assertions and announces that no collector was proven to read the
                       endpoint
    REDIS_ADDRESS=   — additionally skips SERVER-SENT EVENTS entirely (the cross-replica half needs the backplane),
                       and the per-major cache and rate-limit demos announce that they were not verified out of
                       band. The example applications read their own .env beside their own executable, so a
                       cleared variable never reconfigures them: it only withdraws the harness's ability to check
                       what they did. MYSQL_DSN= likewise leaves the demo notes those runs wrote behind

The websocket, encrypt and session sections need no backend and always run; the rest run only when their env
var is set. Exits non-zero on the first unexpected outcome. */
package main

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http/httptest"
    "os"
    "time"

    "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func main() {
    sections := 0
    infrastructureSections := 0

    section("WEBSOCKET FAN-OUT (in-process)")
    runWebsocketCheck()
    sections++

    section("ENCRYPT COMPARTMENTS (in-process)")
    runEncryptCompartmentCheck()
    sections++

    section("SESSION ID ROTATION (in-process)")
    runSessionRotationCheck()
    sections++

    redisAddress := os.Getenv("REDIS_ADDRESS")

    if address := redisAddress; "" != address {
        infrastructureSections++
        section("CROSS-APP HMAC AUTH (live redis)")
        runHmacCheck(address)
        sections++

        section("CACHE (live redis)")
        runCacheCheck(address)
        sections++

        section("RATE LIMIT (live redis)")
        runRateLimitCheck(address)
        sections++

        section("RUN EXCLUSIVE (live redis)")
        runRunExclusiveCheck(address)
        sections++

        section("LEADER GATE (live redis)")
        runLeaderGateCheck(address)
        sections++
    }

    if dsn := os.Getenv("POSTGRES_DSN"); "" != dsn {
        infrastructureSections++
        section("OUTBOX (live postgres)")
        runOutboxCheck(dsn)
        sections++

        section("PGSQL ADVISORY LOCK (live postgres)")
        runPgsqlLockCheck(dsn)
        sections++

        section("MIGRATE (live postgres)")
        runMigrateCheck(dsn)
        sections++
    }

    if dsn := os.Getenv("AMQP_DSN"); "" != dsn {
        infrastructureSections++
        section("AMQP PUBLISH/CONSUME (live rabbitmq)")
        runAmqpCheck(dsn)
        sections++
    }

    if endpoint := os.Getenv("MINIO_ENDPOINT"); "" != endpoint {
        infrastructureSections++
        section("OBJECT STORAGE PUT (live s3)")
        runObjectStorageCheck(endpoint, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"))
        sections++
    }

    /* the section needs both halves — the smtp endpoint to send through and the mailpit api to verify the receipt — so clearing either variable skips it, per the clear-to-skip contract every backend variable follows; silently substituting a default for a cleared MAILPIT_API_URL would point the check at infrastructure the run explicitly opted out of. */
    if smtpAddress, mailpitApiUrl := os.Getenv("SMTP_ADDRESS"), os.Getenv("MAILPIT_API_URL"); "" != smtpAddress && "" != mailpitApiUrl {
        infrastructureSections++
        section("MAIL (live mailpit)")
        runMailCheck(smtpAddress, mailpitApiUrl)
        sections++
    }

    if baseUrl := os.Getenv("EXAMPLE_BASE_URL"); "" != baseUrl {
        /* the section resets the example's rate limit counters straight in redis, so it needs redis as much as it needs the
           application: without this it hard-failed on the very "clear REDIS_ADDRESS to skip the redis-backed sections" run that
           run.sh documents */
        if "" == redisAddress {
            fmt.Println("\nSKIPPED: EXAMPLE OVER HTTP needs REDIS_ADDRESS to reset the rate limit counters")
        } else {
            infrastructureSections++
            section("EXAMPLE OVER HTTP (live example application)")
            runExampleHttpCheck(baseUrl, os.Getenv("EXAMPLE_LOAD_BALANCER_URL"), redisAddress)
            sections++
        }

        /* the sections below drive the SAME supervised application over http and are gated on EXAMPLE_BASE_URL
           alone: none of them touches redis directly, so clearing REDIS_ADDRESS must not take them out too. The
           one exception states its own second gate — the server-sent-event section needs the redis backplane to
           reach the application as a second replica. They run in this order for a reason: METRICS asserts EXACT
           request-count deltas, so it goes last and its deltas bracket only its own requests. */
        infrastructureSections++

        section("OPENAPI SERVED (live example application)")
        runOpenApiCheck(baseUrl)
        sections++

        section("TRANSLATION (live example application)")
        runTranslationCheck(baseUrl)
        sections++

        section("TOKEN AUTHENTICATION (live example application)")
        runTokenAuthCheck(baseUrl)
        sections++

        section("SWITCH-USER IMPERSONATION (live example application)")
        runImpersonationCheck(baseUrl)
        sections++

        section("HMAC OVER HTTP (live example application)")
        runInternalAuthCheck(baseUrl)
        sections++

        section("TWO-FACTOR (live example application)")
        runTwoFactorCheck(baseUrl)
        sections++

        /* the mint workspace is shared by the sections above, so it cannot be torn down by whichever of them
           finishes first */
        releaseExampleMintWorkspace()

        section("MYSQL AND AUDIT (live example application)")
        runMysqlCheck(baseUrl)
        sections++

        section("MULTIPART (live example application)")
        runMultipartCheck(baseUrl)
        sections++

        /* the server-sent-event section needs BOTH halves: the application to stream from and redis to reach it
           through as a second replica. Clearing either one must announce what did not run rather than degrade to a
           single-replica check that silently drops the cross-replica, origin-suppression and framing coverage. */
        if "" == redisAddress {
            fmt.Println("\nSKIPPED: SERVER-SENT EVENTS needs REDIS_ADDRESS — the cross-replica delivery, id/retry framing, id sanitisation, origin suppression, topic isolation and malformed-payload coverage did not run")
        } else {
            section("SERVER-SENT EVENTS (live example application + redis backplane)")
            runServerSentEventCheck(baseUrl, redisAddress)
            sections++
        }

        /* LAST of the group on purpose: the metrics assertions are exact request-count deltas, so anything issued
           against this application afterwards would move a counter that was already measured */
        section("METRICS (live example application)")
        runMetricsCheck(baseUrl)
        sections++
    }

    /* the per-major example sections need no backend of the harness's own: each application is configured from the .env beside
       its own executable, which is what keeps clearing REDIS_ADDRESS (or any other backend variable) from reconfiguring them.
       The harness's own connections are handed in for the out-of-band half of the integration demos — the read that proves the
       application really reached the backend rather than merely reporting that it did. Clearing one of them therefore skips the
       verification, not the application's behaviour: the example still wires and still serves the demo. */
    exampleMajors := exampleMajorList()
    if 0 == len(exampleMajors) {
        fmt.Printf("\nSKIPPED: %s is empty — no major's .example application was built, booted or driven\n", exampleMajorListVariable)
    }

    for _, major := range exampleMajors {
        section(fmt.Sprintf("EXAMPLE APPLICATION %s (live, built from %s)", major.label, major.relativeDirectory))
        runExampleApplicationCheck(major, redisAddress, os.Getenv("MYSQL_DSN"))
        sections++
    }

    /* the in-process sections (websocket, encrypt, session) always run, so they can never witness a missing backend: the guard
       has to count only the sections a backend gates, or a run against an empty environment reports "ALL 3 SECTIONS PASSED" and
       the twelve that matter are silently skipped */
    if 0 == infrastructureSections {
        fail("no infrastructure env set — expected one or more of REDIS_ADDRESS / POSTGRES_DSN / AMQP_DSN / MINIO_ENDPOINT")
    }

    fmt.Printf("\nALL %d E2E SECTION(S) PASSED\n", sections)
}

func newRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func buildRequest(method string, path string, body []byte, headerName string, headerValue string) httpcontract.Request {
    var reader io.Reader
    if 0 < len(body) {
        reader = bytes.NewReader(body)
    }

    httpRequest := httptest.NewRequest(method, path, reader)
    if "" != headerValue {
        httpRequest.Header.Set(headerName, headerValue)
    }

    return melodyhttp.NewRequest(
        httpRequest,
        nil,
        newRuntime(),
        melodyhttp.NewRequestContext("e2e", time.Now()),
    )
}

func section(title string) {
    fmt.Printf("\n=== %s ===\n", title)
}

func pass(format string, args ...any) {
    fmt.Printf("PASS  "+format+"\n", args...)
}

func skip(format string, args ...any) {
    fmt.Printf("SKIP  "+format+"\n", args...)
}

func fail(format string, args ...any) {
    fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
    runFailureCleanup()
    os.Exit(1)
}

/* failureCleanupList holds the teardown steps a failing run must take before it exits. os.Exit runs no deferred
function, so a section that started a child process has to register its teardown here or the process outlives the
run and holds its port against the next one — which the next run would then report as an occupied port instead of
the failure that actually happened. */
var failureCleanupList []func()

/* pushFailureCleanup registers a teardown step and returns the function that unregisters it, so a section that
finished cleanly does not leave a stale entry behind for a later, unrelated failure to run. */
func pushFailureCleanup(cleanup func()) func() {
    index := len(failureCleanupList)
    failureCleanupList = append(failureCleanupList, cleanup)

    return func() {
        if index < len(failureCleanupList) {
            failureCleanupList[index] = nil
        }
    }
}

func runFailureCleanup() {
    for index := len(failureCleanupList) - 1; 0 <= index; index-- {
        cleanup := failureCleanupList[index]
        if nil == cleanup {
            continue
        }

        cleanup()
    }

    failureCleanupList = nil
}
