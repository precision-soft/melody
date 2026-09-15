package amqp

import (
    "errors"
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

func WithReconnectConfig(reconnectConfig *ReconnectConfig) ProviderOption {
    return func(provider *Provider) {
        provider.reconnectConfig = reconnectConfig
    }
}

type Provider struct {
    heartbeat       time.Duration
    reconnectConfig *ReconnectConfig
}

func (instance *Provider) NewTransport(config TransportConfig) *Transport {
    return newTransport(config, instance.reconnectConfig)
}

func (instance *Provider) NewServerSentEventBackplane(config ServerSentEventBackplaneConfig) *ServerSentEventBackplane {
    return newServerSentEventBackplane(config, instance.reconnectConfig)
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
            redactDialError(dialErr),
        )
    }

    return connection, nil
}

func (instance *Provider) Dialer(dsn string) func() (*amqp091.Connection, error) {
    return func() (*amqp091.Connection, error) {
        return instance.Open(dsn)
    }
}

/* Close is bounded by a deadline on the socket, because the client's plain Close is an RPC that shares the send locks with every publish: on a connection whose peer stopped reading, a write in flight holds those locks for good and the close would join it, so the one door the composition root has to end a shared connection would itself never return. */
func (instance *Provider) Close(connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    return connection.CloseDeadline(time.Now().Add(closeJoinTimeout))
}

const redactedDsnPlaceholder = "(redacted)"

func redactDsn(dsn string) string {
    parsed, parseErr := neturl.Parse(dsn)
    if nil != parseErr {
        return redactedDsnPlaceholder
    }

    if "" == parsed.Scheme || "" == parsed.Host {
        return redactedDsnPlaceholder
    }

    if nil != parsed.User {
        parsed.User = neturl.User(parsed.User.Username())
    }

    return parsed.String()
}

func redactDialError(dialErr error) error {
    var urlErr *neturl.Error
    if true == errors.As(dialErr, &urlErr) {
        return &neturl.Error{
            Op:  urlErr.Op,
            URL: redactDsn(urlErr.URL),
            Err: redactUrlCause(urlErr.Err),
        }
    }

    return dialErr
}

func redactUrlCause(cause error) error {
    var escapeErr neturl.EscapeError
    if true == errors.As(cause, &escapeErr) {
        return errors.New("invalid percent-escape in the redacted dsn")
    }

    var invalidHostErr neturl.InvalidHostError
    if true == errors.As(cause, &invalidHostErr) {
        return errors.New("invalid host in the redacted dsn")
    }

    return cause
}
