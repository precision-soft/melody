package amqp

import (
    "context"
    "errors"
    neturl "net/url"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ProviderConfig declares how the provider opens and closes connections and what the transports and backplanes it builds inherit. A limit outside its rule is refused at NewProvider naming the field. */
type ProviderConfig struct {
    /* Heartbeat is the interval the client and the broker prove the connection alive at: 10s when left out, at least 1s since the client reads a shorter one as the server's choice, unlimited refused since the client has no form without one. */
    Heartbeat bound.Duration

    /* Reconnect is the backoff every transport and backplane built through the provider starts from, each field left out the package default. */
    Reconnect ReconnectConfig

    /* CloseTimeout bounds each stretch of a close that cannot observe its signal: 30s when left out, at least 1ms, unlimited refused, since a teardown is never unbounded. The transports and backplanes built through the provider inherit it. */
    CloseTimeout bound.Duration
}

var providerHeartbeatRule = bound.Rule[time.Duration]{
    Name:    "ProviderConfig.Heartbeat",
    Default: 10 * time.Second,
    Minimum: time.Second,
}

var providerCloseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ProviderConfig.CloseTimeout",
    Default: closeJoinTimeout,
    Minimum: bound.TimeoutMinimum,
}

func NewProvider(config ProviderConfig) *Provider {
    heartbeat, heartbeatErr := bound.Resolve(config.Heartbeat, providerHeartbeatRule)
    if nil != heartbeatErr {
        exception.Panic(exception.FromError(heartbeatErr))
    }

    reconnect, reconnectErr := resolveReconnectOver(defaultReconnect, config.Reconnect)
    if nil != reconnectErr {
        exception.Panic(exception.FromError(reconnectErr))
    }

    closeTimeout, closeTimeoutErr := bound.Resolve(config.CloseTimeout, providerCloseTimeoutRule)
    if nil != closeTimeoutErr {
        exception.Panic(exception.FromError(closeTimeoutErr))
    }

    return &Provider{
        heartbeat: heartbeat.Value,
        parent: componentParent{
            reconnect:    reconnect,
            closeTimeout: closeTimeout.Value,
        },
    }
}

type Provider struct {
    heartbeat time.Duration
    parent    componentParent
}

/* componentParent is what a transport or a backplane takes for the fields its config leaves out. */
type componentParent struct {
    reconnect    resolvedReconnect
    closeTimeout time.Duration
}

func resolveInheritedDuration(limit bound.Duration, parent time.Duration, rule bound.Rule[time.Duration]) (time.Duration, error) {
    if true == limit.IsAbsent() {
        return parent, nil
    }

    resolved, resolveErr := bound.Resolve(limit, rule)
    if nil != resolveErr {
        return 0, resolveErr
    }

    return resolved.Value, nil
}

var defaultComponentParent = componentParent{
    reconnect:    defaultReconnect,
    closeTimeout: closeJoinTimeout,
}

func (instance *Provider) NewTransport(config TransportConfig) *Transport {
    return newTransport(config, instance.parent)
}

func (instance *Provider) NewServerSentEventBackplane(config ServerSentEventBackplaneConfig) *ServerSentEventBackplane {
    return newServerSentEventBackplane(config, instance.parent)
}

func (instance *Provider) Open(dsn string) (*amqp091.Connection, error) {
    if "" == dsn {
        return nil, exception.NewError("amqp dsn is empty", nil, nil)
    }

    config := amqp091.Config{Heartbeat: instance.heartbeat}

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

/* Close is CloseWithContext with no deadline of the caller's, bounded by the close timeout. */
func (instance *Provider) Close(connection *amqp091.Connection) error {
    return instance.CloseWithContext(context.Background(), connection)
}

/* CloseWithContext closes a connection under a deadline on the socket, the join timeout within what is left of the caller's deadline, and returns within that bound plus a short grace. The client's plain Close is an RPC sharing the send locks with every publish, so on a connection whose peer stopped reading a write in flight holds those locks and an unbounded close would never return; and a close made in line can still wait behind what such a write holds — the close frame's own send, or the client's shutdown the close joins, which waits for the channel mutex the write holds across the socket call. A close that did not return within the bound answers an error naming it; the client's close ends when its shutdown completes. */
func (instance *Provider) CloseWithContext(closeContext context.Context, connection *amqp091.Connection) error {
    if nil == connection {
        return nil
    }

    closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, instance.parent.closeTimeout), instance.parent.closeTimeout, connection)

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
