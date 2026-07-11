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
      -e GOWORK=off melody-dev-1 \
      sh -c 'export PATH=/usr/local/go/bin:$PATH; cd /app/.dev/e2e && go run .'

Sections:
  - WEBSOCKET FAN-OUT      (in-process) — handshake + hub broadcast delivered to two live sockets
  - ENCRYPT COMPARTMENTS   (in-process) — named-cipher isolation, redaction, unregistered compartment errors
  - CROSS-APP HMAC AUTH    (redis)    — sign→verify, actor propagation, replay/audience/tamper rejection
  - CACHE                  (redis)    — set/get round-trip, time-to-live expiry, miss, atomic increment
  - RATE LIMIT             (redis)    — shared budget, window reset, fail-closed default, opt-in fail-open
  - RUN EXCLUSIVE          (redis)    — one holder per tick, release on return, fail-closed on store outage
  - LEADER GATE            (redis)    — one leader of two contenders, follower promoted on shutdown
  - OUTBOX                 (postgres) — transactional enqueue → relay drains to transport, poison dead-letter
  - PGSQL ADVISORY LOCK    (postgres) — mutual exclusion, release hand-off
  - MIGRATE                (postgres) — up creates+seeds a table, down rolls it back; per-context isolation
  - AMQP PUBLISH/CONSUME   (rabbitmq) — round-trip publish → consume → ack, and delayed redelivery
  - EXAMPLE OVER HTTP      (example)  — forwarded-client-ip trust boundary and the rate limit over real HTTP

The websocket and encrypt sections need no backend and always run; the rest run only when their env var is
set. Exits non-zero on the first unexpected outcome. */
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
    }

    /* the in-process sections (websocket, encrypt) always run, so they can never witness a missing backend: the guard has to
    count only the sections a backend gates, or a run against an empty environment reports "ALL 2 SECTIONS PASSED" and the
    eleven that matter are silently skipped */
    if 0 == infrastructureSections {
        fail("no infrastructure env set — expected one or more of REDIS_ADDRESS / POSTGRES_DSN / AMQP_DSN")
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
    os.Exit(1)
}
