package amqp

import (
    "os"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func TestDelayExpirationMilliseconds_ClampsSubMillisecondToOne(t *testing.T) {
    if 1 != delayExpirationMilliseconds(200*time.Microsecond) {
        t.Fatalf("expected a sub-millisecond delay to clamp to 1ms, got %d (a \"0\" TTL expires immediately and drops the backoff)", delayExpirationMilliseconds(200*time.Microsecond))
    }

    if 1 != delayExpirationMilliseconds(999*time.Microsecond) {
        t.Fatalf("expected 999us to clamp to 1ms, got %d", delayExpirationMilliseconds(999*time.Microsecond))
    }

    if 5 != delayExpirationMilliseconds(5*time.Millisecond) {
        t.Fatalf("expected 5ms to stay 5, got %d", delayExpirationMilliseconds(5*time.Millisecond))
    }
}

func TestServerSentEventBackplane_EnsurePublishChannel_ReopensClosedChannel(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection: connection,
        Hub:        melodyhttp.NewServerSentEventHub(),
        Exchange:   "melody.sse.reopen-publish",
    })

    first, firstErr := backplane.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := backplane.ensurePublishChannel()
    if nil != secondErr {
        t.Fatalf("second ensurePublishChannel: %v", secondErr)
    }
    if true == second.IsClosed() {
        t.Fatalf("expected a fresh open channel, got a closed one (the stale channel was reused)")
    }
    if second == first {
        t.Fatalf("expected the stale closed channel to be replaced, got the same channel")
    }
}
