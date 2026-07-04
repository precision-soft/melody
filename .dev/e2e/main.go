/* Command e2e is a live, functional exercise of melody's cross-app / integration primitives against real
infrastructure — the behaviours that only show up end-to-end against a real broker, database or cache. It
is DEV TOOLING, kept under .dev (not shipped in any module tag) and grouped with the other dev scripts.

Run it against the dev compose stack (./dc up:all) from inside the dev container. Each section runs only
when its backend env var is set, so you can exercise one or all:

    docker exec \
      -e REDIS_ADDRESS=redis:6379 \
      -e POSTGRES_DSN='postgres://melody:melody@postgres:5432/melody_test?sslmode=disable' \
      -e AMQP_DSN='amqp://guest:guest@rabbitmq:5672/' \
      -e GOWORK=off melody-dev-1 \
      sh -c 'export PATH=/usr/local/go/bin:$PATH; cd /app/.dev/e2e && go run .'

Sections:
  - CROSS-APP HMAC AUTH  (redis)    — sign→verify, actor propagation, replay/audience/tamper rejection
  - OUTBOX               (postgres) — transactional enqueue → relay drains to transport, poison dead-letter
  - PGSQL ADVISORY LOCK  (postgres) — mutual exclusion, release hand-off
  - AMQP PUBLISH/CONSUME (rabbitmq) — round-trip publish → consume → ack, and delayed redelivery

Exits non-zero on the first unexpected outcome. */
package main

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http/httptest"
    "os"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func main() {
    sections := 0

    if address := os.Getenv("REDIS_ADDRESS"); "" != address {
        section("CROSS-APP HMAC AUTH (live redis)")
        runHmacCheck(address)
        sections++
    }

    if dsn := os.Getenv("POSTGRES_DSN"); "" != dsn {
        section("OUTBOX (live postgres)")
        runOutboxCheck(dsn)
        sections++

        section("PGSQL ADVISORY LOCK (live postgres)")
        runPgsqlLockCheck(dsn)
        sections++
    }

    if dsn := os.Getenv("AMQP_DSN"); "" != dsn {
        section("AMQP PUBLISH/CONSUME (live rabbitmq)")
        runAmqpCheck(dsn)
        sections++
    }

    if 0 == sections {
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

func fail(format string, args ...any) {
    fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
    os.Exit(1)
}
