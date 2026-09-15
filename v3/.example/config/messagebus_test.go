package config

import (
    "testing"
    "time"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
)

func TestBuildMessageBusTransport_DoesNotDialAtBoot(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{
        "AMQP_DSN": "amqp://guest:guest@broker.invalid:5672/",
    })

    built := make(chan any, 1)
    go func() {
        defer func() {
            built <- recover()
        }()

        transport := moduleInstance.buildMessageBusTransport()
        if nil == transport {
            t.Error("expected a transport built over the dsn")
        }
    }()

    select {
    case recovered := <-built:
        if nil != recovered {
            t.Fatalf("expected the transport to be built without dialing, it panicked: %v", recovered)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("building the transport did not return within 2s, so something dialed")
    }
}

func TestBuildMessageBusTransport_FallsBackToTheInProcessTransportWithoutADsn(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    transport := moduleInstance.buildMessageBusTransport()

    if _, isInMemory := transport.(*melodymessagebus.InMemoryTransport); false == isInMemory {
        t.Fatalf("expected the in-process transport without a dsn, got %T", transport)
    }
}
