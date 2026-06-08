package amqp

import (
    "os"
    "testing"
)

func TestEnsurePublishChannel_ReopensClosedChannelWithoutDialer(t *testing.T) {
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

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.reopen-publish",
        Registry:   NewMessageRegistry(),
    })

    first, firstErr := transport.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := transport.ensurePublishChannel()
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

func TestEnsureConsumeChannel_ReopensClosedChannelWithoutDialer(t *testing.T) {
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

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.reopen-consume",
        Registry:   NewMessageRegistry(),
    })

    first, firstErr := transport.ensureConsumeChannel()
    if nil != firstErr {
        t.Fatalf("first ensureConsumeChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := transport.ensureConsumeChannel()
    if nil != secondErr {
        t.Fatalf("second ensureConsumeChannel: %v", secondErr)
    }
    if true == second.IsClosed() {
        t.Fatalf("expected a fresh open channel, got a closed one (the stale channel was reused)")
    }
    if second == first {
        t.Fatalf("expected the stale closed channel to be replaced, got the same channel")
    }
}
