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

func (instance *Provider) Close(connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    return connection.Close()
}

/* redactedDsnPlaceholder stands in for any dsn this function cannot prove it has stripped credentials from. */
const redactedDsnPlaceholder = "(redacted)"

/* redactDsn strips the password from a dsn for logging. It fails CLOSED: a string that does not parse as a proper url with a scheme and a host is returned as a placeholder rather than verbatim, because net/url happily parses "guest:guest@host" into a scheme of "guest" with no userinfo at all — and returning that unchanged would put the password straight into the connection-failure log line. */
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

/* redactDialError strips any raw dsn embedded in a dial error before it is wrapped as a cause and logged. amqp091.DialConfig surfaces a net/url parse failure (an unescaped '%' in the password, an unexpanded template, a control character) as a *url.Error whose Error() quotes the entire raw dsn — password included — so the exception cause chain would otherwise print the secret on every connection and reconnect failure. The offending url is replaced with the same fail-closed redaction redactDsn applies to the context field; a dsn malformed enough to reach here cannot re-parse, so redactDsn yields the placeholder rather than a leak. */
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

/* redactUrlCause strips the dsn fragments net/url embeds in its parse-error values: url.EscapeError carries the offending percent-escape triple (e.g. "%ss", the two password characters after a literal '%'), and url.InvalidHostError carries the offending host byte. url.Error.Error() renders that value verbatim, so it would reach the log even after the URL field itself is redacted. */
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
