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

/* Close is CloseWithContext with no deadline of the caller's, bounded by closeJoinTimeout. */
func (instance *Provider) Close(connection *amqp091.Connection) error {
    return instance.CloseWithContext(context.Background(), connection)
}

/* CloseWithContext closes a connection under a deadline on the socket, the join timeout within what is left of the caller's deadline, and returns within that bound plus a short grace. The client's plain Close is an RPC sharing the send locks with every publish, so on a connection whose peer stopped reading a write in flight holds those locks and an unbounded close would never return; and a shutdown the client already began holds the connection mutex its close takes first, so even CloseDeadline made in line would wait for that write. A close that did not return within the bound answers an error naming it; the client's close ends when its shutdown completes. */
func (instance *Provider) CloseWithContext(closeContext context.Context, connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, closeJoinTimeout), connection)

    return closeErr
}

/* redactedDsnPlaceholder stands in for any dsn this function cannot prove it has stripped credentials from. */
const redactedDsnPlaceholder = "(redacted)"

/* redactDsn strips the password from a dsn for logging. It fails closed: a string that does not parse as a url with a scheme and a host becomes the placeholder, because net/url parses "guest:guest@host" into a scheme of "guest" with no userinfo, which would leak the password verbatim; and so does a dsn carrying an "@" the parser did not read as userinfo, because a password holding an unescaped "/", "?" or "#" ends the authority early and lands in the host, the path or the fragment, the part before it read as a host and a port. */
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

/* redactDialError strips any raw dsn embedded in a dial error before it is wrapped as a cause and logged: amqp091.DialConfig surfaces a net/url parse failure as a *url.Error whose Error() quotes the entire dsn, password included. The url is replaced with redactDsn's fail-closed redaction, which yields the placeholder for a dsn that cannot parse. */
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

/* redactUrlCause keeps none of the dsn bytes net/url embeds in its parse-error values: url.EscapeError carries the offending percent-escape triple, which may be two password characters, url.InvalidHostError the offending host byte, and the plain errors quote the offending port, segment or scheme, which a password holding a reserved character turns into password bytes. Only the two typed causes are named, by a fixed text; every other cause becomes "invalid dsn". */
func redactUrlCause(cause error) error {
    var escapeErr neturl.EscapeError
    if true == errors.As(cause, &escapeErr) {
        return errors.New("invalid percent-escape in the redacted dsn")
    }

    var invalidHostErr neturl.InvalidHostError
    if true == errors.As(cause, &invalidHostErr) {
        return errors.New("invalid host in the redacted dsn")
    }

    return errors.New("invalid dsn")
}
