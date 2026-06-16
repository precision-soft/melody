package amqp

import (
    "testing"
    "time"

    amqp091 "github.com/rabbitmq/amqp091-go"
)

func newTestTransportConfig() TransportConfig {
    return TransportConfig{
        Dialer:   func() (*amqp091.Connection, error) { return nil, nil },
        Queue:    "orders",
        Registry: NewMessageRegistry(),
    }
}

func TestNewTransport_DefaultReconnectAndBuffer(t *testing.T) {
    instance := NewTransport(newTestTransportConfig())

    defaults := DefaultReconnectConfig()
    if defaults.InitialBackoff != instance.reconnect.InitialBackoff || defaults.MaxBackoff != instance.reconnect.MaxBackoff || defaults.BackoffFactor != instance.reconnect.BackoffFactor {
        t.Fatalf("expected default reconnect config, got %+v", instance.reconnect)
    }

    if defaultPublishReturnBuffer != instance.publishReturnBuffer {
        t.Fatalf("expected default publish return buffer %d, got %d", defaultPublishReturnBuffer, instance.publishReturnBuffer)
    }
}

func TestNewTransport_PerTransportOverride(t *testing.T) {
    config := newTestTransportConfig()
    config.Reconnect = &ReconnectConfig{InitialBackoff: 5 * time.Second}
    config.PublishReturnBuffer = 64

    instance := NewTransport(config)

    if 5*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected overridden initial backoff 5s, got %s", instance.reconnect.InitialBackoff)
    }

    if 64 != instance.publishReturnBuffer {
        t.Fatalf("expected publish return buffer 64, got %d", instance.publishReturnBuffer)
    }
}

func TestProviderNewTransport_GeneralLayerInherited(t *testing.T) {
    provider := NewProvider(WithReconnectConfig(&ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute}))

    instance := provider.NewTransport(newTestTransportConfig())

    if 2*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected general initial backoff 2s, got %s", instance.reconnect.InitialBackoff)
    }

    if time.Minute != instance.reconnect.MaxBackoff {
        t.Fatalf("expected general max backoff 1m, got %s", instance.reconnect.MaxBackoff)
    }

    if 2.0 != instance.reconnect.BackoffFactor {
        t.Fatalf("expected default backoff factor 2.0, got %v", instance.reconnect.BackoffFactor)
    }
}

func TestProviderNewTransport_TransportOverridesGeneral(t *testing.T) {
    provider := NewProvider(WithReconnectConfig(&ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute}))

    config := newTestTransportConfig()
    config.Reconnect = &ReconnectConfig{MaxBackoff: 10 * time.Second}

    instance := provider.NewTransport(config)

    if 2*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected inherited initial backoff 2s, got %s", instance.reconnect.InitialBackoff)
    }

    if 10*time.Second != instance.reconnect.MaxBackoff {
        t.Fatalf("expected overridden max backoff 10s, got %s", instance.reconnect.MaxBackoff)
    }
}
