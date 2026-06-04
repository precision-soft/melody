package amqp

import (
    neturl "net/url"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func NewProvider(options ...ProviderOption) *Provider {
    provider := &Provider{}
    for _, option := range options {
        option(provider)
    }

    return provider
}

type ProviderOption func(*Provider)

func WithHeartbeat(heartbeat time.Duration) ProviderOption {
    return func(provider *Provider) {
        provider.heartbeat = heartbeat
    }
}

type Provider struct {
    heartbeat time.Duration
}

func (instance *Provider) Open(dsn string) (*amqp091.Connection, error) {
    if "" == dsn {
        return nil, exception.NewError("amqp dsn is empty", nil, nil)
    }

    config := amqp091.Config{}
    if 0 < instance.heartbeat {
        config.Heartbeat = instance.heartbeat
    }

    connection, dialErr := amqp091.DialConfig(dsn, config)
    if nil != dialErr {
        return nil, exception.NewError(
            "amqp connection failed",
            map[string]any{"dsn": redactDsn(dsn)},
            dialErr,
        )
    }

    return connection, nil
}

func (instance *Provider) Close(connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    return connection.Close()
}

func redactDsn(dsn string) string {
    parsed, parseErr := neturl.Parse(dsn)
    if nil != parseErr {
        return ""
    }

    if nil != parsed.User {
        parsed.User = neturl.User(parsed.User.Username())
    }

    return parsed.String()
}
