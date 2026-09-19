package config

import (
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
)

func moduleWithEnvironment(t *testing.T, values map[string]string) *Module {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    return &Module{configuration: configuration}
}

/* Nothing dials at boot. The dsn below points at an address no broker answers on, and the transport is built
   over it without a packet leaving the process: what used to stand here was a full amqp handshake, paid by
   every process — measured on the running stack, db:migrate --help paid one before printing its usage — and
   a boot that could not reach the broker panicked instead of starting. A dsn this application cannot dial
   now surfaces at the first publish, through the transport's own retry loop.

   The test is written as a deadline rather than as an assertion on the return, because the failure it guards
   against is a dial: the address is a discard port on a host that does not resolve, so a probe restored here
   would spend the resolver's own timeout before it panicked. */
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

/* The gate above it, unchanged and pinned: without a dsn the example runs the whole message bus in process,
   which is what makes a checkout without a broker functional rather than merely bootable. */
func TestBuildMessageBusTransport_FallsBackToTheInProcessTransportWithoutADsn(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})

    transport := moduleInstance.buildMessageBusTransport()

    if _, isInMemory := transport.(*melodymessagebus.InMemoryTransport); false == isInMemory {
        t.Fatalf("expected the in-process transport without a dsn, got %T", transport)
    }
}
