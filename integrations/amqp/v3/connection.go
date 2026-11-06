package amqp

import (
    "context"
    "errors"
    neturl "net/url"
    "strings"
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

func (instance *Provider) Close(connection *amqp091.Connection) error {
    return instance.CloseWithContext(context.Background(), connection)
}

/* CloseWithContext returns within the join timeout, capped by the caller's deadline, plus a short grace. */
func (instance *Provider) CloseWithContext(closeContext context.Context, connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, closeJoinTimeout), connection)

    return closeErr
}

const redactedDsnPlaceholder = "(redacted)"

/* redactDsn fails closed: net/url parses "guest:guest@host" as a scheme with no userinfo, which leaks the password. */
func redactDsn(dsn string) string {
    parsed, parseErr := neturl.Parse(dsn)
    if nil != parseErr {
        return redactedDsnPlaceholder
    }

    if "" == parsed.Scheme || "" == parsed.Host {
        return redactedDsnPlaceholder
    }

    if nil == parsed.User && true == strings.Contains(dsn, "@") {
        return redactedDsnPlaceholder
    }

    if nil != parsed.User {
        parsed.User = neturl.User(parsed.User.Username())
    }

    return parsed.String()
}

/* redactDialError strips the dsn from dial errors: a *url.Error's Error() quotes the entire dsn, password included. */
func redactDialError(dialErr error) error {
    var urlErr *neturl.Error
    if true == exception.ChainAs(dialErr, &urlErr) {
        return &neturl.Error{
            Op:  urlErr.Op,
            URL: redactDsn(urlErr.URL),
            Err: redactUrlCause(urlErr.Err),
        }
    }

    return dialErr
}

/* redactUrlCause keeps no dsn bytes: net/url parse errors embed fragments a reserved character turns into password bytes. */
func redactUrlCause(cause error) error {
    var escapeErr neturl.EscapeError
    if true == exception.ChainAs(cause, &escapeErr) {
        return errors.New("invalid percent-escape in the redacted dsn")
    }

    var invalidHostErr neturl.InvalidHostError
    if true == exception.ChainAs(cause, &invalidHostErr) {
        return errors.New("invalid host in the redacted dsn")
    }

    return errors.New("invalid dsn")
}
